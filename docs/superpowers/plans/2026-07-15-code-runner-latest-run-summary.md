# Code Runner Latest Run Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show each Code Runner session's latest language, outcome, duration, and execution time without exposing code, output, or sandbox lifecycle state.

**Architecture:** Extend the existing sessions response with an optional, purpose-built `latest_run` summary. Load all latest runs for the authenticated account in one cross-database query, map them to sessions in the handler, and render the summary as a compact second-level list detail. Sessions with no run receive no summary and display `Not run yet`.

**Tech Stack:** Go 1.26, GORM, SQLite/PostgreSQL/MySQL-compatible SQL, React 19, TypeScript 6, React Query, Radix Themes, Vitest.

## Global Constraints

- Preserve `Handler -> Service -> Entity` layering.
- Use one batched latest-run query; do not issue one query per session.
- The session list response must not include run code, stdin, stdout, stderr, or error text.
- Define success as `exit_code == 0 && error == ""`; stderr alone does not turn a successful process into failure.
- Keep sandbox state, heartbeat, kill, and Run recovery behavior unchanged and hidden from the list UI.
- Keep the current working tree uncommitted unless the user explicitly asks to commit.

---

### Task 1: Add a batched latest-run summary to the sessions API

**Files:**
- Modify: `internal/service/code_runner.go`
- Modify: `internal/handler/code_runner.go`
- Test: `internal/handler/code_runner_test.go`

**Interfaces:**
- Produces: `(*Service).LatestCodeRuns(context.Context, accountID string) (map[string]entity.CodeRun, error)`
- Produces: `codeRunnerLatestRunResponse`
- Extends: `codeRunnerSessionResponse.LatestRun *codeRunnerLatestRunResponse`

- [x] **Step 1: Write the failing sessions API test**

Create two sessions, save two differently timed runs for one session, request `GET /api/v1/code-runner/sessions`, find sessions by ID, and assert:

```go
if withRun.LatestRun == nil ||
	withRun.LatestRun.Language != "python" ||
	withRun.LatestRun.Succeeded ||
	withRun.LatestRun.DurationMS != 3842 {
	t.Fatalf("latest run = %+v, want latest failed python run", withRun.LatestRun)
}
if withoutRun.LatestRun != nil {
	t.Fatalf("latest run = %+v, want omitted", withoutRun.LatestRun)
}
for _, forbidden := range []string{`"code"`, `"stdin"`, `"stdout"`, `"stderr"`, `"error"`} {
	if strings.Contains(rec.Body.String(), forbidden) {
		t.Fatalf("sessions response contains %s", forbidden)
	}
}
```

- [x] **Step 2: Run the test and verify RED**

Run:

```bash
go test ./internal/handler -run TestCodeRunnerSessionsIncludesLatestRunSummary -count=1
```

Expected: FAIL to compile because `LatestRun` and its response type do not exist.

- [x] **Step 3: Implement the single-query service method**

Use a correlated `NOT EXISTS` subquery so PostgreSQL, MySQL, and SQLite select one run per session using the repository's existing ordering contract `(created_at DESC, id DESC)`:

```go
func (s *Service) LatestCodeRuns(ctx context.Context, accountID string) (map[string]entity.CodeRun, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	newer := s.db.Table("code_runs AS newer").
		Select("1").
		Where("newer.account_id = code_runs.account_id").
		Where("newer.session_id = code_runs.session_id").
		Where("newer.deleted_at IS NULL").
		Where("(newer.created_at > code_runs.created_at OR (newer.created_at = code_runs.created_at AND newer.id > code_runs.id))")
	var runs []entity.CodeRun
	if err := s.db.WithContext(ctx).
		Where("code_runs.account_id = ?", accountID).
		Where("NOT EXISTS (?)", newer).
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list latest code runs: %w", err)
	}
	latest := make(map[string]entity.CodeRun, len(runs))
	for _, run := range runs {
		latest[run.SessionID] = run
	}
	return latest, nil
}
```

- [x] **Step 4: Add the response summary and handler mapping**

```go
type codeRunnerLatestRunResponse struct {
	Language   string    `json:"language"`
	Succeeded  bool      `json:"succeeded"`
	DurationMS int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

// In CodeRunnerSessions, after loading sessions:
latestRuns, err := ctrl.service.LatestCodeRuns(c.Request.Context(), accountID)
if err != nil {
	return err
}
// Map only the four summary fields; never reuse codeRunResponse here.
```

- [x] **Step 5: Run the focused handler tests and verify GREEN**

Run:

```bash
go test ./internal/handler -run 'Test(CodeRunnerSessionsIncludesLatestRunSummary|CreateCodeRunnerSession)' -count=1
```

Expected: PASS.

### Task 2: Render the latest run summary in the session list

**Files:**
- Modify: `website/src/api/code-runner.ts`
- Modify: `website/src/views/code-runner/index.tsx`
- Test: `website/src/views/code-runner/index.test.tsx`

**Interfaces:**
- Consumes: session `latest_run`
- Produces: `CodeRunnerLatestRun`
- Produces: `formatCodeRunDuration(durationMS: number): string`

- [x] **Step 1: Write the failing list UI test**

Provide one session with a synthetic `latest_run` and one session without it, then assert the rendered list contains `Python`, `Succeeded`, `3.8 s`, and `Not run yet`, and no longer contains `Updated`.

```ts
expect(container.textContent).toContain('Python')
expect(container.textContent).toContain('Succeeded')
expect(container.textContent).toContain('3.8 s')
expect(container.textContent).toContain('Not run yet')
expect(container.textContent).not.toContain('Updated')
```

- [x] **Step 2: Run the test and verify RED**

Run:

```bash
cd website && npm test -- src/views/code-runner/index.test.tsx
```

Expected: FAIL because the list still renders `Updated` and ignores `latest_run`.

- [x] **Step 3: Add the frontend summary type and compact rendering**

```ts
export interface CodeRunnerLatestRun {
  language: CodeRunnerLanguage
  succeeded: boolean
  duration_ms: number
  created_at: string
}

// Add to CodeRunnerSession:
latest_run?: CodeRunnerLatestRun
```

Render the session name plus a neutral language `Badge`. Render `Succeeded` or `Failed`, a humanized duration, and `formatDateTime(created_at)` in the trailing metadata area. Render `Not run yet` when `latest_run` is absent. Do not render run code or output.

- [x] **Step 4: Run the focused frontend test and verify GREEN**

Run:

```bash
cd website && npm test -- src/views/code-runner/index.test.tsx
```

Expected: all Code Runner page tests pass.

### Task 3: Full verification and browser smoke test

**Files:**
- Verify all files modified in Tasks 1-2.

- [x] **Step 1: Run repository checks**

Run `task check` and require exit status 0.

- [x] **Step 2: Run all tests**

Run `task test` and require all Go and Vitest suites to pass.

- [x] **Step 3: Review the final diff**

Run `git diff --check`; confirm the sessions response contains no code or output fields, latest-run loading is batched, and the existing sandbox lifecycle behavior is unchanged.

- [x] **Step 4: Verify the local list UI**

Reload `http://localhost:19090/code-runner` in the in-app browser. Confirm the current session shows its latest language, result, duration, and execution time without sandbox state or low-level metadata.
