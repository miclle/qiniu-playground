package service

import (
	"context"
	"testing"
)

func TestUpdateWorkspaceRuntimePreservesStaticWorkspaceFields(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	repoID := int64(100)
	workspace, err := svc.SaveWorkspace(context.Background(), user.AccountID, WorkspaceInput{
		Name:          "Hello",
		GitHubRepoID:  &repoID,
		RepoFullName:  "octocat/hello-world",
		Region:        "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:     "sandbox-old",
		TemplateID:    "node",
		State:         "running",
		Endpoint:      "old.example.test",
		WorkspacePath: "/workspace/octocat-hello-world",
		IDEURL:        "https://old.example.test",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	updated, err := svc.UpdateWorkspaceRuntime(context.Background(), user.AccountID, workspace.ID, WorkspaceInput{
		SandboxID:     "sandbox-new",
		TemplateID:    "node",
		State:         "running",
		Endpoint:      "new.example.test",
		WorkspacePath: "/workspace/octocat-hello-world",
		IDEURL:        "https://new.example.test",
	})
	if err != nil {
		t.Fatalf("update workspace runtime: %v", err)
	}

	if updated.Name != "Hello" {
		t.Fatalf("Name = %q, want preserved static field", updated.Name)
	}
	if updated.GitHubRepoID == nil || *updated.GitHubRepoID != repoID {
		t.Fatalf("GitHubRepoID = %v, want preserved repo id", updated.GitHubRepoID)
	}
	if updated.RepoFullName != "octocat/hello-world" || updated.Region != "https://cn-yangzhou-1-sandbox.qiniuapi.com" {
		t.Fatalf("static fields = %q/%q, want preserved repo and region", updated.RepoFullName, updated.Region)
	}
	if updated.SandboxID != "sandbox-new" || updated.Endpoint != "new.example.test" || updated.IDEURL != "https://new.example.test" {
		t.Fatalf("runtime fields = %+v, want updated runtime", updated)
	}
}

func TestSaveWorkspaceUsesProvidedID(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	workspace, err := svc.SaveWorkspace(context.Background(), user.AccountID, WorkspaceInput{
		ID:         "11111111-1111-4111-8111-111111111111",
		Name:       "Scratch",
		Region:     "https://cn-yangzhou-1-sandbox.qiniuapi.com",
		SandboxID:  "sandbox-1",
		TemplateID: "node",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	if workspace.ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("ID = %q, want provided id", workspace.ID)
	}
}
