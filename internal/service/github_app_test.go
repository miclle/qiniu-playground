package service

import (
	"context"
	"testing"
)

func TestSaveGitHubInstallationUpsertsByAccountAndInstallation(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}

	first, err := svc.SaveGitHubInstallation(context.Background(), user.AccountID, GitHubInstallationInput{
		InstallationID:      42,
		TargetType:          "User",
		TargetLogin:         "octocat",
		RepositorySelection: "selected",
	})
	if err != nil {
		t.Fatalf("save installation: %v", err)
	}
	second, err := svc.SaveGitHubInstallation(context.Background(), user.AccountID, GitHubInstallationInput{
		InstallationID:      42,
		TargetType:          "User",
		TargetLogin:         "renamed-octocat",
		RepositorySelection: "all",
	})
	if err != nil {
		t.Fatalf("save installation again: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("ID = %q, want reused %q", second.ID, first.ID)
	}
	if second.TargetLogin != "renamed-octocat" {
		t.Fatalf("TargetLogin = %q, want renamed-octocat", second.TargetLogin)
	}
}

func TestSaveGitHubRepositoriesUpsertsByInstallationAndRepoID(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}

	repos, err := svc.SaveGitHubRepositories(context.Background(), user.AccountID, 42, []GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			Private:       true,
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	})
	if err != nil {
		t.Fatalf("save repositories: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1", len(repos))
	}

	repos, err = svc.SaveGitHubRepositories(context.Background(), user.AccountID, 42, []GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world-renamed",
			FullName:      "octocat/hello-world-renamed",
			DefaultBranch: "trunk",
			HTMLURL:       "https://github.com/octocat/hello-world-renamed",
		},
	})
	if err != nil {
		t.Fatalf("save repositories again: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1 after upsert", len(repos))
	}
	if repos[0].FullName != "octocat/hello-world-renamed" {
		t.Fatalf("FullName = %q, want renamed repository", repos[0].FullName)
	}
}

func TestSaveGitHubRepositoriesKeepsAccountsIsolated(t *testing.T) {
	svc := newTestService(t)
	firstUser, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert first identity: %v", err)
	}
	secondUser, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "67890",
		Login:           "hubot",
	})
	if err != nil {
		t.Fatalf("upsert second identity: %v", err)
	}
	input := []GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	}
	if _, err := svc.SaveGitHubRepositories(context.Background(), firstUser.AccountID, 42, input); err != nil {
		t.Fatalf("save first repositories: %v", err)
	}
	if _, err := svc.SaveGitHubRepositories(context.Background(), secondUser.AccountID, 42, input); err != nil {
		t.Fatalf("save second repositories: %v", err)
	}

	firstRepos, err := svc.ListGitHubRepositories(context.Background(), firstUser.AccountID)
	if err != nil {
		t.Fatalf("list first repositories: %v", err)
	}
	secondRepos, err := svc.ListGitHubRepositories(context.Background(), secondUser.AccountID)
	if err != nil {
		t.Fatalf("list second repositories: %v", err)
	}
	if len(firstRepos) != 1 || len(secondRepos) != 1 {
		t.Fatalf("repo counts = %d/%d, want 1/1", len(firstRepos), len(secondRepos))
	}
	if firstRepos[0].AccountID != firstUser.AccountID || secondRepos[0].AccountID != secondUser.AccountID {
		t.Fatalf("repository owners = %q/%q, want account isolation", firstRepos[0].AccountID, secondRepos[0].AccountID)
	}
}

func TestSaveGitHubRepositoriesDeletesStaleRepositories(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.UpsertGitHubIdentity(context.Background(), OAuthUser{
		ProviderSubject: "12345",
		Login:           "octocat",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}

	if _, err := svc.SaveGitHubRepositories(context.Background(), user.AccountID, 42, []GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
		{
			GitHubRepoID:  101,
			Owner:         "octocat",
			Name:          "old-repo",
			FullName:      "octocat/old-repo",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/old-repo",
		},
	}); err != nil {
		t.Fatalf("save initial repositories: %v", err)
	}
	repos, err := svc.SaveGitHubRepositories(context.Background(), user.AccountID, 42, []GitHubRepositoryInput{
		{
			GitHubRepoID:  100,
			Owner:         "octocat",
			Name:          "hello-world",
			FullName:      "octocat/hello-world",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/octocat/hello-world",
		},
	})
	if err != nil {
		t.Fatalf("sync repositories: %v", err)
	}
	if len(repos) != 1 || repos[0].GitHubRepoID != 100 {
		t.Fatalf("repositories = %+v, want only active repo", repos)
	}
}
