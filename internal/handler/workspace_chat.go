package handler

import (
	"net/http"
	"strconv"
	"strings"
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

type workspaceChatResponse struct {
	UserMessage      workspaceChatMessageResponse `json:"user_message"`
	AssistantMessage workspaceChatMessageResponse `json:"assistant_message"`
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
	prompt := workspaceChatPrompt(workspace, history, message)
	result, err := ctrl.sandboxRuntime.RunAIChat(c.Request.Context(), credentials.SandboxAPIKey, workspace.SandboxID, workspace.Region, sandboxRuntimeAIChatRequest{
		WorkspacePath: workspace.WorkspacePath,
		Prompt:        prompt,
		Envs:          qiniuRuntimeEnvs(credentials),
		Timeout:       3 * time.Minute,
	})
	if err != nil {
		logger.NewWithContext(c.Request.Context()).WithFields(map[string]any{
			"workspace_id": workspaceID,
			"sandbox_id":   workspace.SandboxID,
		}).Warnf("AI Chat runtime failed: %v", err)
		return workspaceChatResponse{
			UserMessage: workspaceChatTransientMessageResponse(service.WorkspaceChatRoleUser, message, 0),
			AssistantMessage: workspaceChatTransientMessageResponse(
				service.WorkspaceChatRoleAssistant,
				workspaceChatRuntimeErrorContent(err),
				-1,
			),
		}
	}
	content := workspaceChatResultContent(result)
	var provider string
	var exitCode int
	if result != nil {
		provider = result.Provider
		exitCode = result.ExitCode
	}
	userMessage, err := ctrl.service.SaveWorkspaceChatMessage(c.Request.Context(), accountID, service.WorkspaceChatMessageInput{
		WorkspaceID: workspaceID,
		SandboxID:   workspace.SandboxID,
		Role:        service.WorkspaceChatRoleUser,
		Content:     message,
	})
	if err != nil {
		return err
	}
	assistantMessage, err := ctrl.service.SaveWorkspaceChatMessage(c.Request.Context(), accountID, service.WorkspaceChatMessageInput{
		WorkspaceID: workspaceID,
		SandboxID:   workspace.SandboxID,
		Role:        service.WorkspaceChatRoleAssistant,
		Content:     content,
		Provider:    provider,
		ExitCode:    exitCode,
	})
	if err != nil {
		return err
	}
	return workspaceChatResponse{
		UserMessage:      workspaceChatMessageResponseFromEntity(*userMessage),
		AssistantMessage: workspaceChatMessageResponseFromEntity(*assistantMessage),
	}
}

func workspaceChatResultContent(result *sandboxRuntimeAIChatResult) string {
	if result == nil {
		return "AI Chat did not receive a sandbox command result."
	}
	parts := make([]string, 0, 3)
	for _, value := range []string{result.Stdout, result.Stderr, result.Error} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return "AI Chat command returned no output. Exit code: " + strconv.Itoa(result.ExitCode)
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
