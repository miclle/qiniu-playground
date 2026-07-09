package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	qiniusb "github.com/qiniu/go-sdk/v7/sandbox"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

func TestOpenRepositoryCreatesSandboxAndStartsIDE(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	if _, err := ctrl.service.SaveGitHubInstallation(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.GitHubInstallationInput{InstallationID: 42}); err != nil {
		t.Fatalf("save installation: %v", err)
	}
	repos, err := ctrl.service.SaveGitHubRepositories(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, 42, []service.GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	})
	if err != nil {
		t.Fatalf("save repositories: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/"+repos[0].ID+"/open", bytes.NewReader([]byte(`{
		"name": "Hello_workspace",
		"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		"template_id": "node"
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
	if runtime.lastRepo != "octocat/hello-world" {
		t.Fatalf("runtime repo = %q, want octocat/hello-world", runtime.lastRepo)
	}
	if runtime.lastCreateRequest.Endpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("runtime endpoint = %q, want selected region", runtime.lastCreateRequest.Endpoint)
	}
	if runtime.lastCreateRequest.TemplateID != "node" {
		t.Fatalf("runtime template id = %q, want node", runtime.lastCreateRequest.TemplateID)
	}
	if runtime.lastCreateRequest.Metadata["created_by"] != "qiniu-playground" ||
		runtime.lastCreateRequest.Metadata["kind"] != "workspace" ||
		runtime.lastCreateRequest.Metadata["repo_full_name"] != "octocat/hello-world" ||
		runtime.lastCreateRequest.Metadata["workspace_name"] != "Hello_workspace" {
		t.Fatalf("runtime metadata = %#v, want workspace metadata", runtime.lastCreateRequest.Metadata)
	}
	if runtime.lastPrepareRequest.Endpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("prepare endpoint = %q, want selected region", runtime.lastPrepareRequest.Endpoint)
	}
	if runtime.lastPrepareRequest.Envs["QINIU_MAAS_API_KEY"] != "qiniu-maas-key" ||
		runtime.lastPrepareRequest.Envs["ANTHROPIC_AUTH_TOKEN"] != "qiniu-maas-key" ||
		runtime.lastPrepareRequest.Envs["ANTHROPIC_BASE_URL"] != "https://api.qnaigc.com" {
		t.Fatalf("prepare envs = %#v, want MAAS key injected", runtime.lastPrepareRequest.Envs)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.IDEURL == "" || payload.WorkspacePath == "" {
		t.Fatalf("payload = %+v, want IDE URL and workspace path", payload)
	}
	if runtime.lastCreateRequest.Metadata["workspace_id"] != payload.ID {
		t.Fatalf("metadata workspace_id = %q, want payload id %q", runtime.lastCreateRequest.Metadata["workspace_id"], payload.ID)
	}
	if payload.RepoFullName != "octocat/hello-world" {
		t.Fatalf("RepoFullName = %q, want octocat/hello-world", payload.RepoFullName)
	}
	if payload.Name != "Hello_workspace" {
		t.Fatalf("Name = %q, want Hello_workspace", payload.Name)
	}
	if payload.Region != "https://cn-yangzhou-1-sandbox.qiniuapi.com" || payload.TemplateID != "node" {
		t.Fatalf("payload workspace config = %q/%q, want selected config", payload.Region, payload.TemplateID)
	}

	reopenReq := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/"+repos[0].ID+"/open", bytes.NewReader([]byte(`{
		"name": "Hello_workspace",
		"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		"template_id": "node"
	}`)))
	reopenReq.Header.Set("Content-Type", "application/json")
	reopenReq.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	reopenRec := httptest.NewRecorder()

	router.ServeHTTP(reopenRec, reopenReq)

	if reopenRec.Code != http.StatusOK {
		t.Fatalf("reopen status = %d, want 200 body=%s", reopenRec.Code, reopenRec.Body.String())
	}
	var reopenPayload workspaceResponse
	if err := json.Unmarshal(reopenRec.Body.Bytes(), &reopenPayload); err != nil {
		t.Fatalf("decode reopen response: %v", err)
	}
	if reopenPayload.ID != payload.ID {
		t.Fatalf("reopened workspace id = %q, want existing id %q", reopenPayload.ID, payload.ID)
	}
	if runtime.lastCreateRequest.Metadata["workspace_id"] != payload.ID {
		t.Fatalf("reopened metadata workspace_id = %q, want existing id %q", runtime.lastCreateRequest.Metadata["workspace_id"], payload.ID)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	listReq.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	listRec := httptest.NewRecorder()

	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", listRec.Code, listRec.Body.String())
	}
	var listPayload workspacesResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listPayload.Workspaces) != 1 || listPayload.Workspaces[0].RepoFullName != "octocat/hello-world" {
		t.Fatalf("workspaces = %+v, want created repository workspace", listPayload.Workspaces)
	}
}

func TestOpenRepositoryRejectsMissingTemplate(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	repos, err := ctrl.service.SaveGitHubRepositories(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, 42, []service.GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	})
	if err != nil {
		t.Fatalf("save repositories: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/"+repos[0].ID+"/open", bytes.NewReader([]byte(`{
		"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com"
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

func TestCreateWorkspaceWithoutRepository(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{
		"name": "Scratch_workspace",
		"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		"template_id": "node"
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
	if runtime.lastWorkspaceRequest.WorkspacePath != "/workspace/Scratch_workspace" {
		t.Fatalf("workspace path = %q, want /workspace/Scratch_workspace", runtime.lastWorkspaceRequest.WorkspacePath)
	}
	if runtime.lastWorkspaceRequest.Envs["QINIU_MAAS_API_KEY"] != "qiniu-maas-key" ||
		runtime.lastWorkspaceRequest.Envs["ANTHROPIC_AUTH_TOKEN"] != "qiniu-maas-key" ||
		runtime.lastWorkspaceRequest.Envs["ANTHROPIC_BASE_URL"] != "https://api.qnaigc.com" {
		t.Fatalf("workspace envs = %#v, want MAAS key injected", runtime.lastWorkspaceRequest.Envs)
	}
	if runtime.lastCreateRequest.TemplateID != "node" {
		t.Fatalf("runtime template id = %q, want node", runtime.lastCreateRequest.TemplateID)
	}
	if runtime.lastCreateRequest.TimeoutSeconds != 86400 || runtime.lastWorkspaceRequest.TimeoutSeconds != 86400 {
		t.Fatalf("runtime timeout = create %d prepare %d, want 86400", runtime.lastCreateRequest.TimeoutSeconds, runtime.lastWorkspaceRequest.TimeoutSeconds)
	}
	if runtime.lastCreateRequest.Metadata["created_by"] != "qiniu-playground" ||
		runtime.lastCreateRequest.Metadata["kind"] != "workspace" ||
		runtime.lastCreateRequest.Metadata["workspace_name"] != "Scratch_workspace" ||
		runtime.lastCreateRequest.Metadata["workspace_path"] != "/workspace/Scratch_workspace" {
		t.Fatalf("runtime metadata = %#v, want scratch workspace metadata", runtime.lastCreateRequest.Metadata)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.GitHubRepoID != nil || payload.RepoFullName != "" {
		t.Fatalf("payload repo = %v/%q, want no repository binding", payload.GitHubRepoID, payload.RepoFullName)
	}
	if runtime.lastCreateRequest.Metadata["workspace_id"] != payload.ID {
		t.Fatalf("metadata workspace_id = %q, want payload id %q", runtime.lastCreateRequest.Metadata["workspace_id"], payload.ID)
	}
	if payload.Name != "Scratch_workspace" {
		t.Fatalf("Name = %q, want Scratch_workspace", payload.Name)
	}
	if payload.IDEURL == "" || payload.WorkspacePath != "/workspace/Scratch_workspace" {
		t.Fatalf("payload = %+v, want IDE URL and named workspace path", payload)
	}
	if _, err := uuid.Parse(payload.ID); err != nil {
		t.Fatalf("workspace id = %q, want uuid: %v", payload.ID, err)
	}
	if payload.CreatedAt.IsZero() || payload.UpdatedAt.IsZero() {
		t.Fatalf("payload timestamps = %s/%s, want created and updated times", payload.CreatedAt, payload.UpdatedAt)
	}
	if payload.Region != "https://cn-yangzhou-1-sandbox.qiniuapi.com" || payload.TemplateID != "node" {
		t.Fatalf("payload workspace config = %q/%q, want selected config", payload.Region, payload.TemplateID)
	}
}

func TestWorkspaceHeartbeatRefreshesSandboxTimeout(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch_workspace",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-1",
		TemplateID:    "node",
		State:         "running",
		Endpoint:      "sandbox-1.example.test",
		WorkspacePath: "/workspace/Scratch_workspace",
		IDEURL:        "https://sandbox-1.example.test",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/heartbeat", nil)
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
	if runtime.lastTimeoutSandboxID != "sandbox-1" {
		t.Fatalf("timeout sandbox = %q, want sandbox-1", runtime.lastTimeoutSandboxID)
	}
	if runtime.lastTimeoutEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("timeout endpoint = %q, want workspace region", runtime.lastTimeoutEndpoint)
	}
	if runtime.lastTimeoutSeconds != 86400 {
		t.Fatalf("timeout seconds = %d, want 86400", runtime.lastTimeoutSeconds)
	}
	var payload struct {
		OK             bool  `json:"ok"`
		TimeoutSeconds int32 `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || payload.TimeoutSeconds != 86400 {
		t.Fatalf("payload = %+v, want ok timeout 86400", payload)
	}
}

func TestPauseWorkspaceSandboxMarksWorkspacePaused(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch_workspace",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-1",
		TemplateID:    "node",
		State:         "running",
		Endpoint:      "sandbox-1.example.test",
		WorkspacePath: "/workspace/Scratch_workspace",
		IDEURL:        "https://sandbox-1.example.test",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/pause", nil)
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
	if runtime.lastPausedSandboxID != "sandbox-1" {
		t.Fatalf("paused sandbox = %q, want sandbox-1", runtime.lastPausedSandboxID)
	}
	if runtime.lastPauseEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("pause endpoint = %q, want workspace region", runtime.lastPauseEndpoint)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.State != "paused" {
		t.Fatalf("payload state = %q, want paused", payload.State)
	}
	updated, err := ctrl.service.Workspace(req.Context(), user.AccountID, workspace.ID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	if updated.State != "paused" {
		t.Fatalf("workspace state = %q, want paused", updated.State)
	}
}

func TestCreateWorkspaceRejectsInvalidName(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{
		"name": "Scratch workspace",
		"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		"template_id": "node"
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

func TestCreateWorkspaceRequiresSandboxAPIKey(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	if err := ctrl.service.DeleteQiniuCredential(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID); err != nil {
		t.Fatalf("delete credential: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{
		"region": "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		"template_id": "node"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428 body=%s", rec.Code, rec.Body.String())
	}
}

func TestConnectWorkspaceRestoresSandboxAndStartsIDE(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-2",
		TemplateID:    "node",
		State:         "stopped",
		Endpoint:      "old.example.test",
		WorkspacePath: "/workspace/Scratch",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/connect", nil)
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
	if runtime.lastConnectEndpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("connect endpoint = %q, want workspace region", runtime.lastConnectEndpoint)
	}
	if runtime.lastWorkspaceRequest.SandboxID != "sandbox-2" || runtime.lastWorkspaceRequest.WorkspacePath != "/workspace/Scratch" {
		t.Fatalf("workspace request = %+v, want sandbox and path restored", runtime.lastWorkspaceRequest)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ID != workspace.ID || payload.IDEURL == "" {
		t.Fatalf("payload = %+v, want same workspace with IDE URL", payload)
	}
	if payload.State != "running" || payload.Endpoint != "sandbox-2.example.test" {
		t.Fatalf("payload runtime = %q/%q, want restored runtime", payload.State, payload.Endpoint)
	}
}

func TestConnectWorkspaceReportsMissingSandboxBeforeRecreate(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-gone",
		TemplateID:    "node",
		WorkspacePath: "/workspace/Scratch",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	ctrl.sandboxRuntime.(*fakeSandboxRuntime).connectErr = &qiniusb.APIError{StatusCode: http.StatusNotFound}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/connect", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastCreateRequest.TemplateID != "" {
		t.Fatalf("create request = %+v, want no sandbox recreation", runtime.lastCreateRequest)
	}
}

func TestConnectWorkspaceRecreatesMissingSandboxWhenRequested(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Scratch",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-gone",
		TemplateID:    "node",
		WorkspacePath: "/workspace/Scratch",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	ctrl.sandboxRuntime.(*fakeSandboxRuntime).connectErr = &qiniusb.APIError{StatusCode: http.StatusNotFound}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/connect", bytes.NewReader([]byte(`{"recreate":true}`)))
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
	if runtime.lastCreateRequest.TemplateID != "node" {
		t.Fatalf("create request = %+v, want workspace template", runtime.lastCreateRequest)
	}
	if runtime.lastCreateRequest.Metadata["workspace_id"] != workspace.ID ||
		runtime.lastCreateRequest.Metadata["workspace_name"] != "Scratch" ||
		runtime.lastCreateRequest.Metadata["workspace_path"] != "/workspace/Scratch" {
		t.Fatalf("runtime metadata = %#v, want recreated workspace metadata", runtime.lastCreateRequest.Metadata)
	}
	if runtime.lastWorkspaceRequest.SandboxID != "sandbox-1" || runtime.lastWorkspaceRequest.WorkspacePath != "/workspace/Scratch" {
		t.Fatalf("workspace request = %+v, want recreated sandbox with existing path", runtime.lastWorkspaceRequest)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SandboxID != "sandbox-1" || payload.IDEURL == "" {
		t.Fatalf("payload = %+v, want recreated sandbox runtime", payload)
	}
}

func TestConnectWorkspaceRecreatesRepositoryWorkspace(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	if _, err := ctrl.service.SaveGitHubInstallation(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.GitHubInstallationInput{InstallationID: 42}); err != nil {
		t.Fatalf("save installation: %v", err)
	}
	githubRepoID := int64(100)
	if _, err := ctrl.service.SaveGitHubRepositories(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, 42, []service.GitHubRepositoryInput{
		{
			GitHubRepoID:  githubRepoID,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	}); err != nil {
		t.Fatalf("save repositories: %v", err)
	}
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Hello",
		GitHubRepoID:  &githubRepoID,
		RepoFullName:  "octocat/hello-world",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-gone",
		TemplateID:    "node",
		WorkspacePath: "/workspace/octocat-hello-world",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/connect", bytes.NewReader([]byte(`{"recreate":true}`)))
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
	if runtime.lastRepo != "octocat/hello-world" {
		t.Fatalf("runtime repo = %q, want repository prepared", runtime.lastRepo)
	}
	if runtime.lastPrepareRequest.DefaultBranch != "main" || runtime.lastPrepareRequest.Token != "installation-token" {
		t.Fatalf("prepare request = %+v, want default branch and installation token", runtime.lastPrepareRequest)
	}
	if runtime.lastPrepareRequest.Envs["QINIU_MAAS_API_KEY"] != "qiniu-maas-key" ||
		runtime.lastPrepareRequest.Envs["ANTHROPIC_AUTH_TOKEN"] != "qiniu-maas-key" ||
		runtime.lastPrepareRequest.Envs["ANTHROPIC_BASE_URL"] != "https://api.qnaigc.com" {
		t.Fatalf("prepare envs = %#v, want MAAS key injected", runtime.lastPrepareRequest.Envs)
	}
	if runtime.lastPrepareRequest.WorkspacePath != "/workspace/octocat-hello-world" {
		t.Fatalf("prepare workspace path = %q, want saved workspace path", runtime.lastPrepareRequest.WorkspacePath)
	}
	if runtime.lastWorkspaceRequest.SandboxID != "" {
		t.Fatalf("workspace request = %+v, want repository prepare path", runtime.lastWorkspaceRequest)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.RepoFullName != "octocat/hello-world" || payload.WorkspacePath != "/workspace/octocat-hello-world" {
		t.Fatalf("payload = %+v, want recreated repository workspace", payload)
	}
}

func TestRedactSecretRemovesRawAndEscapedValues(t *testing.T) {
	secret := "tok/en+value"
	message := "clone failed for tok/en+value and tok%2Fen%2Bvalue"
	got := redactSecret(message, secret)
	if strings.Contains(got, secret) || strings.Contains(got, "tok%2Fen%2Bvalue") {
		t.Fatalf("redactSecret() = %q, want secret redacted", got)
	}
}

func TestWorkspaceRuntimePathFallsBackConsistently(t *testing.T) {
	githubRepoID := int64(100)
	for _, tt := range []struct {
		name      string
		workspace entity.Workspace
		want      string
	}{
		{
			name: "preserves saved path",
			workspace: entity.Workspace{
				WorkspacePath: "/workspace/saved",
				GitHubRepoID:  &githubRepoID,
				RepoFullName:  "octocat/hello-world",
			},
			want: "/workspace/saved",
		},
		{
			name: "repository path uses repository full name",
			workspace: entity.Workspace{
				GitHubRepoID: &githubRepoID,
				RepoFullName: "octocat/hello-world",
				Name:         "hello-world",
			},
			want: "/workspace/octocat__hello-world",
		},
		{
			name: "scratch path uses workspace name",
			workspace: entity.Workspace{
				Name: "Scratch",
			},
			want: "/workspace/Scratch",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspaceRuntimePath(&tt.workspace); got != tt.want {
				t.Fatalf("workspaceRuntimePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListWorkspacesBackfillDoesNotRefreshExistingWorkspace(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	githubRepoID := int64(100)
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:     "sandbox-1",
		TemplateID:    "template-1",
		State:         "running",
		Endpoint:      "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		GitHubRepoID:  &githubRepoID,
		RepoFullName:  "octocat/hello-world",
		WorkspacePath: "/workspace/octocat-hello-world",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		CPUCount:      2,
		MemoryGB:      4,
		IDEURL:        "https://ide.example.test",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}

	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	workspace, err := ctrl.service.WorkspaceByGitHubRepoID(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, githubRepoID)
	if err != nil {
		t.Fatalf("find backfilled workspace: %v", err)
	}
	firstUpdatedAt := workspace.UpdatedAt
	time.Sleep(10 * time.Millisecond)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("second list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	workspace, err = ctrl.service.WorkspaceByGitHubRepoID(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, githubRepoID)
	if err != nil {
		t.Fatalf("find backfilled workspace after second list: %v", err)
	}
	if !workspace.UpdatedAt.Equal(firstUpdatedAt) {
		t.Fatalf("UpdatedAt changed from %s to %s", firstUpdatedAt, workspace.UpdatedAt)
	}
}
