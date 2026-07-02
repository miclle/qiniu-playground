package service

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
)

// SandboxSessionInput is the normalized sandbox session payload.
type SandboxSessionInput struct {
	SandboxID     string
	TemplateID    string
	State         string
	Endpoint      string
	GitHubRepoID  *int64
	RepoFullName  string
	WorkspacePath string
	Region        string
	CPUCount      int32
	MemoryGB      int32
	IDEURL        string
}

// SaveSandboxSession stores or updates a sandbox session for an account.
func (s *Service) SaveSandboxSession(ctx context.Context, accountID string, input SandboxSessionInput) (*entity.SandboxSession, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if input.SandboxID == "" {
		return nil, fmt.Errorf("sandbox id is required")
	}
	if input.TemplateID == "" {
		return nil, fmt.Errorf("template id is required")
	}
	if input.State == "" {
		input.State = "running"
	}
	now := time.Now()
	sessionID, err := id.NewPrefixed("sbx")
	if err != nil {
		return nil, err
	}
	session := entity.SandboxSession{
		ID:              sessionID,
		AccountID:       accountID,
		SandboxID:       input.SandboxID,
		TemplateID:      input.TemplateID,
		State:           input.State,
		Endpoint:        input.Endpoint,
		GitHubRepoID:    input.GitHubRepoID,
		RepoFullName:    input.RepoFullName,
		WorkspacePath:   input.WorkspacePath,
		Region:          input.Region,
		CPUCount:        input.CPUCount,
		MemoryGB:        input.MemoryGB,
		IDEURL:          input.IDEURL,
		LastConnectedAt: &now,
	}
	updateAssignments := map[string]any{
		"template_id":       session.TemplateID,
		"state":             session.State,
		"endpoint":          session.Endpoint,
		"last_connected_at": session.LastConnectedAt,
		"updated_at":        now,
	}
	if input.GitHubRepoID != nil {
		updateAssignments["github_repo_id"] = input.GitHubRepoID
	}
	if input.RepoFullName != "" {
		updateAssignments["repo_full_name"] = input.RepoFullName
	}
	if input.WorkspacePath != "" {
		updateAssignments["workspace_path"] = input.WorkspacePath
	}
	if input.Region != "" {
		updateAssignments["region"] = input.Region
	}
	if input.CPUCount > 0 {
		updateAssignments["cpu_count"] = input.CPUCount
	}
	if input.MemoryGB > 0 {
		updateAssignments["memory_gb"] = input.MemoryGB
	}
	if input.IDEURL != "" {
		updateAssignments["ide_url"] = input.IDEURL
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "account_id"},
			{Name: "sandbox_id"},
		},
		DoUpdates: clause.Assignments(updateAssignments),
	}).Create(&session).Error
	if err != nil {
		return nil, fmt.Errorf("save sandbox session: %w", err)
	}
	return s.SandboxSession(ctx, accountID, input.SandboxID)
}

// SandboxSession returns a sandbox session owned by an account.
func (s *Service) SandboxSession(ctx context.Context, accountID, sandboxID string) (*entity.SandboxSession, error) {
	var session entity.SandboxSession
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND sandbox_id = ?", accountID, sandboxID).
		First(&session).Error
	if err != nil {
		return nil, fmt.Errorf("find sandbox session: %w", err)
	}
	return &session, nil
}

// ListSandboxSessions returns sandbox sessions owned by an account.
func (s *Service) ListSandboxSessions(ctx context.Context, accountID string) ([]entity.SandboxSession, error) {
	var sessions []entity.SandboxSession
	err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("updated_at desc").
		Find(&sessions).Error
	if err != nil {
		return nil, fmt.Errorf("list sandbox sessions: %w", err)
	}
	return sessions, nil
}
