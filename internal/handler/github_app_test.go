package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestGitHubAppInstallReturnsInstallURL(t *testing.T) {
	router := newTestRouter(newTestController(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/github/app/install", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "https://github.com/apps/qiniu-playground-test/installations/new") {
		t.Fatalf("body = %s, want install URL", rec.Body.String())
	}
}

func TestGitHubAppCallbackStoresInstallation(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/github/app/callback?installation_id=42", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	installations, err := ctrl.service.ListGitHubInstallations(req.Context(), user.AccountID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(installations) != 1 || installations[0].InstallationID != 42 {
		t.Fatalf("installations = %+v, want installation 42", installations)
	}
	if installations[0].TargetLogin != "octocat" {
		t.Fatalf("TargetLogin = %q, want octocat", installations[0].TargetLogin)
	}
}

func TestGitHubAppCallbackRejectsOtherAccountInstallation(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/github/app/callback?installation_id=99", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
	installations, err := ctrl.service.ListGitHubInstallations(req.Context(), user.AccountID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(installations) != 0 {
		t.Fatalf("installations = %+v, want none", installations)
	}
}

func TestGitHubAppCallbackAllowsActiveOrganizationMember(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	encryptedToken, err := ctrl.credentialBox.Encrypt("github-token")
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	if err := ctrl.service.SaveGitHubAccessToken(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, encryptedToken); err != nil {
		t.Fatalf("save github token: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/github/app/callback?installation_id=100", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 body=%s", rec.Code, rec.Body.String())
	}
	installations, err := ctrl.service.ListGitHubInstallations(req.Context(), user.AccountID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(installations) != 1 || installations[0].InstallationID != 100 || installations[0].TargetLogin != "qiniu" {
		t.Fatalf("installations = %+v, want qiniu org installation", installations)
	}
}

func TestGitHubRepositoriesSyncsFromInstallation(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	if _, err := ctrl.service.SaveGitHubInstallation(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, installationInput(42)); err != nil {
		t.Fatalf("save installation: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/github/repositories", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload repositoriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Repositories) != 1 {
		t.Fatalf("len(repositories) = %d, want 1", len(payload.Repositories))
	}
	if payload.Repositories[0].FullName != "octocat/hello-world" {
		t.Fatalf("FullName = %q, want octocat/hello-world", payload.Repositories[0].FullName)
	}
}

func TestGitHubRepositoriesRecoversMatchingInstalledApp(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/github/repositories", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	installations, err := ctrl.service.ListGitHubInstallations(req.Context(), user.AccountID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(installations) != 1 || installations[0].InstallationID != 42 {
		t.Fatalf("installations = %+v, want recovered installation 42", installations)
	}
	var payload repositoriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Repositories) != 1 || payload.Repositories[0].FullName != "octocat/hello-world" {
		t.Fatalf("repositories = %+v, want recovered repo", payload.Repositories)
	}
}

func createAuthenticatedUser(t *testing.T, ctrl *Ctrl) *authUserForTest {
	t.Helper()

	user, err := ctrl.service.UpsertGitHubIdentity(httptest.NewRequest(http.MethodGet, "/", nil).Context(), testOAuthUser())
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	return &authUserForTest{AccountID: user.AccountID}
}

type authUserForTest struct {
	AccountID string
}

func testOAuthUser() service.OAuthUser {
	return service.OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
		DisplayName:     "The Octocat",
	}
}

func installationInput(id int64) service.GitHubInstallationInput {
	return service.GitHubInstallationInput{InstallationID: id}
}
