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

func TestSaveQiniuCredentialEncryptsSecrets(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/qiniu/credentials", bytes.NewReader([]byte(`{
		"sandbox_api_key": "test-sandbox-api-key",
		"maas_api_key": "test-maas-api-key",
		"access_key": "test-access-key",
		"secret_key": "test-secret-key"
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
	for _, plaintext := range []string{"test-sandbox-api-key", "test-maas-api-key", "test-access-key", "test-secret-key"} {
		if strings.Contains(rec.Body.String(), plaintext) {
			t.Fatalf("response should not contain plaintext secret %q: %s", plaintext, rec.Body.String())
		}
	}
	var status struct {
		Configured          bool   `json:"configured"`
		KeyHint             string `json:"key_hint"`
		MAASConfigured      bool   `json:"maas_configured"`
		MAASKeyHint         string `json:"maas_key_hint"`
		AccessKeyConfigured bool   `json:"access_key_configured"`
		AccessKeyHint       string `json:"access_key_hint"`
		SecretKeyConfigured bool   `json:"secret_key_configured"`
		SecretKeyHint       string `json:"secret_key_hint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Configured || status.KeyHint != "...-key" {
		t.Fatalf("status = %+v, want configured sandbox key hint", status)
	}
	if !status.MAASConfigured || status.MAASKeyHint != "...-key" {
		t.Fatalf("status = %+v, want configured maas key hint", status)
	}
	if !status.AccessKeyConfigured || status.AccessKeyHint != "...-key" {
		t.Fatalf("status = %+v, want configured access key hint", status)
	}
	if !status.SecretKeyConfigured || status.SecretKeyHint != "...-key" {
		t.Fatalf("status = %+v, want configured secret key hint", status)
	}

	credential, err := ctrl.service.QiniuCredential(req.Context(), user.AccountID)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	apiKey, err := ctrl.credentialBox.Decrypt(credential.EncryptedAPIKey)
	if err != nil {
		t.Fatalf("decrypt api key: %v", err)
	}
	if apiKey != "test-sandbox-api-key" {
		t.Fatalf("api key = %q, want plaintext after decrypt", apiKey)
	}
	maasAPIKey, err := ctrl.credentialBox.Decrypt(credential.EncryptedMAASAPIKey)
	if err != nil {
		t.Fatalf("decrypt maas api key: %v", err)
	}
	if maasAPIKey != "test-maas-api-key" {
		t.Fatalf("maas api key = %q, want plaintext after decrypt", maasAPIKey)
	}
	accessKey, err := ctrl.credentialBox.Decrypt(credential.EncryptedAccessKey)
	if err != nil {
		t.Fatalf("decrypt access key: %v", err)
	}
	if accessKey != "test-access-key" {
		t.Fatalf("access key = %q, want plaintext after decrypt", accessKey)
	}
	secretKey, err := ctrl.credentialBox.Decrypt(credential.EncryptedSecretKey)
	if err != nil {
		t.Fatalf("decrypt secret key: %v", err)
	}
	if secretKey != "test-secret-key" {
		t.Fatalf("secret key = %q, want plaintext after decrypt", secretKey)
	}
}

func TestSaveQiniuCredentialKeepsBlankExistingSecrets(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	router := newTestRouter(ctrl)
	initial := []byte(`{
		"sandbox_api_key": "old-sandbox-api-key",
		"maas_api_key": "old-maas-api-key",
		"access_key": "old-access-key",
		"secret_key": "old-secret-key"
	}`)
	saveCredentialForTest(t, router, ctrl, user.AccountID, initial)
	update := []byte(`{"maas_api_key": "new-maas-api-key"}`)
	rec := saveCredentialForTest(t, router, ctrl, user.AccountID, update)

	if strings.Contains(rec.Body.String(), "new-maas-api-key") {
		t.Fatalf("response should not contain plaintext maas key: %s", rec.Body.String())
	}
	credential, err := ctrl.service.QiniuCredential(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	assertDecryptedSecret(t, ctrl, credential.EncryptedAPIKey, "old-sandbox-api-key")
	assertDecryptedSecret(t, ctrl, credential.EncryptedMAASAPIKey, "new-maas-api-key")
	assertDecryptedSecret(t, ctrl, credential.EncryptedAccessKey, "old-access-key")
	assertDecryptedSecret(t, ctrl, credential.EncryptedSecretKey, "old-secret-key")
	if credential.KeyHint != "...-key" || credential.MAASKeyHint != "...-key" || credential.AccessKeyHint != "...-key" || credential.SecretKeyHint != "...-key" {
		t.Fatalf("credential hints = %+v, want preserved and updated hints", credential)
	}
}

func TestDeleteQiniuCredentialClearsStoredSecret(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	if _, err := ctrl.service.SaveQiniuCredential(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, credentialInputForTest()); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/qiniu/credentials", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	status, err := ctrl.service.QiniuCredentialStatus(req.Context(), user.AccountID)
	if err != nil {
		t.Fatalf("credential status: %v", err)
	}
	if status.Configured {
		t.Fatalf("status = %+v, want unconfigured", status)
	}
}

func saveCredentialForTest(t *testing.T, router http.Handler, ctrl *Ctrl, accountID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/qiniu/credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(accountID, time.Now()),
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	return rec
}

func assertDecryptedSecret(t *testing.T, ctrl *Ctrl, encrypted, want string) {
	t.Helper()
	got, err := ctrl.credentialBox.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt secret: %v", err)
	}
	if got != want {
		t.Fatalf("secret = %q, want %q", got, want)
	}
}

func credentialInputForTest() service.QiniuCredentialInput {
	return service.QiniuCredentialInput{
		KeyHint:             "...1234",
		EncryptedAPIKey:     "encrypted-api-key",
		MAASKeyHint:         "...maas",
		EncryptedMAASAPIKey: "encrypted-maas-api-key",
	}
}
