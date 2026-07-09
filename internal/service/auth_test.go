package service

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/entity"
)

func newTestService(t *testing.T) *Service {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.Account{},
		&entity.OAuthIdentity{},
		&entity.GitHubInstallation{},
		&entity.GitHubRepository{},
		&entity.Workspace{},
		&entity.WorkspaceChatMessage{},
		&entity.QiniuCredential{},
		&entity.SandboxSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

func TestUpsertGitHubIdentityCreatesAccount(t *testing.T) {
	svc := newTestService(t)

	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
		DisplayName:     "The Octocat",
		AvatarURL:       "https://avatars.example/octocat.png",
		Email:           "octocat@example.com",
	})
	if err != nil {
		t.Fatalf("UpsertGitHubIdentity returned error: %v", err)
	}

	if user.AccountID == "" {
		t.Fatal("AccountID should be set")
	}
	if user.Subject != "12345" {
		t.Fatalf("Subject = %q, want 12345", user.Subject)
	}
	if user.Name != "The Octocat" {
		t.Fatalf("Name = %q, want The Octocat", user.Name)
	}
}

func TestUpsertGitHubIdentityReusesStableSubject(t *testing.T) {
	svc := newTestService(t)

	first, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
		DisplayName:     "The Octocat",
	})
	if err != nil {
		t.Fatalf("first upsert returned error: %v", err)
	}
	second, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "renamed-octocat",
		DisplayName:     "Renamed Octocat",
	})
	if err != nil {
		t.Fatalf("second upsert returned error: %v", err)
	}

	if second.AccountID != first.AccountID {
		t.Fatalf("AccountID = %q, want reused %q", second.AccountID, first.AccountID)
	}
	if second.Login != "renamed-octocat" {
		t.Fatalf("Login = %q, want latest login", second.Login)
	}
}

func TestUpsertGitHubIdentityValidatesSubjectAndLogin(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{Login: "octocat"}); err == nil {
		t.Fatal("missing subject should fail")
	}
	if _, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{ProviderSubject: "12345"}); err == nil {
		t.Fatal("missing login should fail")
	}
}
