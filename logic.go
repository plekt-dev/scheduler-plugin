package main

import (
	"fmt"
	"regexp"
	"strings"
)

var toolNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

var jobNameRe = regexp.MustCompile(`^[A-Za-z0-9 _\-\.]{1,120}$`)

func validateCreateJob(p CreateJobParams) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !jobNameRe.MatchString(p.Name) {
		return fmt.Errorf("name %q has invalid characters or length (allowed: A-Z a-z 0-9 _ - . space, 1..120)", p.Name)
	}
	if strings.TrimSpace(p.CronExpr) == "" {
		return fmt.Errorf("cron_expr is required")
	}
	if strings.TrimSpace(p.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	return nil
}

func rowToJob(cols []string, row []any) (JobDTO, error) {
	if len(row) != len(cols) {
		return JobDTO{}, fmt.Errorf("rowToJob: %d cols vs %d row values", len(cols), len(row))
	}
	idx := indexCols(cols)
	get := func(name string) any {
		i, ok := idx[name]
		if !ok {
			return nil
		}
		return row[i]
	}
	j := JobDTO{
		ID:              toInt64(get("id")),
		Name:            toString(get("name")),
		CronExpr:        toString(get("cron_expr")),
		Timezone:        toString(get("timezone")),
		Prompt:          toString(get("prompt")),
		AssigneeAgentID: toString(get("agent_id")),
		Delivery:        toString(get("delivery")),
		Enabled:         toInt64(get("enabled")) != 0,
		CreatedAt:       toString(get("created_at")),
		UpdatedAt:       toString(get("updated_at")),
	}
	if v := get("task_id"); v != nil {
		id := toInt64(v)
		j.TaskID = &id
	}
	if v := get("next_fire_at"); v != nil {
		s := toString(v)
		j.NextFireAt = &s
	}
	if v := get("last_run_at"); v != nil {
		s := toString(v)
		j.LastRunAt = &s
	}
	if v := get("last_run_status"); v != nil {
		s := toString(v)
		j.LastRunStatus = &s
	}
	if v := get("last_error"); v != nil {
		s := toString(v)
		j.LastError = &s
	}
	if v := get("last_duration_ms"); v != nil {
		d := toInt64(v)
		j.LastDurationMs = &d
	}
	if j.Delivery == "" {
		j.Delivery = DeliveryEventBus
	}
	return j, nil
}

// rowToJobRun projects one row of the job_runs table into a JobRunDTO.
func rowToJobRun(cols []string, row []any) (JobRunDTO, error) {
	if len(row) != len(cols) {
		return JobRunDTO{}, fmt.Errorf("rowToJobRun: %d cols vs %d row values", len(cols), len(row))
	}
	idx := indexCols(cols)
	get := func(name string) any {
		i, ok := idx[name]
		if !ok {
			return nil
		}
		return row[i]
	}
	r := JobRunDTO{
		ID:          toInt64(get("id")),
		JobID:       toInt64(get("job_id")),
		TriggeredAt: toString(get("triggered_at")),
		Manual:      toInt64(get("manual")) != 0,
		Status:      toString(get("status")),
		DurationMs:  toInt64(get("duration_ms")),
	}
	if v := get("error"); v != nil {
		s := toString(v)
		r.Error = &s
	}
	if v := get("output"); v != nil {
		s := toString(v)
		if s != "" {
			r.Output = &s
		}
	}
	r.DispatchStatus = toString(get("dispatch_status"))
	return r, nil
}

// indexCols returns a name → position map for the given column slice.
func indexCols(cols []string) map[string]int {
	m := make(map[string]int, len(cols))
	for i, c := range cols {
		m[c] = i
	}
	return m
}

// toInt64 coerces a JSON-decoded value to int64. SQLite via the host fn returns
// numbers as float64 by default; some drivers may also yield int64 directly.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case uint64:
		return int64(n)
	case nil:
		return 0
	default:
		return 0
	}
}

// toString coerces a JSON-decoded value to a string. NULL becomes "".
func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

// boolToInt converts a Go bool to the 0/1 integer SQLite expects.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// applyEnabledDefault returns true when the caller did not provide an explicit
// `enabled` flag, and the verbatim value otherwise.
func applyEnabledDefault(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

// applyTimezoneDefault returns DefaultTimezone for an empty timezone string.
func applyTimezoneDefault(tz string) string {
	if strings.TrimSpace(tz) == "" {
		return DefaultTimezone
	}
	return tz
}

// time package is unreliable in WASM, so the date helpers are hand-rolled.

func parseDate(s string) (year int, month int, day int, ok bool) {
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return 0, 0, 0, false
	}
	y, ey := parseUint(s[0:4])
	m, em := parseUint(s[5:7])
	d, ed := parseUint(s[8:10])
	if ey != nil || em != nil || ed != nil {
		return 0, 0, 0, false
	}
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, 0, 0, false
	}
	return y, m, d, true
}

// parseHour extracts the hour part (0-23) from an RFC3339 timestamp. Returns
// (0, false) when the input is not at least "YYYY-MM-DDTHH".
func parseHour(s string) (int, bool) {
	if len(s) < 13 || s[10] != 'T' {
		return 0, false
	}
	h, err := parseUint(s[11:13])
	if err != nil || h > 23 {
		return 0, false
	}
	return h, true
}

// parseUint parses a non-negative ASCII decimal integer without pulling in
// strconv (keeps WASM binary size down).
func parseUint(s string) (int, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", c)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// daysInMonth returns the number of days in the given month (1-12) using the
// Gregorian leap rule.
func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if (year%4 == 0 && year%100 != 0) || year%400 == 0 {
			return 29
		}
		return 28
	}
	return 0
}

// dayOfWeekISO returns 1=Monday..7=Sunday for any Gregorian date. Uses
// Zeller's congruence so we never touch the time package.
func dayOfWeekISO(year, month, day int) int {
	q := day
	m := month
	y := year
	if m < 3 {
		m += 12
		y -= 1
	}
	k := y % 100
	j := y / 100
	// h=0 Sat, 1 Sun, 2 Mon, ..., 6 Fri.
	h := (q + (13*(m+1))/5 + k + k/4 + j/4 + 5*j) % 7
	return ((h + 5) % 7) + 1
}

// addDays returns (year, month, day) + delta days, handling month/year wrap.
func addDays(year, month, day, delta int) (int, int, int) {
	day += delta
	for day < 1 {
		month--
		if month < 1 {
			month = 12
			year--
		}
		day += daysInMonth(year, month)
	}
	for {
		dim := daysInMonth(year, month)
		if day <= dim {
			break
		}
		day -= dim
		month++
		if month > 12 {
			month = 1
			year++
		}
	}
	return year, month, day
}

// formatDate returns YYYY-MM-DD for integer components.
func formatDate(year, month, day int) string {
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// weekStart returns the Monday (ISO day 1) of the week containing the given date.
func weekStart(year, month, day int) (int, int, int) {
	iso := dayOfWeekISO(year, month, day)
	return addDays(year, month, day, -(iso - 1))
}

// weekdayShort returns a 3-letter English weekday name for an ISO day number.
func weekdayShort(iso int) string {
	switch iso {
	case 1:
		return "Mon"
	case 2:
		return "Tue"
	case 3:
		return "Wed"
	case 4:
		return "Thu"
	case 5:
		return "Fri"
	case 6:
		return "Sat"
	case 7:
		return "Sun"
	}
	return "?"
}

// dateInRange reports whether Y/M/D `a` is in [lo, hi] inclusive.
func dateInRange(ay, am, ad, loy, lom, lod, hiy, him, hid int) bool {
	if ay < loy || (ay == loy && am < lom) || (ay == loy && am == lom && ad < lod) {
		return false
	}
	if ay > hiy || (ay == hiy && am > him) || (ay == hiy && am == him && ad > hid) {
		return false
	}
	return true
}
