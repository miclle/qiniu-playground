package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestCreateCodeRunnerSessionUsesInterpreterTemplate(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions", bytes.NewReader([]byte(`{
		"name":"scratch",
		"region":"https://cn-yangzhou-1-sandbox.qiniuapi.com"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastAPIKey != "qiniu-api-key" {
		t.Fatalf("runtime api key = %q, want decrypted key", runtime.lastAPIKey)
	}
	if runtime.lastCreateRequest.TemplateID != "code-interpreter-v1" {
		t.Fatalf("template id = %q, want code-interpreter-v1", runtime.lastCreateRequest.TemplateID)
	}
	if runtime.lastCreateRequest.Endpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("endpoint = %q, want requested region", runtime.lastCreateRequest.Endpoint)
	}
	if runtime.lastWorkspaceRequest.WorkspacePath != "/workspace/scratch" {
		t.Fatalf("workspace path = %q, want /workspace/scratch", runtime.lastWorkspaceRequest.WorkspacePath)
	}
	if len(runtime.lastWorkspaceRequest.Envs) != 0 {
		t.Fatalf("code runner workspace envs = %v, want no user-accessible credentials", runtime.lastWorkspaceRequest.Envs)
	}
	var payload codeRunnerSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Name != "scratch" || payload.TemplateID != "code-interpreter-v1" || payload.SandboxID != "sandbox-1" {
		t.Fatalf("payload = %+v, want created code runner session", payload)
	}
}

func TestCreateCodeRunnerSessionRejectsUnsupportedRegion(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions", bytes.NewReader([]byte(`{
		"name":"scratch",
		"region":"http://169.254.169.254/latest/meta-data"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastCreateRequest.Endpoint != "" {
		t.Fatalf("runtime endpoint = %q, want no sandbox request", runtime.lastCreateRequest.Endpoint)
	}
}

func TestCreateCodeRunnerSessionRejectsUnsafeName(t *testing.T) {
	for _, name := range []string{"..", "bad/name", strings.Repeat("a", 101)} {
		t.Run(name, func(t *testing.T) {
			ctrl := newTestController(t)
			user := createAuthenticatedUser(t, ctrl)
			saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
			body, err := json.Marshal(map[string]string{
				"name":   name,
				"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com",
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			router := newTestRouter(ctrl)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{
				Name:  sessionCookieName,
				Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
			})
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
			}
			runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
			if runtime.lastCreateRequest.Endpoint != "" {
				t.Fatalf("runtime endpoint = %q, want no sandbox request", runtime.lastCreateRequest.Endpoint)
			}
		})
	}
}

func TestCreateCodeRunnerSessionPersistsAfterRequestCancellation(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	ctx, cancel := context.WithCancel(context.Background())
	runtime.onPrepareWorkspace = cancel
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions", bytes.NewReader([]byte(`{
		"name":"scratch",
		"region":"https://cn-yangzhou-1-sandbox.qiniuapi.com"
	}`))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if runtime.lastWorkspaceContextErr != nil {
		t.Fatalf("workspace context error = %v, want provisioning detached from request cancellation", runtime.lastWorkspaceContextErr)
	}
	sessions, err := ctrl.service.ListCodeRunnerSessions(context.Background(), user.AccountID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SandboxID != "sandbox-1" {
		t.Fatalf("sessions = %+v, want canceled request sandbox persisted", sessions)
	}
}

func TestCreateCodeRunnerSessionShortensSandboxLifetimeWhenPreparationFails(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.workspaceErr = errors.New("prepare workspace failed")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions", bytes.NewReader([]byte(`{
		"name":"scratch",
		"region":"https://cn-yangzhou-1-sandbox.qiniuapi.com"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if runtime.lastTimeoutSandboxID != "sandbox-1" ||
		runtime.lastTimeoutEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" ||
		runtime.lastTimeoutSeconds != 60 {
		t.Fatalf("cleanup timeout = (%q, %q, %d), want sandbox-1, requested region, 60", runtime.lastTimeoutSandboxID, runtime.lastTimeoutEndpoint, runtime.lastTimeoutSeconds)
	}
}

func TestCreateCodeRunnerSessionShortensSandboxLifetimeWhenSessionSaveFails(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.onPrepareWorkspace = func() {
		sqlDB, err := ctrl.service.DB().DB()
		if err != nil {
			t.Fatalf("get sql db: %v", err)
		}
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close sql db: %v", err)
		}
	}
	rec := createCodeRunnerSessionRequest(t, ctrl, user.AccountID)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want save failure body=%s", rec.Code, rec.Body.String())
	}
	assertCodeRunnerSandboxCleanup(t, runtime)
}

func TestCreateCodeRunnerSessionShortensSandboxLifetimeWhenSandboxSaveFails(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	callbackName := "test:fail_code_runner_sandbox_save"
	err := ctrl.service.DB().Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "SandboxSession" {
			_ = tx.AddError(errors.New("save sandbox session failed"))
		}
	})
	if err != nil {
		t.Fatalf("register create callback: %v", err)
	}
	t.Cleanup(func() {
		_ = ctrl.service.DB().Callback().Create().Remove(callbackName)
	})
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	rec := createCodeRunnerSessionRequest(t, ctrl, user.AccountID)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want save failure body=%s", rec.Code, rec.Body.String())
	}
	assertCodeRunnerSandboxCleanup(t, runtime)
	sessions, err := ctrl.service.ListCodeRunnerSessions(context.Background(), user.AccountID)
	if err != nil {
		t.Fatalf("list code runner sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %+v, want transaction rollback", sessions)
	}
}

func createCodeRunnerSessionRequest(t *testing.T, ctrl *Ctrl, accountID string) *httptest.ResponseRecorder {
	t.Helper()
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions", bytes.NewReader([]byte(`{
		"name":"scratch",
		"region":"https://cn-yangzhou-1-sandbox.qiniuapi.com"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(accountID, time.Now()),
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertCodeRunnerSandboxCleanup(t *testing.T, runtime *fakeSandboxRuntime) {
	t.Helper()
	if runtime.lastTimeoutSandboxID != "sandbox-1" ||
		runtime.lastTimeoutEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" ||
		runtime.lastTimeoutSeconds != 60 {
		t.Fatalf("cleanup timeout = (%q, %q, %d), want sandbox-1, requested region, 60", runtime.lastTimeoutSandboxID, runtime.lastTimeoutEndpoint, runtime.lastTimeoutSeconds)
	}
}

func TestConnectCodeRunnerSessionRejectsMissingSandboxID(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:       "scratch",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		TemplateID: "code-interpreter-v1",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/connect", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastConnectEndpoint != "" {
		t.Fatalf("connect endpoint = %q, want no runtime request", runtime.lastConnectEndpoint)
	}
}

func TestConnectCodeRunnerSessionUpdatesSandboxSession(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:          "scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-2",
		TemplateID:    "code-interpreter-v1",
		State:         "paused",
		Endpoint:      "old.example.test",
		WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	if _, err := ctrl.service.SaveSandboxSession(context.Background(), user.AccountID, service.SandboxSessionInput{
		SandboxID:     session.SandboxID,
		TemplateID:    session.TemplateID,
		State:         session.State,
		Endpoint:      session.Endpoint,
		WorkspacePath: session.WorkspacePath,
		Region:        session.Region,
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.lastCreateRequest.TemplateID = "code-interpreter-v1"
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/connect", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	sandboxSession, err := ctrl.service.SandboxSession(context.Background(), user.AccountID, session.SandboxID)
	if err != nil {
		t.Fatalf("load sandbox session: %v", err)
	}
	if sandboxSession.State != "running" ||
		sandboxSession.Endpoint != "sandbox-2.example.test" ||
		sandboxSession.WorkspacePath != "/workspace/scratch" {
		t.Fatalf("sandbox session = %+v, want reconnected runtime state", sandboxSession)
	}
}

func TestRunCodeStoresResult(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	session, err := ctrl.service.SaveCodeRunnerSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.CodeRunnerSessionInput{
		Name:          "scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-2",
		TemplateID:    "code-interpreter-v1",
		State:         "running",
		WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandResult = &sandboxRuntimeCommandResult{
		Stdout:   "42\n",
		Stderr:   "",
		Error:    "",
		ExitCode: 0,
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/runs", bytes.NewReader([]byte(`{
		"language":"python",
		"code":"print(40 + 2)",
		"stdin":"",
		"timeout_seconds":30
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if runtime.lastCommandEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("command endpoint = %q, want session region", runtime.lastCommandEndpoint)
	}
	if runtime.lastCommandRequest.WorkspacePath != "/workspace/scratch" ||
		runtime.lastCommandRequest.Language != "python" ||
		runtime.lastCommandRequest.Code != "print(40 + 2)" ||
		runtime.lastCommandRequest.Timeout != 30*time.Second {
		t.Fatalf("command request = %+v, want python execution in session workspace", runtime.lastCommandRequest)
	}
	if len(runtime.lastCommandRequest.Envs) != 0 {
		t.Fatalf("code runner command envs = %v, want no user-accessible credentials", runtime.lastCommandRequest.Envs)
	}
	var payload codeRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Stdout != "42\n" || payload.ExitCode != 0 || payload.SessionID != session.ID {
		t.Fatalf("payload = %+v, want stored run result", payload)
	}
	runs, err := ctrl.service.ListCodeRuns(req.Context(), user.AccountID, session.ID, 50)
	if err != nil {
		t.Fatalf("list code runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Stdout != "42\n" || runs[0].Code != "print(40 + 2)" {
		t.Fatalf("runs = %+v, want persisted result", runs)
	}
}

func TestRunCodeUpdatesLatestRunWhenSourceUnchanged(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.CodeRunnerSessionInput{
		Name:          "scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-2",
		TemplateID:    "code-interpreter-v1",
		State:         "running",
		WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	router := newTestRouter(ctrl)
	body := []byte(`{
		"language":"python",
		"code":"print(40 + 2)",
		"stdin":"",
		"timeout_seconds":30
	}`)
	runCodeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/runs", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  sessionCookieName,
			Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
		})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "first\n", ExitCode: 0}
	first := runCodeRequest()
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200 body=%s", first.Code, first.Body.String())
	}
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "second\n", ExitCode: 0}
	second := runCodeRequest()
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200 body=%s", second.Code, second.Body.String())
	}

	runs, err := ctrl.service.ListCodeRuns(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, session.ID, 50)
	if err != nil {
		t.Fatalf("list code runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if runs[0].Stdout != "second\n" || runs[0].Code != "print(40 + 2)" {
		t.Fatalf("runs[0] = %+v, want updated latest result", runs[0])
	}
}

func TestRunCodeRejectsUnsupportedLanguage(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.CodeRunnerSessionInput{
		Name:       "scratch",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:  "sandbox-2",
		TemplateID: "code-interpreter-v1",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/runs", bytes.NewReader([]byte(`{
		"language":"ruby",
		"code":"puts 42"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunCodeRejectsOversizedTimeoutBeforeRuntime(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:       "scratch",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:  "sandbox-2",
		TemplateID: "code-interpreter-v1",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/runs", bytes.NewReader([]byte(`{
		"language":"python",
		"code":"print(42)",
		"timeout_seconds":2147483647
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastCommandEndpoint != "" {
		t.Fatalf("command endpoint = %q, want no runtime request", runtime.lastCommandEndpoint)
	}
}
