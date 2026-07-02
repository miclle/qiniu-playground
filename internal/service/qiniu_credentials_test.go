package service

import (
	"context"
	"testing"
)

func TestSaveQiniuCredentialReturnsStatus(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}

	credential, err := svc.SaveQiniuCredential(context.Background(), user.AccountID, QiniuCredentialInput{
		KeyHint:             "...1234",
		EncryptedAPIKey:     "encrypted-api-key",
		MAASKeyHint:         "...maas",
		EncryptedMAASAPIKey: "encrypted-maas-api-key",
		AccessKeyHint:       "...ak",
		EncryptedAccessKey:  "encrypted-access-key",
		SecretKeyHint:       "...sk",
		EncryptedSecretKey:  "encrypted-secret-key",
	})
	if err != nil {
		t.Fatalf("save qiniu credential: %v", err)
	}
	if credential.KeyHint != "...1234" {
		t.Fatalf("KeyHint = %q, want ...1234", credential.KeyHint)
	}
	if credential.MAASKeyHint != "...maas" || credential.AccessKeyHint != "...ak" || credential.SecretKeyHint != "...sk" {
		t.Fatalf("credential hints = %+v, want optional hints", credential)
	}

	status, err := svc.QiniuCredentialStatus(context.Background(), user.AccountID)
	if err != nil {
		t.Fatalf("credential status: %v", err)
	}
	if !status.Configured || status.KeyHint != "...1234" {
		t.Fatalf("status = %+v, want configured status with key hint", status)
	}
	if !status.MAASConfigured || status.MAASKeyHint != "...maas" {
		t.Fatalf("status = %+v, want configured maas status", status)
	}
	if !status.AccessKeyConfigured || status.AccessKeyHint != "...ak" {
		t.Fatalf("status = %+v, want configured access key status", status)
	}
	if !status.SecretKeyConfigured || status.SecretKeyHint != "...sk" {
		t.Fatalf("status = %+v, want configured secret key status", status)
	}
}

func TestDeleteQiniuCredentialClearsStatus(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	if _, err := svc.SaveQiniuCredential(context.Background(), user.AccountID, QiniuCredentialInput{
		KeyHint:             "...1234",
		EncryptedAPIKey:     "encrypted-api-key",
		MAASKeyHint:         "...maas",
		EncryptedMAASAPIKey: "encrypted-maas-api-key",
	}); err != nil {
		t.Fatalf("save qiniu credential: %v", err)
	}
	if err := svc.DeleteQiniuCredential(context.Background(), user.AccountID); err != nil {
		t.Fatalf("delete qiniu credential: %v", err)
	}

	status, err := svc.QiniuCredentialStatus(context.Background(), user.AccountID)
	if err != nil {
		t.Fatalf("credential status: %v", err)
	}
	if status.Configured {
		t.Fatalf("status = %+v, want unconfigured", status)
	}
}
