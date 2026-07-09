package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
)

const (
	WorkspaceChatRoleUser      = "user"
	WorkspaceChatRoleAssistant = "assistant"
)

// WorkspaceChatMessageInput is the normalized chat message payload.
type WorkspaceChatMessageInput struct {
	WorkspaceID string
	SandboxID   string
	Role        string
	Content     string
	Provider    string
	ExitCode    int
}

// SaveWorkspaceChatMessage stores an AI Chat message for a workspace.
func (s *Service) SaveWorkspaceChatMessage(ctx context.Context, accountID string, input WorkspaceChatMessageInput) (*entity.WorkspaceChatMessage, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if strings.TrimSpace(input.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if input.Role != WorkspaceChatRoleUser && input.Role != WorkspaceChatRoleAssistant {
		return nil, fmt.Errorf("unsupported chat role: %s", input.Role)
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, fmt.Errorf("message content is required")
	}
	messageID, err := id.NewPrefixed("msg")
	if err != nil {
		return nil, err
	}
	message := entity.WorkspaceChatMessage{
		ID:          messageID,
		AccountID:   accountID,
		WorkspaceID: input.WorkspaceID,
		SandboxID:   input.SandboxID,
		Role:        input.Role,
		Content:     content,
		Provider:    input.Provider,
		ExitCode:    input.ExitCode,
	}
	if err := s.db.WithContext(ctx).Create(&message).Error; err != nil {
		return nil, fmt.Errorf("save workspace chat message: %w", err)
	}
	return &message, nil
}

// ListWorkspaceChatMessages returns chat history for an account-owned workspace.
func (s *Service) ListWorkspaceChatMessages(ctx context.Context, accountID, workspaceID string, limit int) ([]entity.WorkspaceChatMessage, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var messages []entity.WorkspaceChatMessage
	if err := s.db.WithContext(ctx).
		Where("account_id = ? AND workspace_id = ?", accountID, workspaceID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list workspace chat messages: %w", err)
	}
	slices.Reverse(messages)
	return messages, nil
}
