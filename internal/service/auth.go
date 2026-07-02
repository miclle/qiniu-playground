package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
)

const OAuthProviderGitHub = "github"

// OAuthUser is the normalized identity returned by an OAuth provider.
type OAuthUser struct {
	ProviderSubject string
	Login           string
	DisplayName     string
	AvatarURL       string
	Email           string
}

// AuthUser is the API-safe user view.
type AuthUser struct {
	AccountID string `json:"account_id"`
	Provider  string `json:"provider"`
	Subject   string `json:"subject"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Email     string `json:"email,omitempty"`
}

// UpsertGitHubIdentity creates or updates a GitHub identity and returns the
// joined application user.
func (s *Service) UpsertGitHubIdentity(ctx context.Context, user OAuthUser) (*AuthUser, error) {
	if user.ProviderSubject == "" {
		return nil, fmt.Errorf("provider subject is required")
	}
	if user.Login == "" {
		return nil, fmt.Errorf("login is required")
	}

	var out *AuthUser
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity entity.OAuthIdentity
		err := tx.Where("provider = ? AND provider_subject = ?", OAuthProviderGitHub, user.ProviderSubject).
			First(&identity).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			accountID, err := id.NewPrefixed("acct")
			if err != nil {
				return err
			}
			identityID, err := id.NewPrefixed("oid")
			if err != nil {
				return err
			}
			account := entity.Account{
				ID:        accountID,
				Name:      displayName(user),
				AvatarURL: user.AvatarURL,
			}
			if err := tx.Create(&account).Error; err != nil {
				return fmt.Errorf("create account: %w", err)
			}
			identity = entity.OAuthIdentity{
				ID:              identityID,
				AccountID:       accountID,
				Provider:        OAuthProviderGitHub,
				ProviderSubject: user.ProviderSubject,
			}
		case err != nil:
			return fmt.Errorf("find oauth identity: %w", err)
		}

		identity.Login = user.Login
		identity.DisplayName = user.DisplayName
		identity.AvatarURL = user.AvatarURL
		identity.Email = user.Email
		if err := tx.Save(&identity).Error; err != nil {
			return fmt.Errorf("save oauth identity: %w", err)
		}

		updates := map[string]any{
			"name":       displayName(user),
			"avatar_url": user.AvatarURL,
		}
		if err := tx.Model(&entity.Account{}).
			Where("id = ?", identity.AccountID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("update account: %w", err)
		}

		authUser, err := authUserByAccountID(tx, identity.AccountID)
		if err != nil {
			return err
		}
		out = authUser
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AuthUserByAccountID returns the API-safe user view for a local account.
func (s *Service) AuthUserByAccountID(ctx context.Context, accountID string) (*AuthUser, error) {
	return authUserByAccountID(s.db.WithContext(ctx), accountID)
}

// SaveGitHubAccessToken stores an encrypted GitHub OAuth access token for the account.
func (s *Service) SaveGitHubAccessToken(ctx context.Context, accountID, encryptedToken string) error {
	if accountID == "" {
		return fmt.Errorf("account id is required")
	}
	return s.db.WithContext(ctx).
		Model(&entity.OAuthIdentity{}).
		Where("account_id = ? AND provider = ?", accountID, OAuthProviderGitHub).
		Update("encrypted_token", encryptedToken).Error
}

// GitHubAccessToken returns the encrypted GitHub OAuth access token for the account.
func (s *Service) GitHubAccessToken(ctx context.Context, accountID string) (string, error) {
	var identity entity.OAuthIdentity
	err := s.db.WithContext(ctx).
		Select("encrypted_token").
		Where("account_id = ? AND provider = ?", accountID, OAuthProviderGitHub).
		First(&identity).Error
	if err != nil {
		return "", fmt.Errorf("find github access token: %w", err)
	}
	if identity.EncryptedToken == "" {
		return "", fmt.Errorf("github access token is not available")
	}
	return identity.EncryptedToken, nil
}

func authUserByAccountID(db *gorm.DB, accountID string) (*AuthUser, error) {
	var identity entity.OAuthIdentity
	err := db.Preload("Account").
		Where("account_id = ? AND provider = ?", accountID, OAuthProviderGitHub).
		First(&identity).Error
	if err != nil {
		return nil, fmt.Errorf("find auth user: %w", err)
	}
	return &AuthUser{
		AccountID: identity.AccountID,
		Provider:  identity.Provider,
		Subject:   identity.ProviderSubject,
		Login:     identity.Login,
		Name:      identity.Account.Name,
		AvatarURL: identity.AvatarURL,
		Email:     identity.Email,
	}, nil
}

func displayName(user OAuthUser) string {
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return user.Login
}
