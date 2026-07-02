package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSandboxTemplatesRequiresAPIKey(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	if err := ctrl.service.DeleteQiniuCredential(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID); err != nil {
		t.Fatalf("delete credential: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
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

func TestSandboxTemplatesReturnsRuntimeTemplates(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.templates = []sandboxRuntimeTemplate{
		{TemplateID: "base", BuildStatus: "ready", CPUCount: 2, MemoryMB: 1024, DiskSizeMB: 10240, Public: true},
		{TemplateID: "node", Aliases: []string{"node:20"}, BuildStatus: "building", CPUCount: 4, MemoryMB: 2048, DiskSizeMB: 20480},
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
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
	var payload struct {
		DefaultTemplateID string                    `json:"default_template_id"`
		Templates         []sandboxTemplateResponse `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode templates: %v", err)
	}
	if payload.DefaultTemplateID != "base" || len(payload.Templates) != 2 {
		t.Fatalf("payload = %+v, want default base with two templates", payload)
	}
	if !payload.Templates[0].Default || !payload.Templates[0].Public || payload.Templates[0].CPUCount != 2 {
		t.Fatalf("first template = %+v, want default public base", payload.Templates[0])
	}
	if payload.Templates[1].Default || payload.Templates[1].Aliases[0] != "node:20" {
		t.Fatalf("second template = %+v, want non-default node alias", payload.Templates[1])
	}
}
