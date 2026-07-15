package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	qiniusb "github.com/qiniu/go-sdk/v7/sandbox"
	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/entity"
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
	if runtime.lastCreateRequest.TimeoutSeconds != 30*60 || runtime.lastWorkspaceRequest.TimeoutSeconds != 30*60 {
		t.Fatalf(
			"timeouts = create:%d prepare:%d, want 1800",
			runtime.lastCreateRequest.TimeoutSeconds,
			runtime.lastWorkspaceRequest.TimeoutSeconds,
		)
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

func TestCodeRunnerSessionsIncludesLatestRunSummary(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	withRun, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:       "with run",
		Region:     "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID:  "sandbox-1",
		TemplateID: "code-interpreter-v1",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save session with run: %v", err)
	}
	withoutRun, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:       "without run",
		Region:     "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID:  "sandbox-2",
		TemplateID: "code-interpreter-v1",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save session without run: %v", err)
	}
	olderRun, err := ctrl.service.SaveCodeRun(context.Background(), user.AccountID, service.CodeRunInput{
		SessionID:  withRun.ID,
		SandboxID:  withRun.SandboxID,
		Language:   "javascript",
		Code:       "console.log(1)",
		Stdout:     "1\n",
		ExitCode:   0,
		DurationMS: 100,
	})
	if err != nil {
		t.Fatalf("save older run: %v", err)
	}
	if err := ctrl.service.DB().Model(olderRun).Update("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatalf("age older run: %v", err)
	}
	latestRun, err := ctrl.service.SaveCodeRun(context.Background(), user.AccountID, service.CodeRunInput{
		SessionID:  withRun.ID,
		SandboxID:  withRun.SandboxID,
		Language:   "python",
		Code:       "raise RuntimeError('boom')",
		Stderr:     "traceback\n",
		Error:      "boom",
		ExitCode:   1,
		DurationMS: 3842,
	})
	if err != nil {
		t.Fatalf("save latest run: %v", err)
	}
	latestAt := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	if err := ctrl.service.DB().Model(latestRun).Update("created_at", latestAt).Error; err != nil {
		t.Fatalf("set latest run time: %v", err)
	}
	var latestRunSelects []string
	callbackName := "test:capture_latest_code_run_projection"
	if err := ctrl.service.DB().Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "code_runs" {
			latestRunSelects = append([]string(nil), tx.Statement.Selects...)
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = ctrl.service.DB().Callback().Query().Remove(callbackName)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-runner/sessions", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()
	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	wantLatestRunSelects := []string{
		"code_runs.session_id",
		"code_runs.language",
		"code_runs.exit_code",
		"code_runs.error",
		"code_runs.duration_ms",
		"code_runs.created_at",
	}
	if !slices.Equal(latestRunSelects, wantLatestRunSelects) {
		t.Fatalf("latest run selects = %v, want %v", latestRunSelects, wantLatestRunSelects)
	}
	var payload codeRunnerSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var withRunResponse, withoutRunResponse *codeRunnerSessionResponse
	for i := range payload.Sessions {
		switch payload.Sessions[i].ID {
		case withRun.ID:
			withRunResponse = &payload.Sessions[i]
		case withoutRun.ID:
			withoutRunResponse = &payload.Sessions[i]
		}
	}
	if withRunResponse == nil || withoutRunResponse == nil {
		t.Fatalf("sessions = %+v, want both test sessions", payload.Sessions)
	}
	if withRunResponse.LatestRun == nil ||
		withRunResponse.LatestRun.Language != "python" ||
		withRunResponse.LatestRun.Succeeded ||
		withRunResponse.LatestRun.DurationMS != 3842 ||
		!withRunResponse.LatestRun.CreatedAt.Equal(latestAt) {
		t.Fatalf("latest run = %+v, want latest failed python run", withRunResponse.LatestRun)
	}
	if withoutRunResponse.LatestRun != nil {
		t.Fatalf("latest run = %+v, want omitted", withoutRunResponse.LatestRun)
	}
	for _, forbidden := range []string{`"code"`, `"stdin"`, `"stdout"`, `"stderr"`, `"error"`} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("sessions response contains %s", forbidden)
		}
	}
}

func TestCodeRunnerSessionsSkipsLatestRunsQueryWhenEmpty(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	latestRunsQueries := 0
	callbackName := "test:count_latest_code_run_queries"
	if err := ctrl.service.DB().Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "code_runs" {
			latestRunsQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = ctrl.service.DB().Callback().Query().Remove(callbackName)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-runner/sessions", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()
	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if latestRunsQueries != 0 {
		t.Fatalf("latest run queries = %d, want 0", latestRunsQueries)
	}
	var payload codeRunnerSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Sessions) != 0 {
		t.Fatalf("sessions = %+v, want empty", payload.Sessions)
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
	if runtime.lastConnectTimeout != 30*60 || runtime.lastWorkspaceRequest.TimeoutSeconds != 30*60 {
		t.Fatalf(
			"timeouts = connect:%d prepare:%d, want 1800",
			runtime.lastConnectTimeout,
			runtime.lastWorkspaceRequest.TimeoutSeconds,
		)
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

func TestCodeRunnerSessionHeartbeatRefreshesThirtyMinuteTimeout(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:       "scratch",
		Region:     "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID:  "sandbox-2",
		TemplateID: "code-interpreter-v1",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/heartbeat", nil)
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
	if runtime.lastTimeoutSandboxID != "sandbox-2" ||
		runtime.lastTimeoutEndpoint != "https://us-south-1-sandbox.qiniuapi.com" ||
		runtime.lastTimeoutSeconds != 30*60 {
		t.Fatalf(
			"heartbeat timeout = (%q, %q, %d), want sandbox-2, US region, 1800",
			runtime.lastTimeoutSandboxID,
			runtime.lastTimeoutEndpoint,
			runtime.lastTimeoutSeconds,
		)
	}
}

func TestKillCodeRunnerSessionMarksSandboxKilled(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(
		context.Background(),
		user.AccountID,
		service.CodeRunnerSessionInput{
			Name:       "scratch",
			Region:     "https://us-south-1-sandbox.qiniuapi.com",
			SandboxID:  "sandbox-2",
			TemplateID: "code-interpreter-v1",
			State:      "running",
		},
		service.SandboxSessionInput{
			SandboxID:  "sandbox-2",
			TemplateID: "code-interpreter-v1",
			State:      "running",
			Region:     "https://us-south-1-sandbox.qiniuapi.com",
		},
	)
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	runtime.onKill = func(string) {
		cancelRequest()
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/kill", nil).WithContext(requestCtx)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if runtime.lastKillContextErr != nil {
		t.Fatalf("kill context error = %v, want detached live context", runtime.lastKillContextErr)
	}
	storedSession, err := ctrl.service.CodeRunnerSession(context.Background(), user.AccountID, session.ID)
	if err != nil {
		t.Fatalf("load code runner session: %v", err)
	}
	storedSandbox, err := ctrl.service.SandboxSession(context.Background(), user.AccountID, session.SandboxID)
	if err != nil {
		t.Fatalf("load sandbox session: %v", err)
	}
	if storedSession.State != "killed" || storedSandbox.State != "killed" {
		t.Fatalf("states = session:%q sandbox:%q, want killed", storedSession.State, storedSandbox.State)
	}
}

func TestKillCodeRunnerSessionFollowsConcurrentReplacement(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(
		context.Background(),
		user.AccountID,
		service.CodeRunnerSessionInput{
			Name:       "scratch",
			Region:     "https://us-south-1-sandbox.qiniuapi.com",
			SandboxID:  "sandbox-old",
			TemplateID: "code-interpreter-v1",
			State:      "running",
		},
		service.SandboxSessionInput{
			SandboxID:  "sandbox-old",
			TemplateID: "code-interpreter-v1",
			State:      "running",
			Region:     "https://us-south-1-sandbox.qiniuapi.com",
		},
	)
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	replaced := false
	runtime.onKill = func(sandboxID string) {
		if replaced || sandboxID != "sandbox-old" {
			return
		}
		replaced = true
		if _, saveErr := ctrl.service.SaveCodeRunnerSessionWithSandbox(
			context.Background(),
			user.AccountID,
			service.CodeRunnerSessionInput{
				ID:         session.ID,
				Name:       session.Name,
				Region:     session.Region,
				SandboxID:  "sandbox-new",
				TemplateID: session.TemplateID,
				State:      "running",
			},
			service.SandboxSessionInput{
				SandboxID:  "sandbox-new",
				TemplateID: session.TemplateID,
				State:      "running",
				Region:     session.Region,
			},
		); saveErr != nil {
			t.Fatalf("replace code runner sandbox: %v", saveErr)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/kill", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if !slices.Equal(runtime.killedSandboxIDs, []string{"sandbox-old", "sandbox-new"}) {
		t.Fatalf("killed sandboxes = %v, want old then replacement", runtime.killedSandboxIDs)
	}
	stored, err := ctrl.service.CodeRunnerSession(context.Background(), user.AccountID, session.ID)
	if err != nil {
		t.Fatalf("load code runner session: %v", err)
	}
	if stored.SandboxID != "sandbox-new" || stored.State != "killed" {
		t.Fatalf("stored session = %+v, want replacement sandbox marked killed", stored)
	}
	for _, sandboxID := range []string{"sandbox-old", "sandbox-new"} {
		storedSandbox, loadErr := ctrl.service.SandboxSession(context.Background(), user.AccountID, sandboxID)
		if loadErr != nil {
			t.Fatalf("load sandbox %s: %v", sandboxID, loadErr)
		}
		if storedSandbox.State != "killed" {
			t.Fatalf("sandbox %s state = %q, want killed", sandboxID, storedSandbox.State)
		}
	}
}

func TestKillCodeRunnerSessionReturnsNotFoundWhenDeletedDuringKill(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(
		context.Background(),
		user.AccountID,
		service.CodeRunnerSessionInput{
			Name:       "scratch",
			Region:     "https://us-south-1-sandbox.qiniuapi.com",
			SandboxID:  "sandbox-old",
			TemplateID: "code-interpreter-v1",
			State:      "running",
		},
		service.SandboxSessionInput{
			SandboxID:  "sandbox-old",
			TemplateID: "code-interpreter-v1",
			State:      "running",
			Region:     "https://us-south-1-sandbox.qiniuapi.com",
		},
	)
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.onKill = func(string) {
		if deleteErr := ctrl.service.DB().Delete(&entity.CodeRunnerSession{}, "id = ? AND account_id = ?", session.ID, user.AccountID).Error; deleteErr != nil {
			t.Fatalf("delete code runner session: %v", deleteErr)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-runner/sessions/"+session.ID+"/kill", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", rec.Code, rec.Body.String())
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
	if runtime.lastTimeoutSandboxID != "sandbox-2" || runtime.lastTimeoutSeconds != 30*60 {
		t.Fatalf(
			"post-run timeout = %q/%d, want sandbox-2/1800",
			runtime.lastTimeoutSandboxID,
			runtime.lastTimeoutSeconds,
		)
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

func TestRunCodeKeepsThirtyMinuteTTLWhenRequestIsCanceled(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
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
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "42\n", ExitCode: 0}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	runtime.onRunCommand = cancelRequest
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/code-runner/sessions/"+session.ID+"/runs",
		strings.NewReader(`{"language":"python","code":"print(42)"}`),
	).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if runtime.lastCommandRequest.SandboxTimeoutSeconds != codeRunnerSandboxTimeoutSeconds {
		t.Fatalf(
			"command sandbox timeout = %d, want %d",
			runtime.lastCommandRequest.SandboxTimeoutSeconds,
			codeRunnerSandboxTimeoutSeconds,
		)
	}
	if runtime.lastTimeoutContextErr != nil {
		t.Fatalf("refresh timeout context error = %v, want live detached context", runtime.lastTimeoutContextErr)
	}
	if runtime.lastTimeoutSeconds != codeRunnerSandboxTimeoutSeconds {
		t.Fatalf("refresh timeout = %d, want %d", runtime.lastTimeoutSeconds, codeRunnerSandboxTimeoutSeconds)
	}
}

func TestRunCodeRecreatesMissingSandboxAndRetries(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:          "scratch",
		Region:        "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-gone",
		TemplateID:    "code-interpreter-v1",
		State:         "running",
		WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandErrors = []error{&qiniusb.APIError{StatusCode: http.StatusNotFound}}
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "recovered\n", ExitCode: 0}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/code-runner/sessions/"+session.ID+"/runs",
		strings.NewReader(`{"language":"python","code":"print(42)"}`),
	)
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
	if !slices.Equal(runtime.commandSandboxIDs, []string{"sandbox-gone", "sandbox-1"}) {
		t.Fatalf("command sandboxes = %v, want old then recreated sandbox", runtime.commandSandboxIDs)
	}
	if runtime.timeoutCalls != 1 || runtime.lastTimeoutSandboxID != "sandbox-1" {
		t.Fatalf(
			"timeout refreshes = %d for %q, want one refresh for recreated sandbox",
			runtime.timeoutCalls,
			runtime.lastTimeoutSandboxID,
		)
	}
	storedSession, err := ctrl.service.CodeRunnerSession(context.Background(), user.AccountID, session.ID)
	if err != nil {
		t.Fatalf("load code runner session: %v", err)
	}
	if storedSession.SandboxID != "sandbox-1" || storedSession.State != "running" {
		t.Fatalf("stored session = %+v, want recreated running sandbox", storedSession)
	}
	runs, err := ctrl.service.ListCodeRuns(context.Background(), user.AccountID, session.ID, 50)
	if err != nil {
		t.Fatalf("list code runs: %v", err)
	}
	if len(runs) != 1 || runs[0].SandboxID != "sandbox-1" || runs[0].Stdout != "recovered\n" {
		t.Fatalf("runs = %+v, want recovered run on sandbox-1", runs)
	}
}

func TestRunCodeReturnsNotFoundWhenSessionDeletedDuringRecovery(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:          "scratch",
		Region:        "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-gone",
		TemplateID:    "code-interpreter-v1",
		State:         "running",
		WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandErrors = []error{&qiniusb.APIError{StatusCode: http.StatusNotFound}}
	runtime.onRunCommand = func() {
		if deleteErr := ctrl.service.DB().Delete(&entity.CodeRunnerSession{}, "id = ? AND account_id = ?", session.ID, user.AccountID).Error; deleteErr != nil {
			t.Fatalf("delete code runner session: %v", deleteErr)
		}
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/code-runner/sessions/"+session.ID+"/runs",
		strings.NewReader(`{"language":"python","code":"print(42)"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunCodeDoesNotRestoreSandboxAfterConcurrentKill(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(
		context.Background(),
		user.AccountID,
		service.CodeRunnerSessionInput{
			Name:          "scratch",
			Region:        "https://us-south-1-sandbox.qiniuapi.com",
			SandboxID:     "sandbox-gone",
			TemplateID:    "code-interpreter-v1",
			State:         "running",
			WorkspacePath: "/workspace/scratch",
		},
		service.SandboxSessionInput{
			SandboxID:     "sandbox-gone",
			TemplateID:    "code-interpreter-v1",
			State:         "running",
			Region:        "https://us-south-1-sandbox.qiniuapi.com",
			WorkspacePath: "/workspace/scratch",
		},
	)
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandErrors = []error{&qiniusb.APIError{StatusCode: http.StatusNotFound}}
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "must not run\n", ExitCode: 0}
	runtime.onPrepareWorkspace = func() {
		if _, saveErr := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
			ID:            session.ID,
			Name:          session.Name,
			Region:        session.Region,
			SandboxID:     session.SandboxID,
			TemplateID:    session.TemplateID,
			State:         "killed",
			WorkspacePath: session.WorkspacePath,
		}); saveErr != nil {
			t.Fatalf("mark code runner killed: %v", saveErr)
		}
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/code-runner/sessions/"+session.ID+"/runs",
		strings.NewReader(`{"language":"python","code":"print(42)"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	newTestRouter(ctrl).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	stored, err := ctrl.service.CodeRunnerSession(context.Background(), user.AccountID, session.ID)
	if err != nil {
		t.Fatalf("load code runner session: %v", err)
	}
	if stored.SandboxID != "sandbox-gone" || stored.State != "killed" {
		t.Fatalf("stored session = %+v, want concurrent kill preserved", stored)
	}
	if runtime.lastTimeoutSandboxID != "sandbox-1" || runtime.lastTimeoutSeconds != codeRunnerCleanupLifetime {
		t.Fatalf(
			"replacement cleanup = %q/%d, want sandbox-1/%d",
			runtime.lastTimeoutSandboxID,
			runtime.lastTimeoutSeconds,
			codeRunnerCleanupLifetime,
		)
	}
	runs, err := ctrl.service.ListCodeRuns(context.Background(), user.AccountID, session.ID, 50)
	if err != nil {
		t.Fatalf("list code runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %+v, want no execution after concurrent kill", runs)
	}
}

func TestRunCodeRecreatesKilledSandboxAndRetries(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	session, err := ctrl.service.SaveCodeRunnerSession(context.Background(), user.AccountID, service.CodeRunnerSessionInput{
		Name:          "scratch",
		Region:        "https://us-south-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-old",
		TemplateID:    "code-interpreter-v1",
		State:         "killed",
		WorkspacePath: "/workspace/scratch",
	})
	if err != nil {
		t.Fatalf("save code runner session: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.commandResult = &sandboxRuntimeCommandResult{Stdout: "recovered\n", ExitCode: 0}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/code-runner/sessions/"+session.ID+"/runs",
		strings.NewReader(`{"language":"python","code":"print(42)"}`),
	)
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
	if !slices.Equal(runtime.commandSandboxIDs, []string{"sandbox-1"}) {
		t.Fatalf("command sandboxes = %v, want only recreated sandbox", runtime.commandSandboxIDs)
	}
	runs, err := ctrl.service.ListCodeRuns(context.Background(), user.AccountID, session.ID, 50)
	if err != nil {
		t.Fatalf("list code runs: %v", err)
	}
	if len(runs) != 1 || runs[0].SandboxID != "sandbox-1" || runs[0].Stdout != "recovered\n" {
		t.Fatalf("runs = %+v, want recovered run on sandbox-1", runs)
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
