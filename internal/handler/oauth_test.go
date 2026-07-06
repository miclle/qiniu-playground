package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/config"
	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

type fakeGitHubOAuth struct {
	user           service.OAuthUser
	exchangeErr    error
	fetchErr       error
	orgMemberships map[string]githubOrgMembership
}

type fakeGitHubApp struct {
	repos []service.GitHubRepositoryInput
	token string
}

type fakeSandboxRuntime struct {
	lastAPIKey             string
	lastCreateRequest      sandboxRuntimeCreateRequest
	lastWorkspaceRequest   sandboxRuntimeWorkspaceRequest
	lastPrepareRequest     sandboxRuntimeRepositoryRequest
	lastTemplatesEndpoint  string
	lastConnectEndpoint    string
	lastPTYEndpoint        string
	lastFilesystemEndpoint string
	lastFilesystemPath     string
	lastMetricsEndpoint    string
	lastMetricsParams      sandboxMetricsParams
	lastRepo               string
	lastPTYInput           string
	lastPTYSize            sandboxPTYSize
	onPTYData              func([]byte)
	templates              []sandboxRuntimeTemplate
	connectErr             error
}

type fakePTYSession struct {
	runtime *fakeSandboxRuntime
}

func (f fakeGitHubApp) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	if f.token == "" {
		return "installation-token", nil
	}
	return f.token, nil
}

func (f fakeGitHubApp) ListAppInstallations(ctx context.Context) ([]service.GitHubInstallationInput, error) {
	return []service.GitHubInstallationInput{
		{
			InstallationID:      42,
			TargetType:          "User",
			TargetLogin:         "octocat",
			RepositorySelection: "selected",
		},
		{
			InstallationID:      99,
			TargetType:          "User",
			TargetLogin:         "mallory",
			RepositorySelection: "selected",
		},
		{
			InstallationID:      100,
			TargetType:          "Organization",
			TargetLogin:         "qiniu",
			RepositorySelection: "selected",
		},
	}, nil
}

func (f fakeGitHubApp) ListInstallationRepositories(ctx context.Context, installationID int64) ([]service.GitHubRepositoryInput, error) {
	return f.repos, nil
}

func (f *fakeSandboxRuntime) ListTemplates(ctx context.Context, apiKey, endpoint string) ([]sandboxRuntimeTemplate, error) {
	f.lastAPIKey = apiKey
	f.lastTemplatesEndpoint = endpoint
	if f.templates != nil {
		return f.templates, nil
	}
	return []sandboxRuntimeTemplate{
		{TemplateID: "base", BuildStatus: "ready", CPUCount: 2, MemoryMB: 1024, DiskSizeMB: 10240, Public: true},
	}, nil
}

func (f *fakeSandboxRuntime) Create(ctx context.Context, apiKey string, req sandboxRuntimeCreateRequest) (*sandboxRuntimeInfo, error) {
	f.lastAPIKey = apiKey
	f.lastCreateRequest = req
	return &sandboxRuntimeInfo{
		SandboxID:  "sandbox-1",
		TemplateID: req.TemplateID,
		State:      "running",
		Endpoint:   "sandbox-1.example.test",
	}, nil
}

func (f *fakeSandboxRuntime) Connect(ctx context.Context, apiKey, sandboxID string, timeoutSeconds int32, endpoint string) (*sandboxRuntimeInfo, error) {
	f.lastAPIKey = apiKey
	f.lastConnectEndpoint = endpoint
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return &sandboxRuntimeInfo{
		SandboxID:  sandboxID,
		TemplateID: "base",
		State:      "running",
		Endpoint:   sandboxID + ".example.test",
	}, nil
}

func (f *fakeSandboxRuntime) PrepareWorkspace(ctx context.Context, apiKey string, req sandboxRuntimeWorkspaceRequest) (*sandboxRuntimeWorkspace, error) {
	f.lastAPIKey = apiKey
	f.lastWorkspaceRequest = req
	workspacePath := req.WorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace"
	}
	return &sandboxRuntimeWorkspace{
		SandboxID:     req.SandboxID,
		TemplateID:    f.lastCreateRequest.TemplateID,
		State:         "running",
		Endpoint:      req.SandboxID + ".example.test",
		WorkspacePath: workspacePath,
		IDEURL:        "https://" + req.SandboxID + ".example.test",
	}, nil
}

func (f *fakeSandboxRuntime) PrepareRepository(ctx context.Context, apiKey string, req sandboxRuntimeRepositoryRequest) (*sandboxRuntimeWorkspace, error) {
	f.lastAPIKey = apiKey
	f.lastPrepareRequest = req
	f.lastRepo = req.FullName
	workspacePath := req.WorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace/" + req.FullName
	}
	return &sandboxRuntimeWorkspace{
		SandboxID:     req.SandboxID,
		TemplateID:    f.lastCreateRequest.TemplateID,
		State:         "running",
		Endpoint:      req.SandboxID + ".example.test",
		WorkspacePath: workspacePath,
		IDEURL:        "https://" + req.SandboxID + ".example.test",
	}, nil
}

func (f *fakeSandboxRuntime) StartPTY(ctx context.Context, apiKey, sandboxID string, endpoint string, size sandboxPTYSize, onData func([]byte)) (sandboxPTYSession, error) {
	f.lastAPIKey = apiKey
	f.lastPTYEndpoint = endpoint
	f.lastPTYSize = size
	f.onPTYData = onData
	onData([]byte("connected\n"))
	return &fakePTYSession{runtime: f}, nil
}

func (f *fakeSandboxRuntime) ListFiles(ctx context.Context, apiKey, sandboxID, endpoint, filePath string, depth uint32) ([]sandboxRuntimeFileEntry, error) {
	f.lastAPIKey = apiKey
	f.lastFilesystemEndpoint = endpoint
	f.lastFilesystemPath = filePath
	return []sandboxRuntimeFileEntry{
		{Name: "README.md", Type: "file", Path: pathJoin(filePath, "README.md"), Size: 42, Permissions: "-rw-r--r--", Owner: "user", Group: "user"},
		{Name: "src", Type: "dir", Path: pathJoin(filePath, "src"), Permissions: "drwxr-xr-x", Owner: "user", Group: "user"},
	}, nil
}

func (f *fakeSandboxRuntime) ReadFileStream(ctx context.Context, apiKey, sandboxID, endpoint, filePath string) (io.ReadCloser, error) {
	f.lastAPIKey = apiKey
	f.lastFilesystemEndpoint = endpoint
	f.lastFilesystemPath = filePath
	return io.NopCloser(strings.NewReader("hello from sandbox\n")), nil
}

func (f *fakeSandboxRuntime) GetMetrics(ctx context.Context, apiKey, sandboxID, endpoint string, params sandboxMetricsParams) ([]sandboxRuntimeMetric, error) {
	f.lastAPIKey = apiKey
	f.lastMetricsEndpoint = endpoint
	f.lastMetricsParams = params
	now := time.Unix(1_780_000_000, 0).UTC()
	return []sandboxRuntimeMetric{
		{
			CPUCount:      2,
			CPUUsedPct:    12.5,
			MemTotal:      4 * 1024 * 1024 * 1024,
			MemUsed:       1536 * 1024 * 1024,
			DiskTotal:     20 * 1024 * 1024 * 1024,
			DiskUsed:      6 * 1024 * 1024 * 1024,
			Timestamp:     now,
			TimestampUnix: now.Unix(),
		},
	}, nil
}

func pathJoin(basePath, name string) string {
	if basePath == "/" {
		return "/" + name
	}
	return strings.TrimRight(basePath, "/") + "/" + name
}

func (s *fakePTYSession) Send(ctx context.Context, data []byte) error {
	s.runtime.lastPTYInput = string(data)
	s.runtime.onPTYData([]byte("echo:" + string(data)))
	return nil
}

func (s *fakePTYSession) Resize(ctx context.Context, size sandboxPTYSize) error {
	s.runtime.lastPTYSize = size
	return nil
}

func (s *fakePTYSession) Close(ctx context.Context) error {
	return nil
}

func (f fakeGitHubOAuth) AuthCodeURL(state string) string {
	return "https://github.com/login/oauth/authorize?client_id=test-client&state=" + url.QueryEscape(state)
}

func (f fakeGitHubOAuth) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	return &oauth2.Token{AccessToken: "test-token"}, nil
}

func (f fakeGitHubOAuth) FetchUser(ctx context.Context, token *oauth2.Token) (*service.OAuthUser, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return &f.user, nil
}

func (f fakeGitHubOAuth) OrgMembership(ctx context.Context, accessToken, org string) (*githubOrgMembership, error) {
	if membership, ok := f.orgMemberships[org]; ok {
		return &membership, nil
	}
	return nil, httperrors.New(http.StatusForbidden, "organization installation is not authorized for this account")
}

func newTestController(t *testing.T) *Ctrl {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.Account{},
		&entity.OAuthIdentity{},
		&entity.GitHubInstallation{},
		&entity.GitHubRepository{},
		&entity.Workspace{},
		&entity.QiniuCredential{},
		&entity.SandboxSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctrl := New(svc, &config.Config{
		Auth: config.AuthConfig{
			SessionSecret: "test-session-secret",
			EncryptionKey: "test-encryption-key",
		},
		GitHub: config.GitHubConfig{
			OAuthClientID:     "test-client",
			OAuthClientSecret: "test-secret",
			OAuthRedirectURL:  "http://example.test/api/v1/auth/github/callback",
		},
	})
	ctrl.githubOAuth = fakeGitHubOAuth{user: service.OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
		DisplayName:     "The Octocat",
		AvatarURL:       "https://avatars.example/octocat.png",
	}, orgMemberships: map[string]githubOrgMembership{
		"qiniu": {State: "active", Role: "member"},
	}}
	ctrl.githubAppSlug = "qiniu-playground-test"
	ctrl.githubApp = fakeGitHubApp{repos: []service.GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			Private:       true,
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	}}
	ctrl.sandboxRuntime = &fakeSandboxRuntime{}
	return ctrl
}

func newTestRouter(ctrl *Ctrl) *fox.Engine {
	router := fox.Default()
	ctrl.RegisterRoutes(router)
	return router
}

func TestGitHubLoginRedirectsAndSetsState(t *testing.T) {
	router := newTestRouter(newTestController(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/login", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if rec.Header().Get("Location") == "" {
		t.Fatal("Location should be set")
	}
	if cookie := responseCookie(rec.Result(), oauthStateCookie); cookie == nil {
		t.Fatal("state cookie missing")
	}
}

func TestGitHubCallbackRejectsInvalidState(t *testing.T) {
	router := newTestRouter(newTestController(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?code=abc&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "right"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGitHubCallbackCreatesSession(t *testing.T) {
	ctrl := newTestController(t)
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?code=abc&state=right", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "right"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	sessionCookie := responseCookie(rec.Result(), sessionCookieName)
	if sessionCookie == nil {
		t.Fatal("session cookie missing")
	}
	accountID, err := ctrl.sessionSigner.Verify(sessionCookie.Value, time.Now())
	if err != nil {
		t.Fatalf("verify session: %v", err)
	}
	if accountID == "" {
		t.Fatal("account ID should be encoded in the session")
	}
}

func TestGitHubCallbackHidesProviderErrors(t *testing.T) {
	ctrl := newTestController(t)
	ctrl.githubOAuth = fakeGitHubOAuth{
		exchangeErr: errors.New("provider leaked secret endpoint"),
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?code=abc&state=right", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "right"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "provider leaked secret endpoint") {
		t.Fatalf("response leaked provider error: %s", rec.Body.String())
	}
}

func responseCookie(resp *http.Response, name string) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestMeReturnsAuthenticatedUser(t *testing.T) {
	ctrl := newTestController(t)
	user, err := ctrl.service.UpsertGitHubIdentity(context.Background(), service.OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
		DisplayName:     "The Octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
