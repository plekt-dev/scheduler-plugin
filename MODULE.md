# scheduler-plugin

Cron for humans, MCP for agents. Schedule recurring prompts, dispatch them
to chosen agents, track every fire and run.

A human operator (or another agent via MCP) defines a job: cron expression,
timezone, prompt, target agent. The plugin owns the data layer (`jobs`,
`job_runs`); the actual schedule firing is performed by the core scheduler
engine so timing accuracy is not subject to WASM clock quirks.

## Overview

- **Version:** 1.0.0
- **License:** MIT
- **Frontend:** `plugin.js`, `plugin.css` (page-scoped)

## Pieces

- **Core engine** (`internal/scheduler`): tick loop, semaphore-bounded
  firings, cron next-fire computation, EventBus subscription that turns
  `scheduler.trigger.requested` into a manual fire.
- **This plugin**: CRUD over the two SQLite tables, manifest pages
  (`scheduler_week` / `_month` / `_list`), MCP tool surface.
- **Host function `mc_cron::validate`**: single source of truth for cron
  parsing. The plugin calls it on every create/update so the persisted
  `next_fire_at` stays engine-consistent.

## MCP tools (7)

| Tool              | Purpose                                                                                                                                                      |
|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `list_jobs`       | Filter by `enabled_only`, `agent_id`; paginate with `limit`/`offset`. Returns `{jobs, total}`                                                                |
| `get_job`         | One job + its 10 most recent runs                                                                                                                            |
| `create_job`      | Validates cron, computes first fire, inserts, emits `scheduler.job.created`                                                                                  |
| `update_job`      | Partial update. `task_id=-1` clears linkage. Cron/timezone change triggers re-validation                                                                     |
| `delete_job`      | Removes a job; `ON DELETE CASCADE` on `job_runs.job_id` removes its history                                                                                  |
| `trigger_job_now` | Pre-allocates a `job_runs` row and emits `scheduler.trigger.requested` with its `run_id`; the engine promotes that row (exactly one row per manual trigger)  |
| `validate_cron`   | Read-only preview. Returns up to five upcoming fire times for a given expression and timezone                                                                |

## Events

- **emits**: `scheduler.job.created`, `scheduler.job.updated`,
  `scheduler.job.deleted`, `scheduler.trigger.requested`.
- **subscribes**: `scheduler.job.fired`, `scheduler.run.completed` (used by
  the UI to update "last fired" badges).

## Schema notes

`job_runs.job_id` is a foreign key to `jobs.id` with `ON DELETE CASCADE`.
The loader opens every per-plugin SQLite database with
`PRAGMA foreign_keys = ON`, so deleting a parent `jobs` row automatically
removes its run history in one statement. `delete_job` relies on that
cascade: it issues a single `DELETE FROM jobs` and returns the pre-query
run count for the response.

## Build

```bash
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
```

Requires the Extism Go PDK (`github.com/extism/go-pdk`).
