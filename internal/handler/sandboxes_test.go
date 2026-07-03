package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestCreateSandboxUsesStoredAPIKey(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes", bytes.NewReader([]byte(`{"template_id":"base"}`)))
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
	var payload sandboxSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SandboxID != "sandbox-1" || payload.TemplateID != "base" {
		t.Fatalf("payload = %+v, want created sandbox", payload)
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/sandbox-2/connect", nil)
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
}

func saveEncryptedAPIKey(t *testing.T, ctrl *Ctrl, accountID, apiKey string) {
	t.Helper()

	encrypted, err := ctrl.credentialBox.Encrypt(apiKey)
	if err != nil {
		t.Fatalf("encrypt api key: %v", err)
	}
	if _, err := ctrl.service.SaveQiniuCredential(httptest.NewRequest(http.MethodGet, "/", nil).Context(), accountID, service.QiniuCredentialInput{
		KeyHint:         keyHint(apiKey),
		EncryptedAPIKey: encrypted,
	}); err != nil {
		t.Fatalf("save qiniu credential: %v", err)
	}
}
