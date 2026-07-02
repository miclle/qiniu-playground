package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
)

// QiniuCredentialInput contains already-encrypted credential values.
type QiniuCredentialInput struct {
	KeyHint             string
	EncryptedAPIKey     string
	MAASKeyHint         string
	EncryptedMAASAPIKey string
	AccessKeyHint       string
	EncryptedAccessKey  string
	SecretKeyHint       string
	EncryptedSecretKey  string
}

// QiniuCredentialStatus is the API-safe credential view.
type QiniuCredentialStatus struct {
	Configured          bool   `json:"configured"`
	KeyHint             string `json:"key_hint,omitempty"`
	MAASConfigured      bool   `json:"maas_configured"`
	MAASKeyHint         string `json:"maas_key_hint,omitempty"`
	AccessKeyConfigured bool   `json:"access_key_configured"`
	AccessKeyHint       string `json:"access_key_hint,omitempty"`
	SecretKeyConfigured bool   `json:"secret_key_configured"`
	SecretKeyHint       string `json:"secret_key_hint,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

// SaveQiniuCredential stores encrypted Qiniu credentials for an account.
func (s *Service) SaveQiniuCredential(ctx context.Context, accountID string, input QiniuCredentialInput) (*entity.QiniuCredential, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if input.EncryptedAPIKey == "" {
		return nil, fmt.Errorf("encrypted qiniu api key is required")
	}
	credentialID, err := id.NewPrefixed("qnc")
	if err != nil {
		return nil, err
	}
	credential := entity.QiniuCredential{
		ID:                  credentialID,
		AccountID:           accountID,
		KeyHint:             input.KeyHint,
		EncryptedAPIKey:     input.EncryptedAPIKey,
		MAASKeyHint:         input.MAASKeyHint,
		EncryptedMAASAPIKey: input.EncryptedMAASAPIKey,
		AccessKeyHint:       input.AccessKeyHint,
		EncryptedAccessKey:  input.EncryptedAccessKey,
		SecretKeyHint:       input.SecretKeyHint,
		EncryptedSecretKey:  input.EncryptedSecretKey,
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"key_hint",
			"encrypted_api_key",
			"maas_key_hint",
			"encrypted_maas_api_key",
			"access_key_hint",
			"encrypted_access_key",
			"secret_key_hint",
			"encrypted_secret_key",
			"updated_at",
			"deleted_at",
		}),
	}).Create(&credential).Error
	if err != nil {
		return nil, fmt.Errorf("save qiniu credential: %w", err)
	}
	return s.QiniuCredential(ctx, accountID)
}

// QiniuCredential returns encrypted credentials for internal sandbox calls.
func (s *Service) QiniuCredential(ctx context.Context, accountID string) (*entity.QiniuCredential, error) {
	var credential entity.QiniuCredential
	err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		First(&credential).Error
	if err != nil {
		return nil, fmt.Errorf("find qiniu credential: %w", err)
	}
	return &credential, nil
}

// QiniuCredentialStatus returns an API-safe status object.
func (s *Service) QiniuCredentialStatus(ctx context.Context, accountID string) (*QiniuCredentialStatus, error) {
	credential, err := s.QiniuCredential(ctx, accountID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &QiniuCredentialStatus{}, nil
		}
		return nil, err
	}
	return &QiniuCredentialStatus{
		Configured:          true,
		KeyHint:             credential.KeyHint,
		MAASConfigured:      credential.EncryptedMAASAPIKey != "",
		MAASKeyHint:         credential.MAASKeyHint,
		AccessKeyConfigured: credential.EncryptedAccessKey != "",
		AccessKeyHint:       credential.AccessKeyHint,
		SecretKeyConfigured: credential.EncryptedSecretKey != "",
		SecretKeyHint:       credential.SecretKeyHint,
		UpdatedAt:           credential.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// DeleteQiniuCredential removes stored Qiniu credentials for an account.
func (s *Service) DeleteQiniuCredential(ctx context.Context, accountID string) error {
	if accountID == "" {
		return fmt.Errorf("account id is required")
	}
	return s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Delete(&entity.QiniuCredential{}).Error
}
