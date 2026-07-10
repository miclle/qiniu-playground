package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"github.com/fox-gonic/fox/logger"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

const workspaceChatHistoryLimit = 40

type workspaceChatMessagesResponse struct {
	Messages []workspaceChatMessageResponse `json:"messages"`
}

type workspaceChatMessageResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Provider  string `json:"provider,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
}

type workspaceChatRequest struct {
	Message string `json:"message"`
}

type workspaceChatSSEPayload struct {
	Message workspaceChatMessageResponse `json:"message,omitempty"`
	Delta   string                       `json:"delta,omitempty"`
	Error   string                       `json:"error,omitempty"`
	Status  string                       `json:"status,omitempty"`
}

func (ctrl *Ctrl) WorkspaceChatMessages(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	workspaceID := c.Param("workspaceID")
	if _, err := ctrl.service.Workspace(c.Request.Context(), accountID, workspaceID); err != nil {
		return httperrors.New(http.StatusNotFound, "workspace not found")
	}
	messages, err := ctrl.service.ListWorkspaceChatMessages(c.Request.Context(), accountID, workspaceID, 100)
	if err != nil {
		return err
	}
	return workspaceChatMessagesResponse{Messages: workspaceChatMessageResponses(messages)}
}

func (ctrl *Ctrl) SendWorkspaceChatMessage(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	var req workspaceChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return httperrors.New(http.StatusBadRequest, "invalid request body")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return httperrors.New(http.StatusBadRequest, "message is required")
	}
	workspaceID := c.Param("workspaceID")
	workspace, err := ctrl.service.Workspace(c.Request.Context(), accountID, workspaceID)
	if err != nil {
		return httperrors.New(http.StatusNotFound, "workspace not found")
	}
	if workspace.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "workspace sandbox is not ready")
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	if credentials.MAASAPIKey == "" {
		return httperrors.New(http.StatusPreconditionRequired, "Qiniu MAAS API Key is not configured")
	}
	history, err := ctrl.service.ListWorkspaceChatMessages(c.Request.Context(), accountID, workspaceID, workspaceChatHistoryLimit)
	if err != nil {
		return err
	}
	stream := newWorkspaceChatSSEStream(c.Writer)
	userMessage, err := ctrl.service.SaveWorkspaceChatMessage(c.Request.Context(), accountID, service.WorkspaceChatMessageInput{
		WorkspaceID: workspaceID,
		SandboxID:   workspace.SandboxID,
		Role:        service.WorkspaceChatRoleUser,
		Content:     message,
	})
	if err != nil {
		logger.NewWithContext(c.Request.Context()).Errorf("failed to save user chat message: %v", err)
		_ = stream.Send("error", workspaceChatSSEPayload{Error: "Failed to save chat message."})
		_ = stream.Send("done", workspaceChatSSEPayload{})
		return nil
	}
	if err := stream.Send("user_message", workspaceChatSSEPayload{Message: workspaceChatMessageResponseFromEntity(*userMessage)}); err != nil {
		return nil
	}
	if err := stream.Send("status", workspaceChatSSEPayload{Status: "Running AI Chat in the sandbox..."}); err != nil {
		return nil
	}
	prompt := workspaceChatPrompt(workspace, history, message)
	deltaFilter := newWorkspaceChatThoughtDeltaFilter(func(delta string) {
		_ = stream.Send("assistant_delta", workspaceChatSSEPayload{Delta: delta})
	})
	result, err := ctrl.sandboxRuntime.RunAIChat(c.Request.Context(), credentials.SandboxAPIKey, workspace.SandboxID, workspace.Region, sandboxRuntimeAIChatRequest{
		WorkspacePath: workspace.WorkspacePath,
		Prompt:        prompt,
		Envs:          qiniuRuntimeEnvs(credentials),
		Timeout:       3 * time.Minute,
		OnOutput:      deltaFilter.Write,
	})
	deltaFilter.Flush()
	if err != nil {
		if c.Request.Context().Err() != nil {
			return nil
		}
		logger.NewWithContext(c.Request.Context()).WithFields(map[string]any{
			"workspace_id": workspaceID,
			"sandbox_id":   workspace.SandboxID,
		}).Warnf("AI Chat runtime failed: %v", err)
		assistantMessage := workspaceChatTransientMessageResponse(
			service.WorkspaceChatRoleAssistant,
			workspaceChatRuntimeErrorContent(err),
			-1,
		)
		_ = stream.Send("assistant_message", workspaceChatSSEPayload{Message: assistantMessage})
		_ = stream.Send("done", workspaceChatSSEPayload{})
		return nil
	}
	content, thought := workspaceChatResultContent(result)
	var provider string
	var exitCode int
	if result != nil {
		provider = result.Provider
		exitCode = result.ExitCode
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
	defer cancel()
	assistantMessage, err := ctrl.service.SaveWorkspaceChatMessage(saveCtx, accountID, service.WorkspaceChatMessageInput{
		WorkspaceID: workspaceID,
		SandboxID:   workspace.SandboxID,
		Role:        service.WorkspaceChatRoleAssistant,
		Content:     content,
		Thought:     thought,
		Provider:    provider,
		ExitCode:    exitCode,
	})
	if err != nil {
		logger.NewWithContext(c.Request.Context()).Errorf("failed to save assistant chat message: %v", err)
		_ = stream.Send("error", workspaceChatSSEPayload{Error: "Failed to save assistant response."})
		_ = stream.Send("done", workspaceChatSSEPayload{})
		return nil
	}
	_ = stream.Send("assistant_message", workspaceChatSSEPayload{Message: workspaceChatMessageResponseFromEntity(*assistantMessage)})
	_ = stream.Send("done", workspaceChatSSEPayload{})
	return nil
}

func workspaceChatResultContent(result *sandboxRuntimeAIChatResult) (string, string) {
	if result == nil {
		return "AI Chat did not receive a sandbox command result.", ""
	}
	parts := make([]string, 0, 3)
	for _, value := range []string{result.Stdout, result.Stderr, result.Error} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	content := ""
	if len(parts) > 0 {
		content = strings.Join(parts, "\n")
	} else {
		content = "AI Chat command returned no output. Exit code: " + strconv.Itoa(result.ExitCode)
	}
	content, thought := splitWorkspaceChatThought(content)
	if result.Thought != "" {
		thought = strings.TrimSpace(strings.Join([]string{thought, result.Thought}, "\n\n"))
	}
	if strings.TrimSpace(content) == "" {
		content = "AI Chat command returned no answer. Exit code: " + strconv.Itoa(result.ExitCode)
	}
	return content, thought
}

func workspaceChatRuntimeErrorContent(err error) string {
	if err == nil {
		return "AI Chat failed before the sandbox command completed."
	}
	return "AI Chat failed before the sandbox command completed: " + err.Error()
}

func workspaceChatTransientMessageResponse(role, content string, exitCode int) workspaceChatMessageResponse {
	return workspaceChatMessageResponse{
		ID:        "temp-" + role + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		CreatedAt: time.Now().Format(time.RFC3339),
		Role:      role,
		Content:   content,
		ExitCode:  exitCode,
	}
}

func workspaceChatMessageResponses(messages []entity.WorkspaceChatMessage) []workspaceChatMessageResponse {
	out := make([]workspaceChatMessageResponse, 0, len(messages))
	for _, message := range messages {
		out = append(out, workspaceChatMessageResponseFromEntity(message))
	}
	return out
}

func workspaceChatMessageResponseFromEntity(message entity.WorkspaceChatMessage) workspaceChatMessageResponse {
	return workspaceChatMessageResponse{
		ID:        message.ID,
		CreatedAt: message.CreatedAt.Format(time.RFC3339),
		Role:      message.Role,
		Content:   message.Content,
		Provider:  message.Provider,
		ExitCode:  message.ExitCode,
	}
}

type workspaceChatSSEStream struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func newWorkspaceChatSSEStream(writer http.ResponseWriter) *workspaceChatSSEStream {
	header := writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	flusher, _ := writer.(http.Flusher)
	writer.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}
	return &workspaceChatSSEStream{writer: writer, flusher: flusher}
}

func (s *workspaceChatSSEStream) Send(event string, payload workspaceChatSSEPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

type workspaceChatThoughtDeltaFilter struct {
	mu        sync.Mutex
	onDelta   func(string)
	buffer    string
	inThought bool
	closeTag  string
	openTags  []string
	closeTags []string
}

func newWorkspaceChatThoughtDeltaFilter(onDelta func(string)) *workspaceChatThoughtDeltaFilter {
	openTags := []string{"<think>", "<thinking>", "<thought>"}
	closeTags := []string{"</think>", "</thinking>", "</thought>"}
	return &workspaceChatThoughtDeltaFilter{
		onDelta:   onDelta,
		openTags:  openTags,
		closeTags: closeTags,
	}
}

func (f *workspaceChatThoughtDeltaFilter) Write(chunk string) {
	if f.onDelta == nil || chunk == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeLocked(chunk)
}

func (f *workspaceChatThoughtDeltaFilter) writeLocked(chunk string) {
	f.buffer += chunk
	for {
		if f.inThought {
			index := indexASCIIFold(f.buffer, f.closeTag)
			if index < 0 {
				f.buffer = f.closeTagPrefixSuffix(f.buffer)
				return
			}
			f.buffer = f.buffer[index+len(f.closeTag):]
			f.inThought = false
			f.closeTag = ""
			continue
		}
		index, openTag := f.nextOpenTag(f.buffer)
		if index >= 0 {
			f.emit(f.buffer[:index])
			f.buffer = f.buffer[index+len(openTag):]
			f.inThought = true
			f.closeTag = "</" + strings.Trim(openTag, "<>") + ">"
			continue
		}
		emit, keep := f.visiblePrefix(f.buffer)
		f.emit(emit)
		f.buffer = keep
		return
	}
}

func (f *workspaceChatThoughtDeltaFilter) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inThought {
		f.buffer = ""
		return
	}
	f.emit(f.buffer)
	f.buffer = ""
}

func (f *workspaceChatThoughtDeltaFilter) emit(delta string) {
	if delta != "" {
		f.onDelta(delta)
	}
}

func (f *workspaceChatThoughtDeltaFilter) nextOpenTag(content string) (int, string) {
	bestIndex := -1
	bestTag := ""
	for _, tag := range f.openTags {
		index := indexASCIIFold(content, tag)
		if index >= 0 && (bestIndex == -1 || index < bestIndex) {
			bestIndex = index
			bestTag = tag
		}
	}
	return bestIndex, bestTag
}

func (f *workspaceChatThoughtDeltaFilter) visiblePrefix(content string) (string, string) {
	lastOpen := strings.LastIndex(content, "<")
	if lastOpen < 0 {
		return content, ""
	}
	suffix := content[lastOpen:]
	for _, tag := range f.openTags {
		if hasPrefixASCIIFold(tag, suffix) {
			return content[:lastOpen], content[lastOpen:]
		}
	}
	return content, ""
}

func (f *workspaceChatThoughtDeltaFilter) closeTagPrefixSuffix(content string) string {
	for length := min(len(content), maxWorkspaceChatThoughtCloseTagLength); length > 0; length-- {
		suffix := content[len(content)-length:]
		for _, tag := range f.closeTags {
			if hasPrefixASCIIFold(tag, suffix) {
				return content[len(content)-length:]
			}
		}
	}
	return ""
}

const maxWorkspaceChatThoughtCloseTagLength = len("</thinking>") - 1

func splitWorkspaceChatThought(content string) (string, string) {
	trimmed := strings.TrimSpace(content)
	var thoughts []string
	for {
		start := -1
		open := ""
		close := ""
		for _, tag := range []string{"think", "thinking", "thought"} {
			candidateOpen := "<" + tag + ">"
			candidateStart := indexASCIIFold(trimmed, candidateOpen)
			if candidateStart >= 0 && (start == -1 || candidateStart < start) {
				start = candidateStart
				open = candidateOpen
				close = "</" + tag + ">"
			}
		}
		if start < 0 {
			return strings.TrimSpace(trimmed), strings.TrimSpace(strings.Join(thoughts, "\n\n"))
		}
		thoughtStart := start + len(open)
		relativeEnd := indexASCIIFold(trimmed[thoughtStart:], close)
		if relativeEnd < 0 {
			thoughts = append(thoughts, strings.TrimSpace(trimmed[thoughtStart:]))
			trimmed = strings.TrimSpace(trimmed[:start])
			return trimmed, strings.TrimSpace(strings.Join(thoughts, "\n\n"))
		}
		end := thoughtStart + relativeEnd
		thoughts = append(thoughts, strings.TrimSpace(trimmed[thoughtStart:end]))
		trimmed = strings.TrimSpace(trimmed[:start] + trimmed[end+len(close):])
	}
}

func indexASCIIFold(content, pattern string) int {
	if pattern == "" {
		return 0
	}
	if len(content) < len(pattern) {
		return -1
	}
	for index := 0; index <= len(content)-len(pattern); index++ {
		if hasPrefixASCIIFold(content[index:], pattern) {
			return index
		}
	}
	return -1
}

func hasPrefixASCIIFold(content, prefix string) bool {
	if len(content) < len(prefix) {
		return false
	}
	for index := 0; index < len(prefix); index++ {
		if asciiFold(content[index]) != asciiFold(prefix[index]) {
			return false
		}
	}
	return true
}

func asciiFold(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value - 'A' + 'a'
	}
	return value
}

func workspaceChatPrompt(workspace *entity.Workspace, history []entity.WorkspaceChatMessage, message string) string {
	var builder strings.Builder
	builder.WriteString("You are running inside a Qiniu Playground sandbox for this workspace.\n")
	if workspace != nil && workspace.WorkspacePath != "" {
		builder.WriteString("Workspace path: " + workspace.WorkspacePath + "\n")
	}
	if workspace != nil && workspace.RepoFullName != "" {
		builder.WriteString("Repository: " + workspace.RepoFullName + "\n")
	}
	builder.WriteString("Use the local repository and shell tools when needed. Keep the response concise and actionable.\n\n")
	if len(history) > 0 {
		builder.WriteString("Recent conversation:\n")
		for _, item := range history {
			builder.WriteString(item.Role)
			builder.WriteString(": ")
			builder.WriteString(item.Content)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("User: ")
	builder.WriteString(message)
	return builder.String()
}
