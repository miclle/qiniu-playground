package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qiniusb "github.com/qiniu/go-sdk/v7/sandbox"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestCreateSandboxUsesStoredAPIKey(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes", bytes.NewReader([]byte(`{"template_id":"base","region":"https://us-south-1-sandbox.qiniuapi.com"}`)))
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
	if runtime.lastCreateRequest.Metadata["created_by"] != "qiniu-playground" ||
		runtime.lastCreateRequest.Metadata["kind"] != "standalone" ||
		runtime.lastCreateRequest.Metadata["template_id"] != "base" {
		t.Fatalf("runtime metadata = %#v, want standalone sandbox metadata", runtime.lastCreateRequest.Metadata)
	}
	if runtime.lastCreateRequest.Endpoint != "https://us-south-1-sandbox.qiniuapi.com" {
		t.Fatalf("runtime endpoint = %q, want requested region", runtime.lastCreateRequest.Endpoint)
	}
	var payload sandboxSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SandboxID != "sandbox-1" || payload.TemplateID != "base" {
		t.Fatalf("payload = %+v, want created sandbox", payload)
	}
	if payload.Region != "https://us-south-1-sandbox.qiniuapi.com" {
		t.Fatalf("payload region = %q, want requested region", payload.Region)
	}
	if payload.Metadata["created_by"] != "qiniu-playground" || payload.Metadata["kind"] != "standalone" {
		t.Fatalf("payload metadata = %#v, want standalone metadata", payload.Metadata)
	}
}

func TestConnectSandboxUpdatesSession(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/sandbox-2/connect", bytes.NewReader([]byte(`{"region":"https://us-south-1-sandbox.qiniuapi.com"}`)))
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
	session, err := ctrl.service.SandboxSession(req.Context(), user.AccountID, "sandbox-2")
	if err != nil {
		t.Fatalf("load sandbox session: %v", err)
	}
	if session.Endpoint != "sandbox-2.example.test" {
		t.Fatalf("Endpoint = %q, want sandbox-2.example.test", session.Endpoint)
	}
	if session.Region != "https://us-south-1-sandbox.qiniuapi.com" {
		t.Fatalf("Region = %q, want requested region", session.Region)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastConnectEndpoint != "https://us-south-1-sandbox.qiniuapi.com" {
		t.Fatalf("connect endpoint = %q, want requested region", runtime.lastConnectEndpoint)
	}
}

func TestSandboxSessionsUsesRemoteListAndEnrichesLocalMetadata(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.listSandboxes = []sandboxRuntimeListedSandbox{
		{SandboxID: "sandbox-current", TemplateID: "base", State: "running", CPUCount: 4, MemoryMB: 8192, DiskSizeMB: 20480},
		{SandboxID: "sandbox-remote-only", TemplateID: "node", State: "paused", CPUCount: 2, MemoryMB: 1024, DiskSizeMB: 10240},
	}
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:     "sandbox-current",
		TemplateID:    "base",
		State:         "running",
		Endpoint:      "old-current.example.test",
		Region:        "https://us-south-1-sandbox.qiniuapi.com",
		WorkspacePath: "/workspace/Foo",
		Metadata: map[string]string{
			"workspace_name": "Foo",
		},
	}); err != nil {
		t.Fatalf("save current sandbox session: %v", err)
	}
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:     "sandbox-stale",
		TemplateID:    "base",
		State:         "running",
		Endpoint:      "sandbox-stale.example.test",
		Region:        "https://us-south-1-sandbox.qiniuapi.com",
		WorkspacePath: "/workspace/Stale",
	}); err != nil {
		t.Fatalf("save stale sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes?region=https%3A%2F%2Fus-south-1-sandbox.qiniuapi.com", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if runtime.lastAPIKey != "qiniu-api-key" {
		t.Fatalf("runtime api key = %q, want decrypted key", runtime.lastAPIKey)
	}
	if runtime.lastListEndpoint != "https://us-south-1-sandbox.qiniuapi.com" {
		t.Fatalf("list endpoint = %q, want requested region", runtime.lastListEndpoint)
	}
	var payload struct {
		Sandboxes []sandboxSessionResponse `json:"sandboxes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Sandboxes) != 2 {
		t.Fatalf("sandboxes = %+v, want only remote sandboxes", payload.Sandboxes)
	}
	if payload.Sandboxes[0].SandboxID != "sandbox-current" {
		t.Fatalf("first sandbox = %+v, want enriched current sandbox first", payload.Sandboxes[0])
	}
	if payload.Sandboxes[0].WorkspacePath != "/workspace/Foo" ||
		payload.Sandboxes[0].Metadata["workspace_name"] != "Foo" {
		t.Fatalf("current sandbox = %+v, want local workspace metadata", payload.Sandboxes[0])
	}
	if payload.Sandboxes[0].CPUCount != 4 || payload.Sandboxes[0].MemoryGB != 8 || payload.Sandboxes[0].DiskSizeMB != 20480 {
		t.Fatalf("current sandbox resources = %+v, want remote cpu, memory, and disk", payload.Sandboxes[0])
	}
	if !payload.Sandboxes[0].LocalSession {
		t.Fatalf("current sandbox local_session = false, want true")
	}
	if payload.Sandboxes[1].SandboxID != "sandbox-remote-only" {
		t.Fatalf("second sandbox = %+v, want remote-only sandbox", payload.Sandboxes[1])
	}
	if payload.Sandboxes[1].LocalSession {
		t.Fatalf("remote-only sandbox local_session = true, want false")
	}
}

func TestSandboxFilesListUsesSessionRegion(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/filesystem?path=/workspace/project", nil)
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
	if runtime.lastFilesystemEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("filesystem endpoint = %q, want session region", runtime.lastFilesystemEndpoint)
	}
	if runtime.lastFilesystemPath != "/workspace/project" {
		t.Fatalf("filesystem path = %q, want /workspace/project", runtime.lastFilesystemPath)
	}
	var payload sandboxFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Entries) != 2 || payload.Entries[0].Name != "README.md" || payload.Entries[1].Type != "dir" {
		t.Fatalf("payload entries = %+v, want fake files", payload.Entries)
	}
}

func TestSandboxFilesRejectsLargeDepth(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/filesystem?path=/&depth=9", nil)
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
	if runtime.lastFilesystemPath != "" {
		t.Fatalf("filesystem path = %q, want runtime not called", runtime.lastFilesystemPath)
	}
}

func TestSandboxFileContentRejectsRelativePath(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/filesystem/content?path=README.md", nil)
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

func TestSandboxFileContentReadsFile(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/filesystem/content?path=/workspace/project/README.md", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "hello from sandbox\n" {
		t.Fatalf("body = %q, want file content", got)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastFilesystemPath != "/workspace/project/README.md" {
		t.Fatalf("filesystem path = %q, want requested file", runtime.lastFilesystemPath)
	}
}

func TestSandboxFilePreviewServesHTMLWithSandboxCSP(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/preview/home/user/snake.html", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q, want html", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox;") || !strings.Contains(got, "connect-src 'none'") {
		t.Fatalf("csp = %q, want sandboxed preview without network access", got)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastAPIKey != "qiniu-api-key" {
		t.Fatalf("runtime api key = %q, want decrypted key", runtime.lastAPIKey)
	}
	if runtime.lastFilesystemEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("filesystem endpoint = %q, want session region", runtime.lastFilesystemEndpoint)
	}
	if runtime.lastFilesystemPath != "/home/user/snake.html" {
		t.Fatalf("filesystem path = %q, want requested preview file", runtime.lastFilesystemPath)
	}
}

func TestSandboxFilePreviewRejectsEmptyPath(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/preview/", nil)
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
	if runtime.lastFilesystemPath != "" {
		t.Fatalf("filesystem path = %q, want runtime not called", runtime.lastFilesystemPath)
	}
}

func TestWorkspaceFilePreviewUsesCurrentWorkspaceSandbox(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-current",
		TemplateID:    "node",
		State:         "running",
		WorkspacePath: "/workspace/Scratch",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+workspace.ID+"/preview/home/user/snake.html", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q, want html", got)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastFilesystemPath != "/home/user/snake.html" {
		t.Fatalf("filesystem path = %q, want requested preview file", runtime.lastFilesystemPath)
	}
	if runtime.lastFilesystemEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("filesystem endpoint = %q, want workspace region", runtime.lastFilesystemEndpoint)
	}
}

func TestWorkspaceFilePreviewReportsMissingRuntimeSandbox(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-gone",
		TemplateID:    "node",
		State:         "running",
		WorkspacePath: "/workspace/Scratch",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	ctrl.sandboxRuntime.(*fakeSandboxRuntime).readFileErr = &qiniusb.APIError{
		StatusCode: http.StatusBadGateway,
		Message:    "The sandbox was not found",
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+workspace.ID+"/preview/home/user/snake.html", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "workspace sandbox no longer exists") {
		t.Fatalf("body = %q, want missing sandbox message", rec.Body.String())
	}
}

func TestSandboxMetricsUsesSessionRegion(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/metrics?start=1780000000&end=1780000600", nil)
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
	if runtime.lastMetricsEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("metrics endpoint = %q, want session region", runtime.lastMetricsEndpoint)
	}
	if runtime.lastMetricsParams.Start == nil || *runtime.lastMetricsParams.Start != 1780000000 {
		t.Fatalf("start = %v, want 1780000000", runtime.lastMetricsParams.Start)
	}
	var payload sandboxMetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SandboxID != "sandbox-2" || len(payload.Metrics) != 1 {
		t.Fatalf("payload = %+v, want one sandbox metric", payload)
	}
	if payload.Metrics[0].CPUUsedPct != 12.5 || payload.Metrics[0].TimestampUnix != 1780000000 {
		t.Fatalf("metric = %+v, want fake metric", payload.Metrics[0])
	}
}

func TestSandboxMetricsRejectsInvalidRange(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-2",
		TemplateID: "base",
		State:      "running",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/sandbox-2/metrics?start=20&end=10", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if ctrl.sandboxRuntime.(*fakeSandboxRuntime).lastMetricsEndpoint != "" {
		t.Fatalf("metrics runtime should not be called")
	}
}

func saveEncryptedAPIKey(t *testing.T, ctrl *Ctrl, accountID, apiKey string) {
	t.Helper()
	saveEncryptedQiniuKeys(t, ctrl, accountID, apiKey, "")
}

func saveEncryptedQiniuKeys(t *testing.T, ctrl *Ctrl, accountID, apiKey, maasAPIKey string) {
	t.Helper()

	encrypted, err := ctrl.credentialBox.Encrypt(apiKey)
	if err != nil {
		t.Fatalf("encrypt api key: %v", err)
	}
	var maasKeyHint string
	var encryptedMAASAPIKey string
	if maasAPIKey != "" {
		maasKeyHint = keyHint(maasAPIKey)
		encryptedMAASAPIKey, err = ctrl.credentialBox.Encrypt(maasAPIKey)
		if err != nil {
			t.Fatalf("encrypt maas api key: %v", err)
		}
	}
	if _, err := ctrl.service.SaveQiniuCredential(httptest.NewRequest(http.MethodGet, "/", nil).Context(), accountID, service.QiniuCredentialInput{
		KeyHint:             keyHint(apiKey),
		EncryptedAPIKey:     encrypted,
		MAASKeyHint:         maasKeyHint,
		EncryptedMAASAPIKey: encryptedMAASAPIKey,
	}); err != nil {
		t.Fatalf("save qiniu credential: %v", err)
	}
}
