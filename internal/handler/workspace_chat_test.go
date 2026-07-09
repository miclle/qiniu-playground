package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miclle/qiniu-playground/internal/service"
)

func TestSendWorkspaceChatMessageRunsSandboxCLIAndPersistsMessages(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "VisionTube",
		RepoFullName:  "qiniu/vision-tube",
		Region:        "sandbox.example.com",
		SandboxID:     "sbox_456",
		TemplateID:    "base",
		State:         "running",
		WorkspacePath: "/workspace/qiniu__vision-tube",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.aiChatResult = &sandboxRuntimeAIChatResult{
		Provider: "codex",
		Stdout:   "Use `task check` to validate it.",
		ExitCode: 0,
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/chat/messages", bytes.NewReader([]byte(`{"message":"What should I run?"}`)))
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload workspaceChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.UserMessage.Role != service.WorkspaceChatRoleUser || payload.UserMessage.Content != "What should I run?" {
		t.Fatalf("user message = %+v, want saved user message", payload.UserMessage)
	}
	if payload.AssistantMessage.Role != service.WorkspaceChatRoleAssistant ||
		payload.AssistantMessage.Content != "Use `task check` to validate it." ||
		payload.AssistantMessage.Provider != "codex" {
		t.Fatalf("assistant message = %+v, want codex response", payload.AssistantMessage)
	}
	if runtime.lastAPIKey != "qiniu-api-key" || runtime.lastAIChatEndpoint != "sandbox.example.com" {
		t.Fatalf("runtime auth/endpoint = %q/%q, want qiniu key and workspace region", runtime.lastAPIKey, runtime.lastAIChatEndpoint)
	}
	if runtime.lastAIChatRequest.WorkspacePath != "/workspace/qiniu__vision-tube" {
		t.Fatalf("workspace path = %q, want workspace path", runtime.lastAIChatRequest.WorkspacePath)
	}
	if runtime.lastAIChatRequest.Envs["QINIU_MAAS_API_KEY"] != "qiniu-maas-key" ||
		runtime.lastAIChatRequest.Envs["ANTHROPIC_AUTH_TOKEN"] != "qiniu-maas-key" ||
		runtime.lastAIChatRequest.Envs["ANTHROPIC_BASE_URL"] != "https://api.qnaigc.com" ||
		runtime.lastAIChatRequest.Envs["OPENAI_API_KEY"] != "qiniu-maas-key" ||
		runtime.lastAIChatRequest.Envs["OPENAI_BASE_URL"] != "https://api.qnaigc.com/v1" {
		t.Fatalf("chat envs = %#v, want MAAS key injected", runtime.lastAIChatRequest.Envs)
	}
	if !strings.Contains(runtime.lastAIChatRequest.Prompt, "Workspace path: /workspace/qiniu__vision-tube") ||
		!strings.Contains(runtime.lastAIChatRequest.Prompt, "User: What should I run?") {
		t.Fatalf("prompt = %q, want workspace context and user message", runtime.lastAIChatRequest.Prompt)
	}
	messages, err := ctrl.service.ListWorkspaceChatMessages(req.Context(), user.AccountID, workspace.ID, 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(messages))
	}
}

func TestWorkspaceChatMessagesReturnsPersistedHistory(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:       "VisionTube",
		Region:     "sandbox.example.com",
		SandboxID:  "sbox_456",
		TemplateID: "base",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	if _, err := ctrl.service.SaveWorkspaceChatMessage(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceChatMessageInput{
		WorkspaceID: workspace.ID,
		SandboxID:   "sbox_456",
		Role:        service.WorkspaceChatRoleUser,
		Content:     "Hi",
	}); err != nil {
		t.Fatalf("save message: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+workspace.ID+"/chat/messages", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload workspaceChatMessagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].Content != "Hi" {
		t.Fatalf("messages = %+v, want persisted chat history", payload.Messages)
	}
}

func TestSendWorkspaceChatMessagePersistsCommandErrorWhenOutputIsEmpty(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:          "Foo",
		Region:        "sandbox.example.com",
		SandboxID:     "sbox_456",
		TemplateID:    "base",
		WorkspacePath: "/workspace/Foo",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.aiChatResult = &sandboxRuntimeAIChatResult{
		Error:    "start command: chdir /workspace/Foo: no such file or directory",
		ExitCode: -1,
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/chat/messages", bytes.NewReader([]byte(`{"message":"Hi"}`)))
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload workspaceChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(payload.AssistantMessage.Content, "chdir /workspace/Foo") {
		t.Fatalf("assistant message = %+v, want command error surfaced", payload.AssistantMessage)
	}
}

func TestSendWorkspaceChatMessageReturnsTransientRuntimeErrorResponse(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:       "Foo",
		Region:     "sandbox.example.com",
		SandboxID:  "sbox_456",
		TemplateID: "base",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	ctrl.sandboxRuntime.(*fakeSandboxRuntime).aiChatErr = errors.New("sandbox timed out")
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/chat/messages", bytes.NewReader([]byte(`{"message":"Hi"}`)))
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload workspaceChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.UserMessage.Role != service.WorkspaceChatRoleUser || payload.UserMessage.Content != "Hi" {
		t.Fatalf("user message = %+v, want saved user message", payload.UserMessage)
	}
	if payload.AssistantMessage.Role != service.WorkspaceChatRoleAssistant ||
		!strings.Contains(payload.AssistantMessage.Content, "sandbox timed out") ||
		payload.AssistantMessage.ExitCode != -1 {
		t.Fatalf("assistant message = %+v, want transient runtime error", payload.AssistantMessage)
	}
	if !strings.HasPrefix(payload.UserMessage.ID, "temp-") || !strings.HasPrefix(payload.AssistantMessage.ID, "temp-") {
		t.Fatalf("message ids = %q/%q, want transient ids", payload.UserMessage.ID, payload.AssistantMessage.ID)
	}
	if payload.UserMessage.ID == payload.AssistantMessage.ID {
		t.Fatalf("message ids = %q/%q, want unique transient ids", payload.UserMessage.ID, payload.AssistantMessage.ID)
	}
	messages, err := ctrl.service.ListWorkspaceChatMessages(req.Context(), user.AccountID, workspace.ID, 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("persisted messages = %+v, want runtime error excluded from history", messages)
	}
}

func TestSendWorkspaceChatMessageHandlesNilSandboxResult(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:       "Foo",
		Region:     "sandbox.example.com",
		SandboxID:  "sbox_456",
		TemplateID: "base",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	ctrl.sandboxRuntime.(*fakeSandboxRuntime).aiChatNilResult = true
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/chat/messages", bytes.NewReader([]byte(`{"message":"Hi"}`)))
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload workspaceChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.AssistantMessage.Content != "AI Chat did not receive a sandbox command result." {
		t.Fatalf("assistant message = %+v, want nil result fallback", payload.AssistantMessage)
	}
}

func TestSendWorkspaceChatMessageRequiresMAASKey(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedAPIKey(t, ctrl, user.AccountID, "qiniu-api-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:       "VisionTube",
		Region:     "sandbox.example.com",
		SandboxID:  "sbox_456",
		TemplateID: "base",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	router := newTestRouter(ctrl)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace.ID+"/chat/messages", bytes.NewReader([]byte(`{"message":"Hi"}`)))
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: ctrl.sessionSigner.Sign(user.AccountID, time.Now()),
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428; body = %s", rec.Code, rec.Body.String())
	}
}
