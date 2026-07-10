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
	runtime.aiChatOutputChunks = []string{"Use `task ", "check` to validate it."}
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
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q, want SSE", contentType)
	}
	events := decodeWorkspaceChatSSEEvents(t, rec.Body.String())
	userMessage := events["user_message"].Message
	assistantMessage := events["assistant_message"].Message
	if userMessage.Role != service.WorkspaceChatRoleUser || userMessage.Content != "What should I run?" {
		t.Fatalf("user message = %+v, want saved user message", userMessage)
	}
	if assistantMessage.Role != service.WorkspaceChatRoleAssistant ||
		assistantMessage.Content != "Use `task check` to validate it." ||
		assistantMessage.Provider != "codex" {
		t.Fatalf("assistant message = %+v, want codex response without repetitive execution trace", assistantMessage)
	}
	deltas := decodeWorkspaceChatSSEEventList(t, rec.Body.String(), "assistant_delta")
	if len(deltas) != 2 || deltas[0].Delta != "Use `task " || deltas[1].Delta != "check` to validate it." {
		t.Fatalf("assistant deltas = %+v, want streaming output chunks", deltas)
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

func TestSendWorkspaceChatMessageFiltersThoughtBlocksFromStreamingDeltas(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:       "VisionTube",
		Region:     "sandbox.example.com",
		SandboxID:  "sbox_456",
		TemplateID: "base",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.aiChatResult = &sandboxRuntimeAIChatResult{
		Provider: "codex",
		Stdout:   "Visible answer.",
		ExitCode: 0,
	}
	runtime.aiChatOutputChunks = []string{"Vis", "ible <thi", "nk>hidden", " reasoning</think> answer."}
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
	deltas := decodeWorkspaceChatSSEEventList(t, rec.Body.String(), "assistant_delta")
	var streamed strings.Builder
	for _, delta := range deltas {
		streamed.WriteString(delta.Delta)
	}
	if got := streamed.String(); got != "Visible  answer." {
		t.Fatalf("streamed content = %q, want thought block filtered", got)
	}
	if strings.Contains(streamed.String(), "hidden reasoning") || strings.Contains(streamed.String(), "<think>") {
		t.Fatalf("streamed content = %q, want no thought content", streamed.String())
	}
}

func TestSendWorkspaceChatMessagePersistsExtractedThoughtWithoutSerializingIt(t *testing.T) {
	ctrl := newTestController(t)
	user := createAuthenticatedUser(t, ctrl)
	saveEncryptedQiniuKeys(t, ctrl, user.AccountID, "qiniu-api-key", "qiniu-maas-key")
	workspace, err := ctrl.service.SaveWorkspace(httptest.NewRequest(http.MethodGet, "/", nil).Context(), user.AccountID, service.WorkspaceInput{
		Name:       "VisionTube",
		Region:     "sandbox.example.com",
		SandboxID:  "sbox_456",
		TemplateID: "base",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	runtime := ctrl.sandboxRuntime.(*fakeSandboxRuntime)
	runtime.aiChatResult = &sandboxRuntimeAIChatResult{
		Provider: "codex",
		Stdout:   "<think>private reasoning</think>Visible answer.",
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
	assistantMessage := decodeWorkspaceChatSSEEvents(t, rec.Body.String())["assistant_message"].Message
	if assistantMessage.Content != "Visible answer." {
		t.Fatalf("assistant message = %+v, want visible content only", assistantMessage)
	}
	if strings.Contains(rec.Body.String(), "private reasoning") {
		t.Fatalf("response body leaks hidden thought: %s", rec.Body.String())
	}
	messages, err := ctrl.service.ListWorkspaceChatMessages(req.Context(), user.AccountID, workspace.ID, 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 || messages[1].Thought != "private reasoning" {
		t.Fatalf("persisted messages = %+v, want assistant thought saved server-side", messages)
	}
}

func TestSplitWorkspaceChatThoughtPreservesUTF8ByteIndexes(t *testing.T) {
	content, thought := splitWorkspaceChatThought("K<THINK>private reasoning</THINK>Visible answer.")

	if content != "KVisible answer." || thought != "private reasoning" {
		t.Fatalf("content/thought = %q/%q, want UTF-8 prefix preserved", content, thought)
	}
}

func TestSplitWorkspaceChatThoughtTreatsUnclosedBlockAsThought(t *testing.T) {
	content, thought := splitWorkspaceChatThought("Visible prefix <think>private reasoning")

	if content != "Visible prefix" || thought != "private reasoning" {
		t.Fatalf("content/thought = %q/%q, want unclosed thought hidden", content, thought)
	}
}

func TestSplitWorkspaceChatThoughtRemovesMultipleBlocks(t *testing.T) {
	content, thought := splitWorkspaceChatThought("Visible <think>first</think> answer <thought>second</thought>.")

	if content != "Visible  answer ." || thought != "first\n\nsecond" {
		t.Fatalf("content/thought = %q/%q, want all thought blocks hidden", content, thought)
	}
}

func TestWorkspaceChatThoughtDeltaFilterPreservesUTF8ByteIndexes(t *testing.T) {
	var got strings.Builder
	filter := newWorkspaceChatThoughtDeltaFilter(func(delta string) {
		got.WriteString(delta)
	})

	filter.Write("K<TH")
	filter.Write("INK>private reasoning</THINK>Visible answer.")
	filter.Flush()

	if got.String() != "KVisible answer." {
		t.Fatalf("streamed content = %q, want UTF-8 prefix preserved", got.String())
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
	assistantMessage := decodeWorkspaceChatSSEEvents(t, rec.Body.String())["assistant_message"].Message
	if !strings.Contains(assistantMessage.Content, "chdir /workspace/Foo") {
		t.Fatalf("assistant message = %+v, want command error surfaced", assistantMessage)
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
	events := decodeWorkspaceChatSSEEvents(t, rec.Body.String())
	userMessage := events["user_message"].Message
	assistantMessage := events["assistant_message"].Message
	if userMessage.Role != service.WorkspaceChatRoleUser || userMessage.Content != "Hi" {
		t.Fatalf("user message = %+v, want saved user message", userMessage)
	}
	if assistantMessage.Role != service.WorkspaceChatRoleAssistant ||
		!strings.Contains(assistantMessage.Content, "sandbox timed out") ||
		assistantMessage.ExitCode != -1 {
		t.Fatalf("assistant message = %+v, want transient runtime error", assistantMessage)
	}
	if strings.HasPrefix(userMessage.ID, "temp-") || !strings.HasPrefix(assistantMessage.ID, "temp-") {
		t.Fatalf("message ids = %q/%q, want persisted user and transient assistant ids", userMessage.ID, assistantMessage.ID)
	}
	if userMessage.ID == assistantMessage.ID {
		t.Fatalf("message ids = %q/%q, want unique transient ids", userMessage.ID, assistantMessage.ID)
	}
	messages, err := ctrl.service.ListWorkspaceChatMessages(req.Context(), user.AccountID, workspace.ID, 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != service.WorkspaceChatRoleUser || messages[0].Content != "Hi" {
		t.Fatalf("persisted messages = %+v, want user prompt kept and runtime error excluded from history", messages)
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
	assistantMessage := decodeWorkspaceChatSSEEvents(t, rec.Body.String())["assistant_message"].Message
	if assistantMessage.Content != "AI Chat did not receive a sandbox command result." {
		t.Fatalf("assistant message = %+v, want nil result fallback", assistantMessage)
	}
}

func decodeWorkspaceChatSSEEvents(t *testing.T, body string) map[string]workspaceChatSSEPayload {
	t.Helper()
	events := map[string]workspaceChatSSEPayload{}
	for _, event := range decodeWorkspaceChatSSEEventBlocks(t, body) {
		events[event.name] = event.payload
	}
	return events
}

func decodeWorkspaceChatSSEEventList(t *testing.T, body, name string) []workspaceChatSSEPayload {
	t.Helper()
	var payloads []workspaceChatSSEPayload
	for _, event := range decodeWorkspaceChatSSEEventBlocks(t, body) {
		if event.name == name {
			payloads = append(payloads, event.payload)
		}
	}
	return payloads
}

type workspaceChatSSEEventBlock struct {
	name    string
	payload workspaceChatSSEPayload
}

func decodeWorkspaceChatSSEEventBlocks(t *testing.T, body string) []workspaceChatSSEEventBlock {
	t.Helper()
	var events []workspaceChatSSEEventBlock
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var eventName string
		var data string
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				eventName = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		if eventName == "" || data == "" {
			continue
		}
		var payload workspaceChatSSEPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("decode %s event: %v", eventName, err)
		}
		events = append(events, workspaceChatSSEEventBlock{name: eventName, payload: payload})
	}
	return events
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
