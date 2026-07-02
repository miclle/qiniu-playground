package service

import (
	"context"
	"testing"
)

func TestSaveSandboxSessionUpsertsByAccountAndSandbox(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}

	first, err := svc.SaveSandboxSession(context.Background(), user.AccountID, SandboxSessionInput{
		SandboxID:  "sandbox-1",
		TemplateID: "base",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save sandbox session: %v", err)
	}
	second, err := svc.SaveSandboxSession(context.Background(), user.AccountID, SandboxSessionInput{
		SandboxID:  "sandbox-1",
		TemplateID: "node",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save sandbox session again: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("ID = %q, want reused %q", second.ID, first.ID)
	}
	if second.TemplateID != "node" {
		t.Fatalf("TemplateID = %q, want node", second.TemplateID)
	}
	sessions, err := svc.ListSandboxSessions(context.Background(), user.AccountID)
	if err != nil {
		t.Fatalf("list sandbox sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
}

func TestSaveSandboxSessionPreservesRepositoryMetadataOnReconnect(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	repoID := int64(100)
	if _, err := svc.SaveSandboxSession(context.Background(), user.AccountID, SandboxSessionInput{
		SandboxID:     "sandbox-1",
		TemplateID:    "base",
		State:         "running",
		Endpoint:      "old.example.test",
		GitHubRepoID:  &repoID,
		RepoFullName:  "octocat/hello-world",
		WorkspacePath: "/workspace/octocat__hello-world",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		CPUCount:      2,
		MemoryGB:      4,
		IDEURL:        "https://old.example.test",
	}); err != nil {
		t.Fatalf("save repository session: %v", err)
	}

	session, err := svc.SaveSandboxSession(context.Background(), user.AccountID, SandboxSessionInput{
		SandboxID:  "sandbox-1",
		TemplateID: "base",
		State:      "running",
		Endpoint:   "new.example.test",
	})
	if err != nil {
		t.Fatalf("save reconnect session: %v", err)
	}

	if session.Endpoint != "new.example.test" {
		t.Fatalf("Endpoint = %q, want new endpoint", session.Endpoint)
	}
	if session.GitHubRepoID == nil || *session.GitHubRepoID != repoID {
		t.Fatalf("GitHubRepoID = %v, want preserved repo id", session.GitHubRepoID)
	}
	if session.RepoFullName != "octocat/hello-world" {
		t.Fatalf("RepoFullName = %q, want preserved repo", session.RepoFullName)
	}
	if session.WorkspacePath != "/workspace/octocat__hello-world" {
		t.Fatalf("WorkspacePath = %q, want preserved workspace", session.WorkspacePath)
	}
	if session.Region != "https://cn-yangzhou-1-sandbox.qiniuapi.com" || session.CPUCount != 2 || session.MemoryGB != 4 {
		t.Fatalf("workspace config = %q/%d/%d, want preserved config", session.Region, session.CPUCount, session.MemoryGB)
	}
	if session.IDEURL != "https://old.example.test" {
		t.Fatalf("IDEURL = %q, want preserved IDE URL", session.IDEURL)
	}
}
