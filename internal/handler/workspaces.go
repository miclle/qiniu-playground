package handler

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

type workspacesResponse struct {
	Workspaces []workspaceResponse `json:"workspaces"`
}

type workspaceResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	GitHubRepoID  *int64 `json:"github_repo_id,omitempty"`
	RepoFullName  string `json:"repo_full_name"`
	Region        string `json:"region"`
	SandboxID     string `json:"sandbox_id,omitempty"`
	TemplateID    string `json:"template_id"`
	State         string `json:"state,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	IDEURL        string `json:"ide_url,omitempty"`
}

type openRepositoryRequest struct {
	Name       string `json:"name"`
	Region     string `json:"region"`
	TemplateID string `json:"template_id"`
}

type createWorkspaceRequest = openRepositoryRequest

var (
	workspaceNamePattern      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	invalidWorkspaceNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
)

func (ctrl *Ctrl) Workspaces(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	if err := ctrl.backfillWorkspacesFromSandboxSessions(c, accountID); err != nil {
		c.Logger.Warnf("failed to backfill workspaces from sandbox sessions: %v", err)
	}
	workspaces, err := ctrl.service.ListWorkspaces(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	out := make([]workspaceResponse, 0, len(workspaces))
	for _, workspace := range workspaces {
		out = append(out, ctrl.workspaceResponse(workspace))
	}
	return workspacesResponse{Workspaces: out}
}

func (ctrl *Ctrl) CreateWorkspace(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	var req createWorkspaceRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return httperrors.New(http.StatusBadRequest, "invalid request body")
		}
	}
	if err := validateOpenRepositoryRequest(openRepositoryRequest(req)); err != nil {
		return err
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	sandboxInfo, err := ctrl.sandboxRuntime.Create(c.Request.Context(), apiKey, sandboxRuntimeCreateRequest{
		TemplateID:      req.TemplateID,
		TimeoutSeconds:  ctrl.defaultSandboxTimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
		Endpoint:        req.Region,
	})
	if err != nil {
		return err
	}
	workspace, err := ctrl.sandboxRuntime.PrepareWorkspace(c.Request.Context(), apiKey, sandboxRuntimeWorkspaceRequest{
		SandboxID:      sandboxInfo.SandboxID,
		TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds,
		Endpoint:       req.Region,
		WorkspacePath:  "/workspace/" + safeWorkspaceName(workspaceName(req.Name, "")),
	})
	if err != nil {
		return err
	}
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:     workspace.SandboxID,
		TemplateID:    workspace.TemplateID,
		State:         workspace.State,
		Endpoint:      workspace.Endpoint,
		WorkspacePath: workspace.WorkspacePath,
		Region:        req.Region,
		IDEURL:        workspace.IDEURL,
	})
	if err != nil {
		return err
	}
	workspaceRecord, err := ctrl.service.SaveWorkspace(c.Request.Context(), accountID, service.WorkspaceInput{
		Name:          workspaceName(req.Name, ""),
		Region:        req.Region,
		SandboxID:     session.SandboxID,
		TemplateID:    session.TemplateID,
		State:         session.State,
		Endpoint:      session.Endpoint,
		WorkspacePath: session.WorkspacePath,
		IDEURL:        session.IDEURL,
	})
	if err != nil {
		return err
	}
	return ctrl.workspaceResponse(*workspaceRecord)
}

func (ctrl *Ctrl) backfillWorkspacesFromSandboxSessions(c *fox.Context, accountID string) error {
	sessions, err := ctrl.service.ListSandboxSessions(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return nil
	}
	workspaces, err := ctrl.service.ListWorkspaces(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	existingRepoIDs := make(map[int64]bool, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.GitHubRepoID != nil {
			existingRepoIDs[*workspace.GitHubRepoID] = true
		}
	}
	for _, session := range sessions {
		if session.GitHubRepoID == nil || session.RepoFullName == "" || session.Region == "" || session.TemplateID == "" {
			continue
		}
		githubRepoID := *session.GitHubRepoID
		if existingRepoIDs[githubRepoID] {
			continue
		}
		if _, err := ctrl.service.SaveWorkspace(c.Request.Context(), accountID, service.WorkspaceInput{
			Name:          workspaceName("", session.RepoFullName),
			GitHubRepoID:  &githubRepoID,
			RepoFullName:  session.RepoFullName,
			Region:        session.Region,
			SandboxID:     session.SandboxID,
			TemplateID:    session.TemplateID,
			State:         session.State,
			Endpoint:      session.Endpoint,
			WorkspacePath: session.WorkspacePath,
			IDEURL:        session.IDEURL,
		}); err != nil {
			return err
		}
		existingRepoIDs[githubRepoID] = true
	}
	return nil
}

func (ctrl *Ctrl) OpenRepository(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	repositoryID := c.Param("repositoryID")
	if repositoryID == "" {
		return httperrors.New(http.StatusBadRequest, "repository id is required")
	}
	repo, err := ctrl.service.GitHubRepository(c.Request.Context(), accountID, repositoryID)
	if err != nil {
		return err
	}
	var req openRepositoryRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return httperrors.New(http.StatusBadRequest, "invalid request body")
		}
	}
	if err := validateOpenRepositoryRequest(req); err != nil {
		return err
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	sandboxInfo, err := ctrl.sandboxRuntime.Create(c.Request.Context(), apiKey, sandboxRuntimeCreateRequest{
		TemplateID:      req.TemplateID,
		TimeoutSeconds:  ctrl.defaultSandboxTimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
		Endpoint:        req.Region,
	})
	if err != nil {
		return err
	}
	token, err := ctrl.githubApp.InstallationToken(c.Request.Context(), repo.InstallationID)
	if err != nil {
		return err
	}
	workspace, err := ctrl.sandboxRuntime.PrepareRepository(c.Request.Context(), apiKey, sandboxRuntimeRepositoryRequest{
		SandboxID:      sandboxInfo.SandboxID,
		TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds,
		Endpoint:       req.Region,
		FullName:       repo.FullName,
		DefaultBranch:  repo.DefaultBranch,
		Token:          token,
	})
	if err != nil {
		return httperrors.New(http.StatusInternalServerError, "failed to prepare repository: "+redactSecret(err.Error(), token))
	}
	githubRepoID := repo.GitHubRepoID
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:     workspace.SandboxID,
		TemplateID:    workspace.TemplateID,
		State:         workspace.State,
		Endpoint:      workspace.Endpoint,
		GitHubRepoID:  &repo.GitHubRepoID,
		RepoFullName:  repo.FullName,
		WorkspacePath: workspace.WorkspacePath,
		Region:        req.Region,
		IDEURL:        workspace.IDEURL,
	})
	if err != nil {
		return err
	}
	workspaceRecord, err := ctrl.service.SaveWorkspace(c.Request.Context(), accountID, service.WorkspaceInput{
		Name:          workspaceName(req.Name, repo.FullName),
		GitHubRepoID:  &githubRepoID,
		RepoFullName:  repo.FullName,
		Region:        req.Region,
		SandboxID:     session.SandboxID,
		TemplateID:    session.TemplateID,
		State:         session.State,
		Endpoint:      session.Endpoint,
		WorkspacePath: session.WorkspacePath,
		IDEURL:        session.IDEURL,
	})
	if err != nil {
		return err
	}
	return ctrl.workspaceResponse(*workspaceRecord)
}

func validateOpenRepositoryRequest(req openRepositoryRequest) error {
	if err := validateWorkspaceName(req.Name); err != nil {
		return err
	}
	if req.Region == "" {
		return httperrors.New(http.StatusBadRequest, "region is required")
	}
	if req.TemplateID == "" {
		return httperrors.New(http.StatusBadRequest, "template id is required")
	}
	return nil
}

func redactSecret(message, secret string) string {
	if secret == "" {
		return message
	}
	message = strings.ReplaceAll(message, secret, "REDACTED")
	message = strings.ReplaceAll(message, url.QueryEscape(secret), "REDACTED")
	return message
}

func validateWorkspaceName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if !workspaceNamePattern.MatchString(name) {
		return httperrors.New(http.StatusBadRequest, "workspace name may only contain letters, numbers, underscores, and hyphens")
	}
	return nil
}

func (ctrl *Ctrl) workspaceResponse(workspace entity.Workspace) workspaceResponse {
	return workspaceResponse{
		ID:            workspace.ID,
		Name:          workspace.Name,
		GitHubRepoID:  workspace.GitHubRepoID,
		RepoFullName:  workspace.RepoFullName,
		Region:        workspace.Region,
		SandboxID:     workspace.SandboxID,
		TemplateID:    workspace.TemplateID,
		State:         workspace.State,
		Endpoint:      workspace.Endpoint,
		WorkspacePath: workspace.WorkspacePath,
		IDEURL:        ctrl.ideProxyURL(workspace.SandboxID, workspace.IDEURL),
	}
}

func workspaceName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	fallback = invalidWorkspaceNameChars.ReplaceAllString(strings.TrimSpace(fallback), "-")
	fallback = strings.Trim(fallback, "-_")
	if fallback != "" {
		return fallback
	}
	return "workspace"
}
