package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
)

// GitHubInstallationInput is the normalized installation payload stored locally.
type GitHubInstallationInput struct {
	InstallationID      int64
	TargetType          string
	TargetLogin         string
	RepositorySelection string
}

// GitHubRepositoryInput is the normalized repository payload returned by GitHub.
type GitHubRepositoryInput struct {
	GitHubRepoID  int64
	Owner         string
	Name          string
	FullName      string
	Private       bool
	DefaultBranch string
	HTMLURL       string
}

// SaveGitHubInstallation records or updates an installation for an account.
func (s *Service) SaveGitHubInstallation(ctx context.Context, accountID string, input GitHubInstallationInput) (*entity.GitHubInstallation, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if input.InstallationID == 0 {
		return nil, fmt.Errorf("installation id is required")
	}

	installationID, err := id.NewPrefixed("ghi")
	if err != nil {
		return nil, err
	}
	installation := entity.GitHubInstallation{
		ID:                  installationID,
		AccountID:           accountID,
		InstallationID:      input.InstallationID,
		TargetType:          input.TargetType,
		TargetLogin:         input.TargetLogin,
		RepositorySelection: input.RepositorySelection,
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "account_id"},
			{Name: "installation_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"target_type",
			"target_login",
			"repository_selection",
			"updated_at",
		}),
	}).Create(&installation).Error
	if err != nil {
		return nil, fmt.Errorf("save github installation: %w", err)
	}
	return s.GitHubInstallation(ctx, accountID, input.InstallationID)
}

// GitHubInstallation returns a single installation owned by the account.
func (s *Service) GitHubInstallation(ctx context.Context, accountID string, installationID int64) (*entity.GitHubInstallation, error) {
	var installation entity.GitHubInstallation
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND installation_id = ?", accountID, installationID).
		First(&installation).Error
	if err != nil {
		return nil, fmt.Errorf("find github installation: %w", err)
	}
	return &installation, nil
}

// ListGitHubInstallations returns installations connected by the account.
func (s *Service) ListGitHubInstallations(ctx context.Context, accountID string) ([]entity.GitHubInstallation, error) {
	var installations []entity.GitHubInstallation
	err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("updated_at desc").
		Find(&installations).Error
	if err != nil {
		return nil, fmt.Errorf("list github installations: %w", err)
	}
	return installations, nil
}

// SaveGitHubRepositories upserts the latest repository snapshot for an installation.
func (s *Service) SaveGitHubRepositories(ctx context.Context, accountID string, installationID int64, inputs []GitHubRepositoryInput) ([]entity.GitHubRepository, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if installationID == 0 {
		return nil, fmt.Errorf("installation id is required")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		activeRepoIDs := make([]int64, 0, len(inputs))
		for _, input := range inputs {
			activeRepoIDs = append(activeRepoIDs, input.GitHubRepoID)
			repoID, err := id.NewPrefixed("ghr")
			if err != nil {
				return err
			}
			repo := entity.GitHubRepository{
				ID:             repoID,
				AccountID:      accountID,
				InstallationID: installationID,
				GitHubRepoID:   input.GitHubRepoID,
				Owner:          input.Owner,
				Name:           input.Name,
				FullName:       input.FullName,
				Private:        input.Private,
				DefaultBranch:  input.DefaultBranch,
				HTMLURL:        input.HTMLURL,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "account_id"},
					{Name: "installation_id"},
					{Name: "github_repo_id"},
				},
				DoUpdates: clause.AssignmentColumns([]string{
					"owner",
					"name",
					"full_name",
					"private",
					"default_branch",
					"html_url",
					"updated_at",
					"deleted_at",
				}),
			}).Create(&repo).Error; err != nil {
				return fmt.Errorf("save github repository: %w", err)
			}
		}
		stale := tx.Where("account_id = ? AND installation_id = ?", accountID, installationID)
		if len(activeRepoIDs) > 0 {
			stale = stale.Where("github_repo_id NOT IN ?", activeRepoIDs)
		}
		if err := stale.Delete(&entity.GitHubRepository{}).Error; err != nil {
			return fmt.Errorf("delete stale github repositories: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ListGitHubRepositories(ctx, accountID)
}

// ListGitHubRepositories returns locally cached repositories for the account.
func (s *Service) ListGitHubRepositories(ctx context.Context, accountID string) ([]entity.GitHubRepository, error) {
	var repos []entity.GitHubRepository
	err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("full_name asc").
		Find(&repos).Error
	if err != nil {
		return nil, fmt.Errorf("list github repositories: %w", err)
	}
	return repos, nil
}

// GitHubRepository returns a locally cached repository owned by the account.
func (s *Service) GitHubRepository(ctx context.Context, accountID, repositoryID string) (*entity.GitHubRepository, error) {
	var repo entity.GitHubRepository
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND id = ?", accountID, repositoryID).
		First(&repo).Error
	if err != nil {
		return nil, fmt.Errorf("find github repository: %w", err)
	}
	return &repo, nil
}
