# Code Runner Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve the three actionable Code Runner review findings without changing the intended hidden lifecycle UX.

**Architecture:** Coordinate Code Runner runtime transitions with an optimistic database condition over the current sandbox ID and state, so a stale Run or kill request cannot overwrite a newer lifecycle decision. Carry the 30-minute Code Runner TTL into the runtime Connect call and refresh it with a cancellation-independent bounded context. Replace full `CodeRun` loading in the sessions endpoint with a summary projection.

**Tech Stack:** Go 1.26, GORM, PostgreSQL/MySQL/SQLite-compatible queries, fox-gonic, React 19 tests, Vitest.

## Global Constraints

- Preserve `Handler -> Service -> Entity` layering.
- Keep Run recovery automatic when the sandbox expired normally.
- Never recreate after a newer explicit kill or sandbox replacement.
- Keep Code Runner sandbox TTL at 1800 seconds; do not fall back to the 86400-second workspace default.
- Do not expose or load code, stdin, stdout, stderr, or metadata for the sessions summary.
- Preserve the current dirty checkout and do not commit or push.

---

### Task 1: Make Run and kill transitions concurrency-safe

**Files:**
- Modify: `internal/service/code_runner.go`
- Modify: `internal/handler/code_runner.go`
- Modify: `internal/handler/code_runner_test.go`
- Modify: `internal/handler/oauth_test.go`

**Interfaces:**
- Produces: `service.ErrCodeRunnerSessionChanged`
- Produces: `service.CodeRunnerSessionCondition`
- Produces: `(*Service).UpdateCodeRunnerSessionWithSandboxIfCurrent(...)`
- Consumes: the session snapshot loaded before an external sandbox lifecycle request

- [x] **Step 1: Write failing handler regressions**

Add a Run regression that changes the stored session to `killed` while a replacement sandbox is being prepared. Assert that Run returns HTTP 409, the killed state remains authoritative, the replacement sandbox is shortened to 60 seconds, and no run is saved.

Add a kill regression whose first external kill races with a simulated replacement from `sandbox-old` to `sandbox-new`. Assert that the handler retries against `sandbox-new` and persists it as killed instead of overwriting the session with stale `sandbox-old` data.

- [x] **Step 2: Run the regressions and verify RED**

Run:

```bash
go test ./internal/handler -run 'Test(RunCodeDoesNotRestoreSandboxAfterConcurrentKill|KillCodeRunnerSessionFollowsConcurrentReplacement)' -count=1
```

Expected: FAIL because recreation and kill both persist stale snapshots unconditionally.

- [x] **Step 3: Implement optimistic runtime updates**

Add a service method that updates a Code Runner session only when `account_id`, `id`, `sandbox_id`, and `state` still match the caller's snapshot. Perform the conditional session update and sandbox-session upsert in one transaction; return `ErrCodeRunnerSessionChanged` when `RowsAffected != 1`.

Use it from recreation. On a stale snapshot, shorten the newly created sandbox lifetime and return HTTP 409. Before recovering a 404 from a previously running sandbox, reload the session and reject recovery when sandbox ID or state changed.

Use the same conditional update in kill. If the snapshot changed, reload once and kill the current sandbox before persisting `killed`.

- [x] **Step 4: Run the focused handler tests and verify GREEN**

Run the command from Step 2 and require both tests to pass.

### Task 2: Preserve the 30-minute TTL through Run

**Files:**
- Modify: `internal/handler/sandbox_runtime.go`
- Modify: `internal/handler/code_runner.go`
- Modify: `internal/handler/code_runner_test.go`
- Modify: `internal/handler/oauth_test.go`

**Interfaces:**
- Extends: `sandboxRuntimeCommandRequest.SandboxTimeoutSeconds int32`
- Consumes: `codeRunnerSandboxTimeoutSeconds`

- [x] **Step 1: Write the failing cancellation regression**

Make the fake RunCommand cancel the HTTP request after producing a successful result. Assert that the command request contains `SandboxTimeoutSeconds == 1800`, and that the subsequent SetTimeout call receives a live context rather than the canceled request context.

- [x] **Step 2: Run the regression and verify RED**

Run:

```bash
go test ./internal/handler -run TestRunCodeKeepsThirtyMinuteTTLWhenRequestIsCanceled -count=1
```

Expected: FAIL because the command request has no sandbox TTL and SetTimeout sees the canceled context.

- [x] **Step 3: Carry and refresh the TTL safely**

Add `SandboxTimeoutSeconds` to the command request and use it in `qiniuSandboxRuntime.RunCommand` when connecting, falling back to the workspace default only for callers that omit it. Pass 1800 from RunCode. Refresh after the command with `codeRunnerWriteContext`, and log a warning when refresh fails without discarding an already completed code result.

- [x] **Step 4: Run the focused test and verify GREEN**

Run the command from Step 2 and require it to pass.

### Task 3: Project only latest-run summary columns

**Files:**
- Modify: `internal/service/code_runner.go`
- Modify: `internal/handler/code_runner_test.go`

**Interfaces:**
- Produces: `service.CodeRunSummary`
- Changes: `LatestCodeRuns(context.Context, string) (map[string]CodeRunSummary, error)`

- [x] **Step 1: Add a failing query projection assertion**

Instrument the sessions handler test with a temporary GORM query callback. Capture the `code_runs` query selections and assert that it explicitly selects only `session_id`, `language`, `exit_code`, `error`, `duration_ms`, and `created_at`.

- [x] **Step 2: Run the test and verify RED**

Run:

```bash
go test ./internal/handler -run TestCodeRunnerSessionsIncludesLatestRunSummary -count=1
```

Expected: FAIL because the current query has no projection and loads all CodeRun columns.

- [x] **Step 3: Add the projection type and Select list**

Define a purpose-built summary type with only the six required columns. Query into that type with an explicit Select list while retaining the correlated `NOT EXISTS` latest-row selection.

- [x] **Step 4: Run the focused test and verify GREEN**

Run the command from Step 2 and require it to pass.

### Task 4: Full verification

**Files:**
- Verify all files modified above.

- [x] **Step 1: Run `task check`**
- [x] **Step 2: Run `task test`**
- [x] **Step 3: Run `git diff --check` and review the complete diff**
- [x] **Step 4: Reload the local Code Runner list and confirm the summary still renders without console errors**
