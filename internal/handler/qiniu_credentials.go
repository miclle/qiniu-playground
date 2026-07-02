package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

type saveQiniuCredentialRequest struct {
	APIKey        string `json:"api_key"`
	SandboxAPIKey string `json:"sandbox_api_key"`
	MAASAPIKey    string `json:"maas_api_key"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
}

func (ctrl *Ctrl) QiniuCredentialStatus(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	status, err := ctrl.service.QiniuCredentialStatus(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	return status
}

func (ctrl *Ctrl) SaveQiniuCredential(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	var req saveQiniuCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return httperrors.New(http.StatusBadRequest, "invalid request body")
	}
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.SandboxAPIKey = strings.TrimSpace(req.SandboxAPIKey)
	req.MAASAPIKey = strings.TrimSpace(req.MAASAPIKey)
	req.AccessKey = strings.TrimSpace(req.AccessKey)
	req.SecretKey = strings.TrimSpace(req.SecretKey)
	if req.SandboxAPIKey == "" {
		req.SandboxAPIKey = req.APIKey
	}
	existing, err := ctrl.service.QiniuCredential(c.Request.Context(), accountID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		existing = &entity.QiniuCredential{}
	}
	if req.SandboxAPIKey == "" && existing.EncryptedAPIKey == "" {
		return httperrors.New(http.StatusBadRequest, "sandbox_api_key is required")
	}
	sandboxKeyHint, encryptedAPIKey, err := ctrl.encryptOrKeepSecret(req.SandboxAPIKey, existing.KeyHint, existing.EncryptedAPIKey)
	if err != nil {
		return err
	}
	maasKeyHint, encryptedMAASAPIKey, err := ctrl.encryptOrKeepSecret(req.MAASAPIKey, existing.MAASKeyHint, existing.EncryptedMAASAPIKey)
	if err != nil {
		return err
	}
	accessKeyHint, encryptedAccessKey, err := ctrl.encryptOrKeepSecret(req.AccessKey, existing.AccessKeyHint, existing.EncryptedAccessKey)
	if err != nil {
		return err
	}
	secretKeyHint, encryptedSecretKey, err := ctrl.encryptOrKeepSecret(req.SecretKey, existing.SecretKeyHint, existing.EncryptedSecretKey)
	if err != nil {
		return err
	}
	if _, err := ctrl.service.SaveQiniuCredential(c.Request.Context(), accountID, service.QiniuCredentialInput{
		KeyHint:             sandboxKeyHint,
		EncryptedAPIKey:     encryptedAPIKey,
		MAASKeyHint:         maasKeyHint,
		EncryptedMAASAPIKey: encryptedMAASAPIKey,
		AccessKeyHint:       accessKeyHint,
		EncryptedAccessKey:  encryptedAccessKey,
		SecretKeyHint:       secretKeyHint,
		EncryptedSecretKey:  encryptedSecretKey,
	}); err != nil {
		return err
	}
	status, err := ctrl.service.QiniuCredentialStatus(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	return status
}

func (ctrl *Ctrl) DeleteQiniuCredential(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	if err := ctrl.service.DeleteQiniuCredential(c.Request.Context(), accountID); err != nil {
		return err
	}
	return map[string]bool{"ok": true}
}

func (ctrl *Ctrl) encryptOrKeepSecret(value, existingHint, existingEncrypted string) (string, string, error) {
	if value == "" {
		return existingHint, existingEncrypted, nil
	}
	encrypted, err := ctrl.credentialBox.Encrypt(value)
	if err != nil {
		return "", "", err
	}
	return keyHint(value), encrypted, nil
}

func keyHint(accessKey string) string {
	if accessKey == "" {
		return ""
	}
	if len(accessKey) <= 4 {
		return "..." + accessKey
	}
	return "..." + accessKey[len(accessKey)-4:]
}
