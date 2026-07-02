package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestOpenRepositoryCreatesSandboxAndStartsIDE(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
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
	if runtime.lastPrepareRequest.Endpoint != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("prepare endpoint = %q, want selected region", runtime.lastPrepareRequest.Endpoint)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.IDEURL == "" || payload.WorkspacePath == "" {
		t.Fatalf("payload = %+v, want IDE URL and workspace path", payload)
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
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")

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
	if runtime.lastCreateRequest.TemplateID != "node" {
		t.Fatalf("runtime template id = %q, want node", runtime.lastCreateRequest.TemplateID)
	}
	var payload workspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.GitHubRepoID != nil || payload.RepoFullName != "" {
		t.Fatalf("payload repo = %v/%q, want no repository binding", payload.GitHubRepoID, payload.RepoFullName)
	}
	if payload.Name != "Scratch_workspace" {
		t.Fatalf("Name = %q, want Scratch_workspace", payload.Name)
	}
	if payload.IDEURL == "" || payload.WorkspacePath != "/workspace/Scratch_workspace" {
		t.Fatalf("payload = %+v, want IDE URL and named workspace path", payload)
	}
	if payload.Region != "https://cn-yangzhou-1-sandbox.qiniuapi.com" || payload.TemplateID != "node" {
		t.Fatalf("payload workspace config = %q/%q, want selected config", payload.Region, payload.TemplateID)
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

func TestRedactSecretRemovesRawAndEscapedValues(t *testing.T) {
	secret := "tok/en+value"
	message := "clone failed for tok/en+value and tok%2Fen%2Bvalue"
	got := redactSecret(message, secret)
	if strings.Contains(got, secret) || strings.Contains(got, "tok%2Fen%2Bvalue") {
		t.Fatalf("redactSecret() = %q, want secret redacted", got)
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
