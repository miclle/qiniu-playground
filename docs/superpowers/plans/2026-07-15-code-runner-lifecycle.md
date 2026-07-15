# Code Runner Sandbox Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep active Code Runner sandboxes alive for 30-minute windows, kill sandboxes after 30 minutes of browser inactivity, and transparently recreate an expired sandbox when the user runs code.

**Architecture:** Add Code Runner-specific heartbeat and kill endpoints beside the existing session routes. The browser records meaningful user activity and refreshes the 30-minute server TTL every minute while active; after 30 idle minutes it submits one kill request. `RunCode` remains the recovery authority: it recreates the session sandbox only when the stored state is killed or the runtime reports that the sandbox no longer exists, then retries the command once.

**Tech Stack:** Go 1.26, fox-gonic/fox, GORM, Qiniu Sandbox SDK, React 19, React Query v5, Axios, Vitest.

## Global Constraints

- Preserve the existing `Handler -> Service -> Entity` layering and route registration in `internal/handler/handler.go`.
- Use a Code Runner-specific sandbox TTL of exactly 1,800 seconds without changing the 24-hour workspace default.
- Heartbeats must never create sandboxes; only an explicit Run may recover a killed or expired sandbox.
- Automatic recovery retries a code command at most once.
- Do not add database columns or dependencies.
- Preserve the current uncommitted region-ordering change and leave all work uncommitted unless the user asks for a commit.

---

### Task 1: Code Runner TTL, heartbeat, and kill APIs

**Files:**
- Modify: `internal/handler/code_runner.go`
- Modify: `internal/handler/handler.go`
- Modify: `internal/handler/sandbox_runtime.go`
- Modify: `internal/handler/oauth_test.go`
- Modify: `internal/handler/code_runner_test.go`
- Modify: `internal/handler/sandbox_runtime_test.go`

**Interfaces:**
- Produces: `const codeRunnerSandboxTimeoutSeconds int32 = 30 * 60`
- Produces: `CodeRunnerSessionHeartbeat(*fox.Context) any`
- Produces: `KillCodeRunnerSession(*fox.Context) any`
- Produces: `sandboxRuntime.Kill(context.Context, apiKey, sandboxID, endpoint string) error`
- Produces routes `POST /api/v1/code-runner/sessions/:sessionID/heartbeat` and `POST /api/v1/code-runner/sessions/:sessionID/kill`

- [x] **Step 1: Write failing handler tests for the 30-minute TTL and heartbeat**

Add assertions to `TestCreateCodeRunnerSessionUsesInterpreterTemplate` and this heartbeat test:

```go
if runtime.lastCreateRequest.TimeoutSeconds != 30*60 ||
	runtime.lastWorkspaceRequest.TimeoutSeconds != 30*60 {
	t.Fatalf("timeouts = create:%d prepare:%d, want 1800", runtime.lastCreateRequest.TimeoutSeconds, runtime.lastWorkspaceRequest.TimeoutSeconds)
}

func TestCodeRunnerSessionHeartbeatRefreshesThirtyMinuteTimeout(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name: "scratch", Region: "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID: "sandbox-2", TemplateID: "code-interpreter-v1", State: "running",
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/heartbeat", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now())})
	rec := httptest.NewRecorder()
	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastTimeoutSandboxID != "sandbox-2" ||
		runtime.lastTimeoutEndpoint != "https://us-south-1-sandbox.qiniuapi.com" ||
		runtime.lastTimeoutSeconds != 30*60 {
		t.Fatalf("heartbeat timeout = %q/%q/%d", runtime.lastTimeoutSandboxID, runtime.lastTimeoutEndpoint, runtime.lastTimeoutSeconds)
	}
}
```

- [x] **Step 2: Run the focused handler tests and verify RED**

Run: `go test ./internal/handler -run 'Test(CreateCodeRunnerSessionUsesInterpreterTemplate|CodeRunnerSessionHeartbeatRefreshesThirtyMinuteTimeout)$' -count=1`

Expected: FAIL because creation uses the global sandbox timeout and the heartbeat route returns 404.

- [x] **Step 3: Implement the Code Runner-specific TTL and heartbeat**

Use `codeRunnerSandboxTimeoutSeconds` for Code Runner Create, Connect, PrepareWorkspace, command connections, and heartbeat calls. The heartbeat response is:

```go
type codeRunnerHeartbeatResponse struct {
	OK             bool  `json:"ok"`
	TimeoutSeconds int32 `json:"timeout_seconds"`
}
```

If `SetTimeout` reports a missing sandbox, return HTTP 409 with `code runner sandbox no longer exists`; do not recreate it.

- [x] **Step 4: Run the focused handler tests and verify GREEN**

Run: `go test ./internal/handler -run 'Test(CreateCodeRunnerSessionUsesInterpreterTemplate|CodeRunnerSessionHeartbeatRefreshesThirtyMinuteTimeout)$' -count=1`

Expected: PASS.

- [x] **Step 5: Write failing runtime and handler tests for Kill**

Add these tests:

```go
func TestKillSandboxUsesDeleteAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodDelete || req.URL.Path != "/sandboxes/sandbox-2" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		if req.Header.Get("X-API-Key") != "api-key" || req.Header.Get("Authorization") != "Bearer api-key" {
			t.Fatalf("auth headers = %q/%q", req.Header.Get("X-API-Key"), req.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := (&qiniuSandboxRuntime{}).Kill(context.Background(), "api-key", "sandbox-2", server.URL); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
}

func TestKillCodeRunnerSessionMarksSandboxKilled(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(context.Background(), user.AccountID,
		service.CodeRunnerSessionInput{Name: "scratch", Region: "https://us-south-1-sandbox.qiniuapi.com", SandboxID: "sandbox-2", TemplateID: "code-interpreter-v1", State: "running"},
		service.SandboxSessionInput{SandboxID: "sandbox-2", TemplateID: "code-interpreter-v1", State: "running", Region: "https://us-south-1-sandbox.qiniuapi.com"})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/kill", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now())})
	rec := httptest.NewRecorder()
	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastKilledSandboxID != "sandbox-2" || runtime.lastKillEndpoint != "https://us-south-1-sandbox.qiniuapi.com" {
		t.Fatalf("kill target = %q/%q", runtime.lastKilledSandboxID, runtime.lastKillEndpoint)
	}
	stored, _ := ctrl.service.CodeRunnerSession(context.Background(), user.AccountID, session.ID)
	sandbox, _ := ctrl.service.SandboxSession(context.Background(), user.AccountID, "sandbox-2")
	if stored.State != "killed" || sandbox.State != "killed" {
		t.Fatalf("states = %q/%q, want killed", stored.State, sandbox.State)
	}
}
```

- [x] **Step 6: Run the kill tests and verify RED**

Run: `go test ./internal/handler -run 'Test(KillSandboxUsesDeleteAPI|KillCodeRunnerSessionMarksSandboxKilled)$' -count=1`

Expected: FAIL because `sandboxRuntime.Kill` and the kill route do not exist.

- [x] **Step 7: Implement idempotent sandbox termination**

Extend the runtime interface and fake with:

```go
Kill(ctx context.Context, apiKey, sandboxID, endpoint string) error
```

The real runtime sends an authenticated DELETE to the sandbox resource URL. The handler treats a missing sandbox as already killed, then atomically saves the Code Runner and sandbox-session views with `state=killed` while retaining the old sandbox ID for audit and Run recovery.

- [x] **Step 8: Run the kill tests and verify GREEN**

Run: `go test ./internal/handler -run 'Test(KillSandboxUsesDeleteAPI|KillCodeRunnerSessionMarksSandboxKilled)$' -count=1`

Expected: PASS.

### Task 2: Transparent Run recovery

**Files:**
- Modify: `internal/handler/code_runner.go`
- Modify: `internal/handler/oauth_test.go`
- Modify: `internal/handler/code_runner_test.go`

**Interfaces:**
- Consumes: `codeRunnerSandboxTimeoutSeconds`
- Consumes: `isSandboxNotFoundError(error) bool`
- Produces: `recreateCodeRunnerSandbox(context.Context, accountID, apiKey string, session *entity.CodeRunnerSession) (*entity.CodeRunnerSession, error)`

- [x] **Step 1: Write failing recovery tests**

Add a fake runtime command-error queue and command sandbox-ID history. The expired-sandbox test is:

```go
func TestRunCodeRecreatesMissingSandboxAndRetries(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name: "scratch", Region: "https://us-south-1-sandbox.qiniuapi.com", SandboxID: "sandbox-gone",
		TemplateID: "code-interpreter-v1", State: "running", WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandErrors = []error{&qiniusb.APIError{StatusCode: http.StatusNotFound}}
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "recovered\n", ExitCode: 0}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/runs", strings.NewReader(`{"language":"python","code":"print(42)"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now())})
	rec := httptest.NewRecorder()
	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if !slices.Equal(runtime.commandSandboxIDs, []string{"sandbox-gone", "sandbox-1"}) {
		t.Fatalf("command sandboxes = %v", runtime.commandSandboxIDs)
	}
	stored, _ := ctrl.service.CodeRunnerSession(context.Background(), user.AccountID, session.ID)
	if stored.SandboxID != "sandbox-1" || stored.State != "running" {
		t.Fatalf("stored session = %+v", stored)
	}
}
```

The known-killed-state test is:

```go
func TestRunCodeRecreatesKilledSandboxAndRetries(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name: "scratch", Region: "https://us-south-1-sandbox.qiniuapi.com", SandboxID: "sandbox-old",
		TemplateID: "code-interpreter-v1", State: "killed", WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "recovered\n", ExitCode: 0}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/runs", strings.NewReader(`{"language":"python","code":"print(42)"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now())})
	rec := httptest.NewRecorder()
	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if !slices.Equal(runtime.commandSandboxIDs, []string{"sandbox-1"}) {
		t.Fatalf("command sandboxes = %v", runtime.commandSandboxIDs)
	}
	runs, _ := ctrl.service.ListCodeRuns(context.Background(), user.AccountID, session.ID, 50)
	if len(runs) != 1 || runs[0].SandboxID != "sandbox-1" {
		t.Fatalf("runs = %+v", runs)
	}
}
```

- [x] **Step 2: Run the recovery tests and verify RED**

Run: `go test ./internal/handler -run 'TestRunCode(RecreatesMissingSandbox|RecreatesKilledSandbox)AndRetries$' -count=1`

Expected: FAIL because Run returns the runtime error or executes against the killed sandbox.

- [x] **Step 3: Implement one-shot recreation and retry**

`recreateCodeRunnerSandbox` creates the existing session template in the existing region with the 1,800-second TTL, prepares the existing workspace path, and saves the same Code Runner session ID with the new sandbox. `RunCode` provisions first when `session.State == "killed"`; otherwise it runs once, recreates only for `isSandboxNotFoundError`, and retries once against the new sandbox. All other runtime errors return unchanged.

- [x] **Step 4: Run the recovery tests and the existing Code Runner suite**

Run: `go test ./internal/handler -run 'Test(Create|Connect|CodeRunner|Kill|RunCode)' -count=1`

Expected: PASS.

### Task 3: Browser activity heartbeat and idle kill

**Files:**
- Modify: `website/src/api/code-runner.ts`
- Modify: `website/src/views/code-runner/index.tsx`
- Modify: `website/src/views/code-runner/index.test.tsx`

**Interfaces:**
- Produces: `heartbeatCodeRunnerSession(sessionID: string)`
- Produces: `killCodeRunnerSession(sessionID: string)`
- Produces constants `codeRunnerHeartbeatIntervalMs = 60_000` and `codeRunnerIdleTimeoutMs = 30 * 60_000`

- [x] **Step 1: Write failing page tests for heartbeat, activity reset, idle kill, and killed-state Run**

Render `/code-runner/:sessionId` with a complete session fixture and use this timer sequence:

```tsx
test('keeps an active code runner alive and kills it after thirty idle minutes', async () => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date('2026-07-15T00:00:00Z'))
  codeRunnerSessionFixtures = [{
    id: 'crs_1', name: 'Scratch', region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1', template_id: 'code-interpreter-v1', state: 'running', workspace_path: '/workspace/Scratch',
  }]
  const root = renderCodeRunner('/code-runner/crs_1')
  await waitFor(() => expect(heartbeatCodeRunnerSessionMock).toHaveBeenCalledWith('crs_1'))

  await act(async () => vi.advanceTimersByTime(29 * 60_000))
  expect(killCodeRunnerSessionMock).not.toHaveBeenCalled()
  await act(async () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' })))
  await act(async () => vi.advanceTimersByTime(29 * 60_000))
  expect(killCodeRunnerSessionMock).not.toHaveBeenCalled()
  await act(async () => vi.advanceTimersByTime(60_000))
  expect(killCodeRunnerSessionMock).toHaveBeenCalledTimes(1)
  expect(killCodeRunnerSessionMock).toHaveBeenCalledWith('crs_1')
  await act(async () => root.unmount())
})

test('allows Run to recover a killed code runner sandbox', async () => {
  codeRunnerSessionFixtures = [{
    id: 'crs_1', name: 'Scratch', region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-old', template_id: 'code-interpreter-v1', state: 'killed', workspace_path: '/workspace/Scratch',
  }]
  const root = renderCodeRunner('/code-runner/crs_1')
  let runButton: HTMLButtonElement | undefined
  await waitFor(() => {
    runButton = Array.from(document.querySelectorAll('button')).find((button) => button.textContent?.includes('Run'))
    expect(runButton?.disabled).toBe(false)
  })
  await act(async () => root.unmount())
})
```

- [x] **Step 2: Run the focused frontend tests and verify RED**

Run: `npm test -- src/views/code-runner/index.test.tsx`

Expected: FAIL because the page never calls heartbeat or kill and disables Run for a killed session.

- [x] **Step 3: Add API functions and active-session lifecycle effect**

Add:

```ts
export function heartbeatCodeRunnerSession(sessionID: string) {
  return client.post<{ ok: boolean, timeout_seconds: number }>(`/code-runner/sessions/${sessionID}/heartbeat`)
}

export function killCodeRunnerSession(sessionID: string) {
  return client.post<CodeRunnerSession>(`/code-runner/sessions/${sessionID}/kill`)
}
```

The detail page records `pointerdown`, `keydown`, and `input` activity; sends heartbeat immediately and every minute while not idle; submits one kill after 30 idle minutes; removes all listeners/timers on unmount; invalidates the session query after kill and after a successful Run. Run is enabled when code and an existing sandbox ID are present even if stored state is killed.

- [x] **Step 4: Run the focused frontend tests and verify GREEN**

Run: `npm test -- src/views/code-runner/index.test.tsx`

Expected: PASS.

### Task 4: Full verification and browser smoke test

**Files:**
- Verify all files modified by Tasks 1-3.

**Interfaces:**
- Consumes: all lifecycle endpoints and browser behavior above.
- Produces: verified uncommitted working tree.

- [x] **Step 1: Run formatting and repository checks**

Run: `gofmt -w internal/handler/code_runner.go internal/handler/code_runner_test.go internal/handler/handler.go internal/handler/oauth_test.go internal/handler/sandbox_runtime.go internal/handler/sandbox_runtime_test.go`

Run: `task check`

Expected: zero issues and exit status 0.

- [x] **Step 2: Run all tests**

Run: `task test`

Expected: all Go and Vitest suites pass.

- [x] **Step 3: Review the final diff**

Run: `git diff --check` and inspect every modified or new file. Confirm no secret-bearing environment variables reach Code Runner commands, the 24-hour workspace timeout is unchanged, and recovery has no loop.

- [x] **Step 4: Smoke test the local Code Runner detail page**

Reload `http://localhost:19090/code-runner/:sessionId`, confirm the Run control remains usable, trigger a heartbeat through the loaded lifecycle effect, and inspect browser console errors. Do not wait 30 real minutes; automated fake-timer coverage is authoritative for the idle deadline.
