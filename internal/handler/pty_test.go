package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestSandboxPTYBridgesWebSocketToRuntime(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-1",
		TemplateID: "base",
		State:      "running",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	server := httptest.NewServer(newTestRouter(ctrl))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/sandbox-1/pty"
	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	}).String())
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	_, initial, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial message: %v", err)
	}
	if string(initial) != "connected\n" {
		t.Fatalf("initial = %q, want connected", string(initial))
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("pwd\n")); err != nil {
		t.Fatalf("write message: %v", err)
	}
	_, echoed, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read echoed message: %v", err)
	}
	if string(echoed) != "echo:pwd\n" {
		t.Fatalf("echoed = %q, want echo", string(echoed))
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	if runtime.lastPTYInput != "pwd\n" {
		t.Fatalf("last input = %q, want pwd", runtime.lastPTYInput)
	}
}

func TestSandboxPTYRejectsCrossOriginWebSocket(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	if _, err := ctrl.service.SaveSandboxSession(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.SandboxSessionInput{
		SandboxID:  "sandbox-1",
		TemplateID: "base",
		State:      "running",
	}); err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	server := httptest.NewServer(newTestRouter(ctrl))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/sandbox-1/pty"
	header := http.Header{}
	header.Set("Origin", "https://attacker.example")
	header.Add("Cookie", (&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	}).String())

	conn, resp, err := websocket.DefaultDialer.Dial(url, header)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatalf("dial websocket succeeded, want cross-origin rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want 403", resp)
	}
}
