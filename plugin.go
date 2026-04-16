//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"

	pdk "github.com/extism/go-pdk"
)

//go:wasmimport mc_db query
func hostDBQuery(offset uint64) uint64

//go:wasmimport mc_db exec
func hostDBExec(offset uint64) uint64

//go:wasmimport mc_event emit
func hostEventEmit(offset uint64) uint64

//go:wasmimport mc_time now
func hostTimeNow(offset uint64) uint64

//go:wasmimport mc_cron validate
func hostCronValidate(offset uint64) uint64

func callDBQuery(sql string, args []any) (dbQueryOutput, error) {
	input := dbQueryInput{SQL: sql, Args: args}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return dbQueryOutput{}, fmt.Errorf("marshal query input: %w", err)
	}
	mem := pdk.AllocateBytes(inputBytes)
	defer mem.Free()

	outOffset := hostDBQuery(mem.Offset())
	outMem := pdk.FindMemory(outOffset)
	outBytes := outMem.ReadBytes()

	var out dbQueryOutput
	if err := json.Unmarshal(outBytes, &out); err != nil {
		return dbQueryOutput{}, fmt.Errorf("unmarshal query output: %w", err)
	}
	if out.Error != "" {
		return out, fmt.Errorf("%s", out.Error)
	}
	return out, nil
}

func callDBExec(sql string, args []any) (dbExecOutput, error) {
	input := dbExecInput{SQL: sql, Args: args}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return dbExecOutput{}, fmt.Errorf("marshal exec input: %w", err)
	}
	mem := pdk.AllocateBytes(inputBytes)
	defer mem.Free()

	outOffset := hostDBExec(mem.Offset())
	outMem := pdk.FindMemory(outOffset)
	outBytes := outMem.ReadBytes()

	var out dbExecOutput
	if err := json.Unmarshal(outBytes, &out); err != nil {
		return dbExecOutput{}, fmt.Errorf("unmarshal exec output: %w", err)
	}
	if out.Error != "" {
		return out, fmt.Errorf("%s", out.Error)
	}
	return out, nil
}

func emitEvent(eventName string, payload any) {
	input := emitInput{EventName: eventName, Payload: payload}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return
	}
	mem := pdk.AllocateBytes(inputBytes)
	defer mem.Free()
	hostEventEmit(mem.Offset())
}

// WASM time.Now() is broken; ask the host instead.
func nowUTC() string {
	mem := pdk.AllocateBytes([]byte("{}"))
	defer mem.Free()
	outOffset := hostTimeNow(mem.Offset())
	outMem := pdk.FindMemory(outOffset)
	outBytes := outMem.ReadBytes()
	var result struct {
		Now string `json:"now"`
	}
	if err := json.Unmarshal(outBytes, &result); err != nil {
		return "1970-01-01T00:00:00Z"
	}
	return result.Now
}

func callCronValidate(expr, tz string) (CronValidateResponse, error) {
	input := cronValidateInput{Expression: expr, Timezone: tz}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return CronValidateResponse{}, fmt.Errorf("marshal cron validate input: %w", err)
	}
	mem := pdk.AllocateBytes(inputBytes)
	defer mem.Free()
	outOffset := hostCronValidate(mem.Offset())
	outMem := pdk.FindMemory(outOffset)
	outBytes := outMem.ReadBytes()

	// host writeHostError shape is {"error": "..."}; surface the message verbatim.
	// Validation failures arrive as CronValidateResponse with Valid=false.
	var resp CronValidateResponse
	if err := json.Unmarshal(outBytes, &resp); err != nil {
		var he hostError
		if jerr := json.Unmarshal(outBytes, &he); jerr == nil && he.Error != "" {
			return CronValidateResponse{Valid: false, Error: he.Error}, nil
		}
		return CronValidateResponse{}, fmt.Errorf("unmarshal cron validate output: %w", err)
	}
	return resp, nil
}

func loadJobByID(id int64) (JobDTO, error) {
	out, err := callDBQuery(
		"SELECT "+jobColumns+" FROM jobs WHERE id = ?1",
		[]any{id},
	)
	if err != nil {
		return JobDTO{}, fmt.Errorf("db query error: %w", err)
	}
	if len(out.Rows) == 0 {
		return JobDTO{}, fmt.Errorf("job %d not found", id)
	}
	return rowToJob(out.Columns, out.Rows[0])
}

// loadRecentRuns fetches up to `limit` most-recent runs for a job, newest first.
func loadRecentRuns(jobID int64, limit int) ([]JobRunDTO, error) {
	if limit <= 0 {
		limit = 10
	}
	out, err := callDBQuery(
		"SELECT "+jobRunColumns+" FROM job_runs WHERE job_id = ?1 ORDER BY triggered_at DESC LIMIT ?2",
		[]any{jobID, limit},
	)
	if err != nil {
		return nil, fmt.Errorf("db query error: %w", err)
	}
	runs := make([]JobRunDTO, 0, len(out.Rows))
	for _, row := range out.Rows {
		r, rerr := rowToJobRun(out.Columns, row)
		if rerr != nil {
			return nil, rerr
		}
		runs = append(runs, r)
	}
	return runs, nil
}

// emitLifecycle is a small convenience wrapper to keep the tool bodies short.
func emitLifecycle(action string, job JobDTO) {
	emitEvent("scheduler.job."+action, JobLifecyclePayload{
		JobID:     job.ID,
		JobName:   job.Name,
		ChangedBy: "agent", // Phase C: no per-call user identity from MCP
		Action:    action,
	})
}

// fail is a tiny helper for the common "log + return 1" pattern in exports.
func fail(msg string) int32 {
	pdk.OutputString(msg)
	return 1
}

func failf(format string, args ...any) int32 {
	pdk.OutputString(fmt.Sprintf(format, args...))
	return 1
}

// queryJobs is the shared backend for both the list_jobs MCP tool and the
// get_scheduler_list UI data function. It enforces the same filters
// (enabled_only, agent_id, limit, offset), orders by id DESC by default, and
// also returns the unfiltered total count so the caller can render pagination.
//
// Extracted as a helper so both entry points share a single SQL source of truth.
func queryJobs(params ListJobsParams, orderBy string) (jobs []JobDTO, total int, err error) {
	sql := "SELECT " + jobColumns + " FROM jobs"
	var args []any
	conds := []string{}
	if params.EnabledOnly {
		conds = append(conds, "enabled = 1")
	}
	if params.AgentID != "" {
		conds = append(conds, fmt.Sprintf("agent_id = ?%d", len(args)+1))
		args = append(args, params.AgentID)
	}
	if len(conds) > 0 {
		sql += " WHERE " + joinAnd(conds)
	}
	if orderBy == "" {
		orderBy = "id DESC"
	}
	sql += " ORDER BY " + orderBy
	if params.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT ?%d", len(args)+1)
		args = append(args, params.Limit)
		if params.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET ?%d", len(args)+1)
			args = append(args, params.Offset)
		}
	}

	out, qerr := callDBQuery(sql, args)
	if qerr != nil {
		return nil, 0, fmt.Errorf("db query error: %w", qerr)
	}
	jobs = make([]JobDTO, 0, len(out.Rows))
	for _, row := range out.Rows {
		j, rowErr := rowToJob(out.Columns, row)
		if rowErr != nil {
			return nil, 0, fmt.Errorf("row scan error: %w", rowErr)
		}
		jobs = append(jobs, j)
	}

	// total = rows matching the same WHERE without LIMIT/OFFSET.
	totalSQL := "SELECT COUNT(*) FROM jobs"
	var totalArgs []any
	if len(conds) > 0 {
		freshConds := []string{}
		if params.EnabledOnly {
			freshConds = append(freshConds, "enabled = 1")
		}
		if params.AgentID != "" {
			freshConds = append(freshConds, fmt.Sprintf("agent_id = ?%d", len(totalArgs)+1))
			totalArgs = append(totalArgs, params.AgentID)
		}
		totalSQL += " WHERE " + joinAnd(freshConds)
	}
	totalOut, terr := callDBQuery(totalSQL, totalArgs)
	total = len(jobs)
	if terr == nil && len(totalOut.Rows) > 0 && len(totalOut.Rows[0]) > 0 {
		total = int(toInt64(totalOut.Rows[0][0]))
	}
	return jobs, total, nil
}

//go:wasmexport list_jobs
func listJobs() int32 {
	var params ListJobsParams
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	jobs, total, err := queryJobs(params, "id DESC")
	if err != nil {
		return failf("%s", err)
	}
	resultBytes, _ := json.Marshal(ListJobsResult{Jobs: jobs, Total: total})
	pdk.OutputString(string(resultBytes))
	return 0
}

// joinAnd is a tiny stdlib-free strings.Join replacement so we don't pull in
// the strings package just for one call. (We do import strings in logic.go,
// but keeping the export bodies dependency-free helps WASM binary size.)
func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

//go:wasmexport get_job
func getJob() int32 {
	var params GetJobParams
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	if params.ID <= 0 {
		return fail("id is required")
	}
	job, err := loadJobByID(params.ID)
	if err != nil {
		return failf("%s", err)
	}
	runs, err := loadRecentRuns(params.ID, 10)
	if err != nil {
		return failf("%s", err)
	}
	resultBytes, _ := json.Marshal(GetJobResult{Job: job, RecentRuns: runs})
	pdk.OutputString(string(resultBytes))
	return 0
}

//go:wasmexport create_job
func createJob() int32 {
	var params CreateJobParams
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	if err := validateCreateJob(params); err != nil {
		return failf("%s", err)
	}

	tz := applyTimezoneDefault(params.Timezone)
	enabled := applyEnabledDefault(params.Enabled)

	// 1. Validate cron expression and pre-compute the first fire time. Without
	// next_fire_at the engine will never pick up the job.
	cron, err := callCronValidate(params.CronExpr, tz)
	if err != nil {
		return failf("cron validate transport error: %s", err)
	}
	if !cron.Valid {
		return failf("invalid cron expression: %s", cron.Error)
	}
	if len(cron.NextFires) == 0 {
		return fail("cron validator returned no next fires")
	}
	nextFire := cron.NextFires[0]

	// 2. Insert.
	now := nowUTC()
	var taskIDArg any
	if params.TaskID != nil {
		taskIDArg = *params.TaskID
	}
	execOut, err := callDBExec(
		`INSERT INTO jobs (name, cron_expr, timezone, prompt, agent_id, task_id, delivery, enabled, next_fire_at, created_at, updated_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)`,
		[]any{
			params.Name, params.CronExpr, tz, params.Prompt, params.AgentID,
			taskIDArg, DeliveryEventBus, boolToInt(enabled), nextFire, now, now,
		},
	)
	if err != nil {
		return failf("db exec error: %s", err)
	}

	// 3. Read-back so the response carries the canonical row (including computed defaults).
	job, err := loadJobByID(execOut.LastInsertID)
	if err != nil {
		return failf("%s", err)
	}

	// 4. Notify the rest of the system.
	emitLifecycle("created", job)

	resultBytes, _ := json.Marshal(CreateJobResult{Job: job})
	pdk.OutputString(string(resultBytes))
	return 0
}

//go:wasmexport update_job
func updateJob() int32 {
	var params UpdateJobParams
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	if params.ID <= 0 {
		return fail("id is required")
	}

	existing, err := loadJobByID(params.ID)
	if err != nil {
		return failf("%s", err)
	}

	// Build SET clause incrementally so we never write columns the caller didn't touch.
	setClauses := []string{}
	args := []any{}
	addSet := func(col string, val any) {
		setClauses = append(setClauses, fmt.Sprintf("%s = ?%d", col, len(args)+1))
		args = append(args, val)
	}

	if params.Name != nil {
		if !jobNameRe.MatchString(*params.Name) {
			return failf("name %q has invalid characters or length", *params.Name)
		}
		addSet("name", *params.Name)
	}
	if params.Prompt != nil {
		addSet("prompt", *params.Prompt)
	}
	if params.AgentID != nil {
		addSet("agent_id", *params.AgentID)
	}
	if params.TaskID != nil {
		// Sentinel: -1 clears the task linkage to NULL.
		if *params.TaskID == -1 {
			addSet("task_id", nil)
		} else {
			addSet("task_id", *params.TaskID)
		}
	}
	if params.Enabled != nil {
		addSet("enabled", boolToInt(*params.Enabled))
	}

	// If cron_expr or timezone changes, re-validate and recompute next_fire_at.
	cronChanged := params.CronExpr != nil
	tzChanged := params.Timezone != nil
	if cronChanged || tzChanged {
		newExpr := existing.CronExpr
		if cronChanged {
			newExpr = *params.CronExpr
		}
		newTZ := existing.Timezone
		if tzChanged {
			newTZ = applyTimezoneDefault(*params.Timezone)
		}
		cron, cerr := callCronValidate(newExpr, newTZ)
		if cerr != nil {
			return failf("cron validate transport error: %s", cerr)
		}
		if !cron.Valid {
			return failf("invalid cron expression: %s", cron.Error)
		}
		if len(cron.NextFires) == 0 {
			return fail("cron validator returned no next fires")
		}
		if cronChanged {
			addSet("cron_expr", newExpr)
		}
		if tzChanged {
			addSet("timezone", newTZ)
		}
		addSet("next_fire_at", cron.NextFires[0])
	}

	if len(setClauses) == 0 {
		resultBytes, _ := json.Marshal(UpdateJobResult{Job: existing})
		pdk.OutputString(string(resultBytes))
		return 0
	}

	now := nowUTC()
	addSet("updated_at", now)

	sql := "UPDATE jobs SET " + joinComma(setClauses) + fmt.Sprintf(" WHERE id = ?%d", len(args)+1)
	args = append(args, params.ID)

	if _, err := callDBExec(sql, args); err != nil {
		return failf("db exec error: %s", err)
	}

	updated, err := loadJobByID(params.ID)
	if err != nil {
		return failf("%s", err)
	}
	emitLifecycle("updated", updated)

	resultBytes, _ := json.Marshal(UpdateJobResult{Job: updated})
	pdk.OutputString(string(resultBytes))
	return 0
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

//go:wasmexport delete_job
func deleteJob() int32 {
	var params DeleteJobParams
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	if params.ID <= 0 {
		return fail("id is required")
	}

	// Snapshot the job for the lifecycle event before we destroy it.
	job, err := loadJobByID(params.ID)
	if err != nil {
		return failf("%s", err)
	}

	// Count runs for the response. The FK on job_runs.job_id is declared with
	// ON DELETE CASCADE in schema.yaml and the loader opens the per-plugin DB
	// with `PRAGMA foreign_keys = ON`, so a single DELETE on the parent row
	// removes all child run rows atomically. No hand-rolled cascade.
	cntOut, cerr := callDBQuery("SELECT COUNT(*) FROM job_runs WHERE job_id = ?1", []any{params.ID})
	if cerr != nil {
		return failf("db query error: %s", cerr)
	}
	runsDeleted := int64(0)
	if len(cntOut.Rows) > 0 && len(cntOut.Rows[0]) > 0 {
		runsDeleted = toInt64(cntOut.Rows[0][0])
	}

	if _, err := callDBExec("DELETE FROM jobs WHERE id = ?1", []any{params.ID}); err != nil {
		return failf("db exec error: %s", err)
	}

	emitLifecycle("deleted", job)

	resultBytes, _ := json.Marshal(DeleteJobResult{Deleted: true, RunsDeleted: runsDeleted})
	pdk.OutputString(string(resultBytes))
	return 0
}

// validate_cron is a read-only MCP tool that echoes the mc_cron::validate
// host function to the caller. Phase D adds this so the New Job / Edit Job
// modal can live-preview the next 5 fires as the user types, without having
// to perform a destructive create_job + rollback dance.
//
//go:wasmexport validate_cron
func validateCron() int32 {
	var params cronValidateInput
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	resp, err := callCronValidate(params.Expression, params.Timezone)
	if err != nil {
		return failf("cron validate transport error: %s", err)
	}
	out, _ := json.Marshal(resp)
	pdk.OutputString(string(out))
	return 0
}

//go:wasmexport trigger_job_now
func triggerJobNow() int32 {
	var params TriggerJobNowParams
	if err := json.Unmarshal([]byte(pdk.InputString()), &params); err != nil {
		return failf("invalid input: %s", err)
	}
	if params.ID <= 0 {
		return fail("id is required")
	}

	// 1. Verify the job exists (loadJobByID errors otherwise).
	job, err := loadJobByID(params.ID)
	if err != nil {
		return failf("%s", err)
	}

	// 2. Pre-insert a placeholder run row so the caller has a stable run_id
	// to return immediately. The engine will PROMOTE this same row (refresh
	// triggered_at) rather than inserting a duplicate, thanks to the run_id
	// field on the trigger payload.
	now := nowUTC()
	execOut, err := callDBExec(
		`INSERT INTO job_runs (job_id, triggered_at, manual, status, error, duration_ms)
         VALUES (?1, ?2, 1, 'running', NULL, 0)`,
		[]any{job.ID, now},
	)
	if err != nil {
		return failf("db exec error: %s", err)
	}
	runID := execOut.LastInsertID

	// 3. Ask the engine to actually fire. The engine subscribes to this event
	// host-side, sees the run_id, and takes ownership of the pre-inserted row.
	emitEvent("scheduler.trigger.requested", TriggerRequestedPayload{
		JobID: job.ID,
		RunID: &runID,
	})

	resultBytes, _ := json.Marshal(TriggerJobNowResult{
		RunID:   runID,
		JobID:   job.ID,
		Message: "Job queued for immediate execution",
	})
	pdk.OutputString(string(resultBytes))
	return 0
}

//
// These three are the data_function targets declared in manifest.json. Each
// returns a JSON shape tailored to the page-type renderer in plugin.js:
//
//   - get_scheduler_week  → 7-day grid with hour slots + fires per slot
//   - get_scheduler_month → 6x7 calendar with per-day aggregates
//   - get_scheduler_list  → flat paginated list (reuses queryJobs)
//
// All three accept an optional { agent_id, anchor } filter. Anchor is a
// YYYY-MM-DD string in UTC; when empty it defaults to today.

type uiFilterParams struct {
	AgentID     string `json:"agent_id,omitempty"`
	Anchor      string `json:"anchor,omitempty"`
	EnabledOnly bool   `json:"enabled_only,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}

type uiWeekSlot struct {
	Hour int           `json:"hour"`
	Jobs []uiWeekEntry `json:"jobs"`
}

type uiWeekEntry struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	FireAt  string `json:"fire_at"`
	AgentID string `json:"agent_id"`
	Status  string `json:"status"` // "scheduled" | "fired" | "error" | "running"
	Enabled bool   `json:"enabled"`
}

// uiWeekDay is one day of the week view.
type uiWeekDay struct {
	Date    string       `json:"date"`    // YYYY-MM-DD
	Weekday string       `json:"weekday"` // Mon..Sun
	IsToday bool         `json:"is_today"`
	Slots   []uiWeekSlot `json:"slots"`
}

// uiWeekResult is the full week view payload.
type uiWeekResult struct {
	WeekStart   string      `json:"week_start"`
	WeekEnd     string      `json:"week_end"`
	AnchorToday string      `json:"anchor_today"`
	Days        []uiWeekDay `json:"days"`
	JobsTotal   int         `json:"jobs_total"`
}

// uiMonthDay is one cell of the month grid.
type uiMonthDay struct {
	Date           string `json:"date"`
	Day            int    `json:"day"`
	InMonth        bool   `json:"in_month"`
	IsToday        bool   `json:"is_today"`
	JobsCount      int    `json:"jobs_count"`
	HasErrorsToday bool   `json:"has_errors_today"`
}

// uiMonthResult is the full month view payload.
type uiMonthResult struct {
	MonthStart  string         `json:"month_start"`
	MonthEnd    string         `json:"month_end"`
	MonthLabel  string         `json:"month_label"` // e.g. "April 2026"
	AnchorToday string         `json:"anchor_today"`
	Weeks       [][]uiMonthDay `json:"weeks"`
	JobsTotal   int            `json:"jobs_total"`
}

// uiListResult is the list view payload. We return the raw DTOs and let the
// JS renderer format them; the plugin only sorts by next_fire_at ASC.
type uiListResult struct {
	Jobs   []JobDTO `json:"jobs"`
	Total  int      `json:"total"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
}

// anchorToday resolves the effective anchor date: explicit param wins, else
// today's UTC date from the host. On any parse error falls back to 1970-01-01
// which at least keeps the view functional rather than crashing.
func anchorToday(explicit string) (y, m, d int, nowDate string) {
	now := nowUTC()
	ny, nm, nd, ok := parseDate(now)
	if !ok {
		ny, nm, nd = 1970, 1, 1
	}
	if explicit == "" {
		return ny, nm, nd, formatDate(ny, nm, nd)
	}
	ay, am, ad, aok := parseDate(explicit)
	if !aok {
		return ny, nm, nd, formatDate(ny, nm, nd)
	}
	return ay, am, ad, formatDate(ny, nm, nd)
}

// loadEnabledJobsForUI loads all enabled jobs matching the agent filter.
// Used by both week and month views to bound the cron fire computation.
func loadEnabledJobsForUI(agentID string) ([]JobDTO, error) {
	jobs, _, err := queryJobs(ListJobsParams{EnabledOnly: true, AgentID: agentID}, "id ASC")
	return jobs, err
}

// Returns fires inside the given 7-day window. The host validator returns at
// most 5 upcoming fires per job, so frequent schedules lose resolution past
// the first few. The list view is the canonical source of truth.
func collectWeekFires(jobs []JobDTO, loY, loM, loD, hiY, hiM, hiD int) map[string][]uiWeekEntry {
	out := make(map[string][]uiWeekEntry)
	for _, job := range jobs {
		resp, err := callCronValidate(job.CronExpr, job.Timezone)
		if err != nil || !resp.Valid {
			continue
		}
		for _, f := range resp.NextFires {
			fy, fm, fd, ok := parseDate(f)
			if !ok {
				continue
			}
			if !dateInRange(fy, fm, fd, loY, loM, loD, hiY, hiM, hiD) {
				continue
			}
			dateKey := formatDate(fy, fm, fd)
			out[dateKey] = append(out[dateKey], uiWeekEntry{
				ID:      job.ID,
				Name:    job.Name,
				FireAt:  f,
				AgentID: job.AssigneeAgentID,
				Status:  "scheduled",
				Enabled: job.Enabled,
			})
		}
	}
	return out
}

// triggered_at is RFC3339 starting with YYYY-MM-DD, so string comparison is safe.
func queryRunsInRange(loDate, hiDate string) ([]JobRunDTO, error) {
	out, err := callDBQuery(
		"SELECT "+jobRunColumns+" FROM job_runs WHERE triggered_at >= ?1 AND triggered_at <= ?2 ORDER BY triggered_at ASC",
		[]any{loDate + "T00:00:00Z", hiDate + "T23:59:59Z"},
	)
	if err != nil {
		return nil, err
	}
	runs := make([]JobRunDTO, 0, len(out.Rows))
	for _, row := range out.Rows {
		r, rerr := rowToJobRun(out.Columns, row)
		if rerr != nil {
			return nil, rerr
		}
		runs = append(runs, r)
	}
	return runs, nil
}

//go:wasmexport get_scheduler_week
func getSchedulerWeek() int32 {
	var params uiFilterParams
	_ = json.Unmarshal([]byte(pdk.InputString()), &params)

	anchorY, anchorM, anchorD, todayDate := anchorToday(params.Anchor)
	loY, loM, loD := weekStart(anchorY, anchorM, anchorD)
	hiY, hiM, hiD := addDays(loY, loM, loD, 6)

	jobs, err := loadEnabledJobsForUI(params.AgentID)
	if err != nil {
		return failf("%s", err)
	}

	firesByDate := collectWeekFires(jobs, loY, loM, loD, hiY, hiM, hiD)

	// Day-level overlay: any job with an error today is shown as error,
	// otherwise as fired. Coarse but sufficient for the calendar preview.
	runs, rerr := queryRunsInRange(formatDate(loY, loM, loD), formatDate(hiY, hiM, hiD))
	if rerr == nil {
		type dateJobKey struct {
			date  string
			jobID int64
		}
		statusByKey := make(map[dateJobKey]string)
		for _, r := range runs {
			y, m, d, ok := parseDate(r.TriggeredAt)
			if !ok {
				continue
			}
			key := dateJobKey{date: formatDate(y, m, d), jobID: r.JobID}
			// Severity: error > running > fired (success).
			sev := "fired"
			switch r.Status {
			case "error":
				sev = "error"
			case "running":
				sev = "running"
			}
			existing := statusByKey[key]
			if existing == "error" {
				continue // already worst
			}
			if existing == "running" && sev == "fired" {
				continue
			}
			statusByKey[key] = sev
		}
		for dateKey, entries := range firesByDate {
			for i := range entries {
				k := dateJobKey{date: dateKey, jobID: entries[i].ID}
				if s, ok := statusByKey[k]; ok {
					entries[i].Status = s
				}
			}
			firesByDate[dateKey] = entries
		}
	}

	// Build 7 days × 24 hour slots.
	days := make([]uiWeekDay, 0, 7)
	for i := 0; i < 7; i++ {
		dy, dm, dd := addDays(loY, loM, loD, i)
		dateKey := formatDate(dy, dm, dd)
		day := uiWeekDay{
			Date:    dateKey,
			Weekday: weekdayShort(dayOfWeekISO(dy, dm, dd)),
			IsToday: dateKey == todayDate,
			Slots:   make([]uiWeekSlot, 24),
		}
		for h := 0; h < 24; h++ {
			day.Slots[h] = uiWeekSlot{Hour: h}
		}
		for _, entry := range firesByDate[dateKey] {
			h, ok := parseHour(entry.FireAt)
			if !ok {
				continue
			}
			day.Slots[h].Jobs = append(day.Slots[h].Jobs, entry)
		}
		days = append(days, day)
	}

	result := uiWeekResult{
		WeekStart:   formatDate(loY, loM, loD),
		WeekEnd:     formatDate(hiY, hiM, hiD),
		AnchorToday: todayDate,
		Days:        days,
		JobsTotal:   len(jobs),
	}
	resultBytes, _ := json.Marshal(result)
	pdk.OutputString(string(resultBytes))
	return 0
}

//go:wasmexport get_scheduler_month
func getSchedulerMonth() int32 {
	var params uiFilterParams
	_ = json.Unmarshal([]byte(pdk.InputString()), &params)

	anchorY, anchorM, anchorD, todayDate := anchorToday(params.Anchor)
	_ = anchorD

	jobs, err := loadEnabledJobsForUI(params.AgentID)
	if err != nil {
		return failf("%s", err)
	}

	// Always 6 weeks (42 cells) so the grid stays stable across months.
	firstY, firstM, firstD := anchorY, anchorM, 1
	gridY, gridM, gridD := weekStart(firstY, firstM, firstD)

	dim := daysInMonth(anchorY, anchorM)
	lastY, lastM, lastD := anchorY, anchorM, dim

	// Expand the fire window to the full grid so cells outside the month
	// still get counts (for visual continuity).
	gridHiY, gridHiM, gridHiD := addDays(gridY, gridM, gridD, 41)
	fires := collectWeekFires(jobs, gridY, gridM, gridD, gridHiY, gridHiM, gridHiD)

	runs, _ := queryRunsInRange(formatDate(gridY, gridM, gridD), formatDate(gridHiY, gridHiM, gridHiD))
	errorDates := make(map[string]bool)
	for _, r := range runs {
		if r.Status == "error" {
			if y, m, d, ok := parseDate(r.TriggeredAt); ok {
				errorDates[formatDate(y, m, d)] = true
			}
		}
	}

	weeks := make([][]uiMonthDay, 0, 6)
	cursorY, cursorM, cursorD := gridY, gridM, gridD
	for w := 0; w < 6; w++ {
		row := make([]uiMonthDay, 0, 7)
		for i := 0; i < 7; i++ {
			dateKey := formatDate(cursorY, cursorM, cursorD)
			inMonth := dateInRange(cursorY, cursorM, cursorD, firstY, firstM, firstD, lastY, lastM, lastD)
			row = append(row, uiMonthDay{
				Date:           dateKey,
				Day:            cursorD,
				InMonth:        inMonth,
				IsToday:        dateKey == todayDate,
				JobsCount:      len(fires[dateKey]),
				HasErrorsToday: errorDates[dateKey],
			})
			cursorY, cursorM, cursorD = addDays(cursorY, cursorM, cursorD, 1)
		}
		weeks = append(weeks, row)
	}

	result := uiMonthResult{
		MonthStart:  formatDate(firstY, firstM, firstD),
		MonthEnd:    formatDate(lastY, lastM, lastD),
		MonthLabel:  monthLabel(anchorY, anchorM),
		AnchorToday: todayDate,
		Weeks:       weeks,
		JobsTotal:   len(jobs),
	}
	resultBytes, _ := json.Marshal(result)
	pdk.OutputString(string(resultBytes))
	return 0
}

// monthLabel returns "Month YYYY" for display.
func monthLabel(year, month int) string {
	names := []string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	if month < 1 || month > 12 {
		return ""
	}
	return fmt.Sprintf("%s %d", names[month], year)
}

//go:wasmexport get_scheduler_list
func getSchedulerList() int32 {
	var params uiFilterParams
	_ = json.Unmarshal([]byte(pdk.InputString()), &params)

	listParams := ListJobsParams{
		AgentID:     params.AgentID,
		EnabledOnly: params.EnabledOnly,
		Limit:       params.Limit,
		Offset:      params.Offset,
	}
	// Sort by next_fire_at ASC so the operator sees "what's coming up next".
	// SQLite sorts NULLs first by default; push them last so empty/disabled
	// jobs don't shadow the real schedule.
	jobs, total, err := queryJobs(listParams, "next_fire_at IS NULL, next_fire_at ASC, id ASC")
	if err != nil {
		return failf("%s", err)
	}
	result := uiListResult{Jobs: jobs, Total: total, Limit: listParams.Limit, Offset: listParams.Offset}
	resultBytes, _ := json.Marshal(result)
	pdk.OutputString(string(resultBytes))
	return 0
}

func main() {}
