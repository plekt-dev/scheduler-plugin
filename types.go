package main

type JobDTO struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	CronExpr        string  `json:"cron_expr"`
	Timezone        string  `json:"timezone"`
	Prompt          string  `json:"prompt"`
	AssigneeAgentID string  `json:"assignee_agent_id"`
	TaskID          *int64  `json:"task_id,omitempty"`
	Delivery        string  `json:"delivery"`
	Enabled         bool    `json:"enabled"`
	NextFireAt      *string `json:"next_fire_at,omitempty"`
	LastRunAt       *string `json:"last_run_at,omitempty"`
	LastRunStatus   *string `json:"last_run_status,omitempty"`
	LastError       *string `json:"last_error,omitempty"`
	LastDurationMs  *int64  `json:"last_duration_ms,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type JobRunDTO struct {
	ID             int64   `json:"id"`
	JobID          int64   `json:"job_id"`
	TriggeredAt    string  `json:"triggered_at"`
	Manual         bool    `json:"manual"`
	Status         string  `json:"status"`
	Error          *string `json:"error,omitempty"`
	DurationMs     int64   `json:"duration_ms"`
	Output         *string `json:"output,omitempty"`
	DispatchStatus string  `json:"dispatch_status,omitempty"`
}

type ListJobsParams struct {
	EnabledOnly bool   `json:"enabled_only,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}

type ListJobsResult struct {
	Jobs  []JobDTO `json:"jobs"`
	Total int      `json:"total"`
}

type GetJobParams struct {
	ID int64 `json:"id"`
}

type GetJobResult struct {
	Job        JobDTO      `json:"job"`
	RecentRuns []JobRunDTO `json:"recent_runs"`
}

// Enabled is *bool so callers can omit it (defaults to true) or pass false explicitly.
type CreateJobParams struct {
	Name     string `json:"name"`
	CronExpr string `json:"cron_expr"`
	Prompt   string `json:"prompt"`
	AgentID  string `json:"agent_id"`
	Timezone string `json:"timezone,omitempty"`
	TaskID   *int64 `json:"task_id,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type CreateJobResult struct {
	Job JobDTO `json:"job"`
}

// All fields besides ID are pointers for true partial updates.
// TaskID = -1 clears the task linkage (sets task_id to NULL).
type UpdateJobParams struct {
	ID       int64   `json:"id"`
	Name     *string `json:"name,omitempty"`
	CronExpr *string `json:"cron_expr,omitempty"`
	Prompt   *string `json:"prompt,omitempty"`
	AgentID  *string `json:"agent_id,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	TaskID   *int64  `json:"task_id,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

type UpdateJobResult struct {
	Job JobDTO `json:"job"`
}

type DeleteJobParams struct {
	ID int64 `json:"id"`
}

type DeleteJobResult struct {
	Deleted     bool  `json:"deleted"`
	RunsDeleted int64 `json:"runs_deleted"`
}

type TriggerJobNowParams struct {
	ID int64 `json:"id"`
}

type TriggerJobNowResult struct {
	RunID   int64  `json:"run_id"`
	JobID   int64  `json:"job_id"`
	Message string `json:"message"`
}

type JobLifecyclePayload struct {
	JobID     int64  `json:"job_id"`
	JobName   string `json:"job_name"`
	ChangedBy string `json:"changed_by"`
	Action    string `json:"action"`
}

// When RunID is set, the plugin pre-allocated a job_runs row and the engine
// promotes that same row instead of inserting a new one. This guarantees one
// row per manual trigger and keeps the run id returned to MCP valid end-to-end.
type TriggerRequestedPayload struct {
	JobID int64  `json:"job_id"`
	RunID *int64 `json:"run_id,omitempty"`
}

type dbQueryInput struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

type dbQueryOutput struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Error   string   `json:"error,omitempty"`
}

type dbExecInput struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

type dbExecOutput struct {
	RowsAffected int64  `json:"rows_affected"`
	LastInsertID int64  `json:"last_insert_id"`
	Error        string `json:"error,omitempty"`
}

type emitInput struct {
	EventName string `json:"event_name"`
	Payload   any    `json:"payload"`
}

type cronValidateInput struct {
	Expression string `json:"expression"`
	Timezone   string `json:"timezone,omitempty"`
}

type CronValidateResponse struct {
	Valid     bool     `json:"valid"`
	Error     string   `json:"error,omitempty"`
	NextFires []string `json:"next_fires,omitempty"`
}

type hostError struct {
	Error string `json:"error"`
}

const DefaultTimezone = "Europe/Berlin"

const DeliveryEventBus = "eventbus"

const jobColumns = `id, name, cron_expr, timezone, prompt, agent_id, task_id, delivery, enabled, next_fire_at, last_run_at, last_run_status, last_error, last_duration_ms, created_at, updated_at`

const jobRunColumns = `id, job_id, triggered_at, manual, status, error, duration_ms, output, dispatch_status`
