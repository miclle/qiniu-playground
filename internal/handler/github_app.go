package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"

	"github.com/miclle/qiniu-playground/internal/service"
)

type installURLResponse struct {
	URL string `json:"url"`
}

type repositoriesResponse struct {
	Repositories []repositoryResponse `json:"repositories"`
}

type repositoryResponse struct {
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	GitHubRepoID   int64  `json:"github_repo_id"`
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	FullName       string `json:"full_name"`
	Private        bool   `json:"private"`
	DefaultBranch  string `json:"default_branch"`
	HTMLURL        string `json:"html_url"`
}

func (ctrl *Ctrl) GitHubAppInstall(c *fox.Context) any {
	if ctrl.githubAppSlug == "" {
		return httperrors.New(http.StatusServiceUnavailable, "GitHub App slug is not configured")
	}
	return installURLResponse{
		URL: fmt.Sprintf("https://github.com/apps/%s/installations/new", ctrl.githubAppSlug),
	}
}

func (ctrl *Ctrl) GitHubAppCallback(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	installationID, err := strconv.ParseInt(c.Query("installation_id"), 10, 64)
	if err != nil || installationID == 0 {
		return httperrors.New(http.StatusBadRequest, "installation_id is required")
	}
	installation, err := ctrl.githubInstallationForAccount(c, accountID, installationID)
	if err != nil {
		return err
	}
	_, err = ctrl.service.SaveGitHubInstallation(c.Request.Context(), accountID, installation)
	if err != nil {
		return err
	}
	c.Redirect(http.StatusFound, "/")
	return nil
}

func (ctrl *Ctrl) GitHubInstallations(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	installations, err := ctrl.service.ListGitHubInstallations(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	return map[string]any{"installations": installations}
}

func (ctrl *Ctrl) GitHubRepositories(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	installations, err := ctrl.githubInstallations(c, accountID)
	if err != nil {
		return err
	}
	for _, installation := range installations {
		repos, err := ctrl.githubApp.ListInstallationRepositories(c.Request.Context(), installation.InstallationID)
		if err != nil {
			return err
		}
		if _, err := ctrl.service.SaveGitHubRepositories(c.Request.Context(), accountID, installation.InstallationID, repos); err != nil {
			return err
		}
	}
	repos, err := ctrl.service.ListGitHubRepositories(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	out := make([]repositoryResponse, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repositoryResponse{
			ID:             repo.ID,
			InstallationID: repo.InstallationID,
			GitHubRepoID:   repo.GitHubRepoID,
			Owner:          repo.Owner,
			Name:           repo.Name,
			FullName:       repo.FullName,
			Private:        repo.Private,
			DefaultBranch:  repo.DefaultBranch,
			HTMLURL:        repo.HTMLURL,
		})
	}
	return repositoriesResponse{Repositories: out}
}

func (ctrl *Ctrl) githubInstallationForAccount(c *fox.Context, accountID string, installationID int64) (service.GitHubInstallationInput, error) {
	user, err := ctrl.service.AuthUserByAccountID(c.Request.Context(), accountID)
	if err != nil {
		return service.GitHubInstallationInput{}, err
	}
	installations, err := ctrl.githubApp.ListAppInstallations(c.Request.Context())
	if err != nil {
		return service.GitHubInstallationInput{}, err
	}
	for _, installation := range installations {
		if installation.InstallationID != installationID {
			continue
		}
		if err := ctrl.authorizeGitHubInstallationTarget(c, accountID, user.Login, installation); err != nil {
			return service.GitHubInstallationInput{}, httperrors.New(http.StatusForbidden, "installation does not belong to the signed-in account")
		}
		return installation, nil
	}
	return service.GitHubInstallationInput{}, httperrors.New(http.StatusForbidden, "installation does not belong to the signed-in account")
}

func (ctrl *Ctrl) githubInstallations(c *fox.Context, accountID string) ([]service.GitHubInstallationInput, error) {
	installations, err := ctrl.service.ListGitHubInstallations(c.Request.Context(), accountID)
	if err != nil {
		return nil, err
	}
	if len(installations) > 0 {
		out := make([]service.GitHubInstallationInput, 0, len(installations))
		for _, installation := range installations {
			out = append(out, service.GitHubInstallationInput{
				InstallationID:      installation.InstallationID,
				TargetType:          installation.TargetType,
				TargetLogin:         installation.TargetLogin,
				RepositorySelection: installation.RepositorySelection,
			})
		}
		return out, nil
	}
	return ctrl.syncGitHubInstallationsForAccount(c, accountID)
}

func (ctrl *Ctrl) syncGitHubInstallationsForAccount(c *fox.Context, accountID string) ([]service.GitHubInstallationInput, error) {
	user, err := ctrl.service.AuthUserByAccountID(c.Request.Context(), accountID)
	if err != nil {
		return nil, err
	}
	installations, err := ctrl.githubApp.ListAppInstallations(c.Request.Context())
	if err != nil {
		return nil, err
	}
	var out []service.GitHubInstallationInput
	for _, installation := range installations {
		if err := ctrl.authorizeGitHubInstallationTarget(c, accountID, user.Login, installation); err != nil {
			continue
		}
		saved, err := ctrl.service.SaveGitHubInstallation(c.Request.Context(), accountID, installation)
		if err != nil {
			return nil, err
		}
		out = append(out, service.GitHubInstallationInput{
			InstallationID:      saved.InstallationID,
			TargetType:          saved.TargetType,
			TargetLogin:         saved.TargetLogin,
			RepositorySelection: saved.RepositorySelection,
		})
	}
	return out, nil
}

func (ctrl *Ctrl) authorizeGitHubInstallationTarget(c *fox.Context, accountID, userLogin string, installation service.GitHubInstallationInput) error {
	if strings.EqualFold(installation.TargetLogin, userLogin) {
		return nil
	}
	if !strings.EqualFold(installation.TargetType, "Organization") {
		return httperrors.New(http.StatusForbidden, "installation does not belong to the signed-in account")
	}
	token, err := ctrl.githubAccessToken(c, accountID)
	if err != nil {
		return httperrors.New(http.StatusForbidden, "organization installation requires GitHub org membership access")
	}
	membership, err := ctrl.githubOAuth.OrgMembership(c.Request.Context(), token, installation.TargetLogin)
	if err != nil {
		return err
	}
	if !strings.EqualFold(membership.State, "active") {
		return httperrors.New(http.StatusForbidden, "organization membership is not active")
	}
	return nil
}
