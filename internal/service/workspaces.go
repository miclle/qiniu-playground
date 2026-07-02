package service

import (
	"context"
	"fmt"

	"gorm.io/gorm/clause"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
)

// WorkspaceInput is the normalized repository workspace payload.
type WorkspaceInput struct {
	Name          string
	GitHubRepoID  *int64
	RepoFullName  string
	Region        string
	SandboxID     string
	TemplateID    string
	State         string
	Endpoint      string
	WorkspacePath string
	IDEURL        string
}

// SaveWorkspace stores or updates a configured repository workspace.
func (s *Service) SaveWorkspace(ctx context.Context, accountID string, input WorkspaceInput) (*entity.Workspace, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if input.GitHubRepoID != nil && input.RepoFullName == "" {
		return nil, fmt.Errorf("repo full name is required")
	}
	if input.Region == "" {
		return nil, fmt.Errorf("region is required")
	}
	if input.TemplateID == "" {
		return nil, fmt.Errorf("template id is required")
	}
	workspaceID, err := id.NewPrefixed("wks")
	if err != nil {
		return nil, err
	}
	workspace := entity.Workspace{
		ID:            workspaceID,
		AccountID:     accountID,
		Name:          input.Name,
		GitHubRepoID:  input.GitHubRepoID,
		RepoFullName:  input.RepoFullName,
		Region:        input.Region,
		SandboxID:     input.SandboxID,
		TemplateID:    input.TemplateID,
		State:         input.State,
		Endpoint:      input.Endpoint,
		WorkspacePath: input.WorkspacePath,
		IDEURL:        input.IDEURL,
	}
	if input.GitHubRepoID == nil {
		if err := s.db.WithContext(ctx).Create(&workspace).Error; err != nil {
			return nil, fmt.Errorf("save workspace: %w", err)
		}
		return &workspace, nil
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "account_id"},
			{Name: "github_repo_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"name",
			"github_repo_id",
			"repo_full_name",
			"region",
			"sandbox_id",
			"template_id",
			"state",
			"endpoint",
			"workspace_path",
			"ide_url",
			"updated_at",
			"deleted_at",
		}),
	}).Create(&workspace).Error; err != nil {
		return nil, fmt.Errorf("save workspace: %w", err)
	}
	return s.WorkspaceByGitHubRepoID(ctx, accountID, *input.GitHubRepoID)
}

// WorkspaceByGitHubRepoID returns a workspace for an account GitHub repository id.
func (s *Service) WorkspaceByGitHubRepoID(ctx context.Context, accountID string, githubRepoID int64) (*entity.Workspace, error) {
	var workspace entity.Workspace
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND github_repo_id = ?", accountID, githubRepoID).
		First(&workspace).Error
	if err != nil {
		return nil, fmt.Errorf("find workspace: %w", err)
	}
	return &workspace, nil
}

// WorkspaceExistsByGitHubRepoID reports whether a workspace already exists for an account GitHub repository id.
func (s *Service) WorkspaceExistsByGitHubRepoID(ctx context.Context, accountID string, githubRepoID int64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&entity.Workspace{}).
		Where("account_id = ? AND github_repo_id = ?", accountID, githubRepoID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("count workspace: %w", err)
	}
	return count > 0, nil
}

// ListWorkspaces returns configured workspaces for an account.
func (s *Service) ListWorkspaces(ctx context.Context, accountID string) ([]entity.Workspace, error) {
	var workspaces []entity.Workspace
	err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("updated_at desc").
		Find(&workspaces).Error
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}
