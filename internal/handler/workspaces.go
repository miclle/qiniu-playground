package handler

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"github.com/google/uuid"
	qiniusb "github.com/qiniu/go-sdk/v7/sandbox"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

type workspacesResponse struct {
	Workspaces []workspaceResponse `json:"workspaces"`
}

type workspaceResponse struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Name          string    `json:"name,omitempty"`
	GitHubRepoID  *int64    `json:"github_repo_id,omitempty"`
	RepoFullName  string    `json:"repo_full_name"`
	Region        string    `json:"region"`
	SandboxID     string    `json:"sandbox_id,omitempty"`
	TemplateID    string    `json:"template_id"`
	State         string    `json:"state,omitempty"`
	Endpoint      string    `json:"endpoint,omitempty"`
	WorkspacePath string    `json:"workspace_path,omitempty"`
	IDEURL        string    `json:"ide_url,omitempty"`
}

type openRepositoryRequest struct {
	Name       string `json:"name"`
	Region     string `json:"region"`
	TemplateID string `json:"template_id"`
}

type createWorkspaceRequest = openRepositoryRequest

type connectWorkspaceRequest struct {
	Recreate bool `json:"recreate"`
}

type workspaceHeartbeatResponse struct {
	OK             bool  `json:"ok"`
	TimeoutSeconds int32 `json:"timeout_seconds"`
}

var (
	workspaceNamePattern      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	invalidWorkspaceNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
	dnsLabelUnsafeChars       = regexp.MustCompile(`[^a-z0-9-]+`)
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
		out = append(out, ctrl.workspaceResponse(c.Request, workspace))
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
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	runtimeEnvs := qiniuRuntimeEnvs(credentials)
	workspaceID := uuid.NewString()
	workspacePath := "/workspace/" + safeWorkspaceName(workspaceName(req.Name, ""))
	metadata := workspaceSandboxMetadata(workspaceID, workspaceName(req.Name, ""), nil, "", req.Region, workspacePath)
	sandboxInfo, err := ctrl.sandboxRuntime.Create(c.Request.Context(), credentials.SandboxAPIKey, sandboxRuntimeCreateRequest{
		TemplateID:      req.TemplateID,
		TimeoutSeconds:  ctrl.defaultSandboxTimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
		Endpoint:        req.Region,
		Metadata:        metadata,
	})
	if err != nil {
		return err
	}
	workspace, err := ctrl.sandboxRuntime.PrepareWorkspace(c.Request.Context(), credentials.SandboxAPIKey, sandboxRuntimeWorkspaceRequest{
		SandboxID:      sandboxInfo.SandboxID,
		TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds,
		Endpoint:       req.Region,
		WorkspacePath:  workspacePath,
		IDEPassword:    ctrl.codeServerPassword(sandboxInfo.SandboxID),
		Envs:           runtimeEnvs,
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
		Metadata:      metadata,
	})
	if err != nil {
		return err
	}
	workspaceRecord, err := ctrl.service.SaveWorkspace(c.Request.Context(), accountID, service.WorkspaceInput{
		ID:            workspaceID,
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
	return ctrl.workspaceResponse(c.Request, *workspaceRecord)
}

func (ctrl *Ctrl) ConnectWorkspace(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	workspaceID := c.Param("workspaceID")
	if workspaceID == "" {
		return httperrors.New(http.StatusBadRequest, "workspace id is required")
	}
	workspace, err := ctrl.service.Workspace(c.Request.Context(), accountID, workspaceID)
	if err != nil {
		return err
	}
	var req connectWorkspaceRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return httperrors.New(http.StatusBadRequest, "invalid request body")
		}
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	runtimeEnvs := qiniuRuntimeEnvs(credentials)
	sandboxID := workspace.SandboxID
	var templateID string
	workspacePath := workspaceRuntimePath(workspace)
	metadata := workspaceSandboxMetadata(workspace.ID, workspace.Name, workspace.GitHubRepoID, workspace.RepoFullName, workspace.Region, workspacePath)
	if sandboxID == "" || req.Recreate {
		info, err := ctrl.sandboxRuntime.Create(c.Request.Context(), credentials.SandboxAPIKey, sandboxRuntimeCreateRequest{
			TemplateID:      workspace.TemplateID,
			TimeoutSeconds:  ctrl.defaultSandboxTimeoutSeconds,
			PollingInterval: defaultSandboxPollInterval,
			Endpoint:        workspace.Region,
			Metadata:        metadata,
		})
		if err != nil {
			return err
		}
		sandboxID = info.SandboxID
		templateID = info.TemplateID
	} else {
		info, err := ctrl.sandboxRuntime.Connect(c.Request.Context(), credentials.SandboxAPIKey, sandboxID, ctrl.defaultSandboxTimeoutSeconds, workspace.Region)
		if err != nil {
			if isSandboxNotFoundError(err) {
				return httperrors.New(http.StatusConflict, "workspace sandbox no longer exists")
			}
			return err
		}
		templateID = info.TemplateID
	}
	runtimeWorkspace, err := ctrl.prepareConnectedWorkspace(c, accountID, credentials.SandboxAPIKey, runtimeEnvs, sandboxID, workspace, workspacePath)
	if err != nil {
		return err
	}
	if runtimeWorkspace == nil {
		return httperrors.New(http.StatusInternalServerError, "prepared workspace is nil")
	}
	if runtimeWorkspace.TemplateID != "" {
		templateID = runtimeWorkspace.TemplateID
	}
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    templateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		GitHubRepoID:  workspace.GitHubRepoID,
		RepoFullName:  workspace.RepoFullName,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
		Region:        workspace.Region,
		IDEURL:        runtimeWorkspace.IDEURL,
		Metadata:      metadata,
	})
	if err != nil {
		return err
	}
	workspaceRecord, err := ctrl.service.UpdateWorkspaceRuntime(c.Request.Context(), accountID, workspace.ID, service.WorkspaceInput{
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
	return ctrl.workspaceResponse(c.Request, *workspaceRecord)
}

func (ctrl *Ctrl) WorkspaceHeartbeat(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	workspaceID := c.Param("workspaceID")
	if workspaceID == "" {
		return httperrors.New(http.StatusBadRequest, "workspace id is required")
	}
	workspace, err := ctrl.service.Workspace(c.Request.Context(), accountID, workspaceID)
	if err != nil {
		return httperrors.New(http.StatusNotFound, "workspace not found")
	}
	if workspace.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "workspace sandbox is not connected")
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	if err := ctrl.sandboxRuntime.SetTimeout(
		c.Request.Context(),
		credentials.SandboxAPIKey,
		workspace.SandboxID,
		workspace.Region,
		ctrl.defaultSandboxTimeoutSeconds,
	); err != nil {
		return err
	}
	return workspaceHeartbeatResponse{OK: true, TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds}
}

func (ctrl *Ctrl) PauseWorkspaceSandbox(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	workspaceID := c.Param("workspaceID")
	if workspaceID == "" {
		return httperrors.New(http.StatusBadRequest, "workspace id is required")
	}
	workspace, err := ctrl.service.Workspace(c.Request.Context(), accountID, workspaceID)
	if err != nil {
		return httperrors.New(http.StatusNotFound, "workspace not found")
	}
	if workspace.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "workspace sandbox is not connected")
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	if err := ctrl.sandboxRuntime.Pause(c.Request.Context(), credentials.SandboxAPIKey, workspace.SandboxID, workspace.Region); err != nil {
		return err
	}
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:     workspace.SandboxID,
		TemplateID:    workspace.TemplateID,
		State:         "paused",
		Endpoint:      workspace.Endpoint,
		GitHubRepoID:  workspace.GitHubRepoID,
		RepoFullName:  workspace.RepoFullName,
		WorkspacePath: workspace.WorkspacePath,
		Region:        workspace.Region,
		IDEURL:        workspace.IDEURL,
	})
	if err != nil {
		return err
	}
	workspaceRecord, err := ctrl.service.UpdateWorkspaceRuntime(c.Request.Context(), accountID, workspace.ID, service.WorkspaceInput{
		SandboxID:     workspace.SandboxID,
		TemplateID:    workspace.TemplateID,
		State:         session.State,
		Endpoint:      workspace.Endpoint,
		WorkspacePath: workspace.WorkspacePath,
		IDEURL:        workspace.IDEURL,
	})
	if err != nil {
		return err
	}
	return ctrl.workspaceResponse(c.Request, *workspaceRecord)
}

func (ctrl *Ctrl) prepareConnectedWorkspace(
	c *fox.Context,
	accountID string,
	apiKey string,
	runtimeEnvs map[string]string,
	sandboxID string,
	workspace *entity.Workspace,
	workspacePath string,
) (*sandboxRuntimeWorkspace, error) {
	if workspace.GitHubRepoID == nil || workspace.RepoFullName == "" {
		return ctrl.sandboxRuntime.PrepareWorkspace(c.Request.Context(), apiKey, sandboxRuntimeWorkspaceRequest{
			SandboxID:      sandboxID,
			TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds,
			Endpoint:       workspace.Region,
			WorkspacePath:  workspacePath,
			IDEPassword:    ctrl.codeServerPassword(sandboxID),
			Envs:           runtimeEnvs,
		})
	}
	repo, err := ctrl.service.GitHubRepositoryByGitHubRepoID(c.Request.Context(), accountID, *workspace.GitHubRepoID)
	if err != nil {
		return nil, err
	}
	token, err := ctrl.githubApp.InstallationToken(c.Request.Context(), repo.InstallationID)
	if err != nil {
		return nil, err
	}
	runtimeWorkspace, err := ctrl.sandboxRuntime.PrepareRepository(c.Request.Context(), apiKey, sandboxRuntimeRepositoryRequest{
		SandboxID:      sandboxID,
		TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds,
		Endpoint:       workspace.Region,
		FullName:       repo.FullName,
		DefaultBranch:  repo.DefaultBranch,
		Token:          token,
		WorkspacePath:  workspacePath,
		IDEPassword:    ctrl.codeServerPassword(sandboxID),
		Envs:           runtimeEnvs,
	})
	if err != nil {
		return nil, httperrors.New(http.StatusInternalServerError, "failed to prepare repository: "+redactSecret(err.Error(), token))
	}
	return runtimeWorkspace, nil
}

func isSandboxNotFoundError(err error) bool {
	var apiErr *qiniusb.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusNotFound {
			return true
		}
		return apiErr.StatusCode == http.StatusBadGateway && strings.Contains(strings.ToLower(apiErr.Message), "sandbox was not found")
	}
	var controlErr *sandboxMetricsAPIError
	if !errors.As(err, &controlErr) {
		return false
	}
	if controlErr.StatusCode == http.StatusNotFound {
		return true
	}
	return controlErr.StatusCode == http.StatusBadGateway && strings.Contains(strings.ToLower(string(controlErr.Body)), "sandbox was not found")
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
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	runtimeEnvs := qiniuRuntimeEnvs(credentials)
	workspaceID := uuid.NewString()
	if existing, err := ctrl.service.WorkspaceByGitHubRepoID(c.Request.Context(), accountID, repo.GitHubRepoID); err == nil {
		workspaceID = existing.ID
	}
	workspacePath := "/workspace/" + safeWorkspaceName(repo.FullName)
	metadata := workspaceSandboxMetadata(workspaceID, workspaceName(req.Name, repo.FullName), &repo.GitHubRepoID, repo.FullName, req.Region, workspacePath)
	sandboxInfo, err := ctrl.sandboxRuntime.Create(c.Request.Context(), credentials.SandboxAPIKey, sandboxRuntimeCreateRequest{
		TemplateID:      req.TemplateID,
		TimeoutSeconds:  ctrl.defaultSandboxTimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
		Endpoint:        req.Region,
		Metadata:        metadata,
	})
	if err != nil {
		return err
	}
	token, err := ctrl.githubApp.InstallationToken(c.Request.Context(), repo.InstallationID)
	if err != nil {
		return err
	}
	workspace, err := ctrl.sandboxRuntime.PrepareRepository(c.Request.Context(), credentials.SandboxAPIKey, sandboxRuntimeRepositoryRequest{
		SandboxID:      sandboxInfo.SandboxID,
		TimeoutSeconds: ctrl.defaultSandboxTimeoutSeconds,
		Endpoint:       req.Region,
		FullName:       repo.FullName,
		DefaultBranch:  repo.DefaultBranch,
		Token:          token,
		WorkspacePath:  workspacePath,
		IDEPassword:    ctrl.codeServerPassword(sandboxInfo.SandboxID),
		Envs:           runtimeEnvs,
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
		Metadata:      metadata,
	})
	if err != nil {
		return err
	}
	workspaceRecord, err := ctrl.service.SaveWorkspace(c.Request.Context(), accountID, service.WorkspaceInput{
		Name:          workspaceName(req.Name, repo.FullName),
		GitHubRepoID:  &githubRepoID,
		RepoFullName:  repo.FullName,
		ID:            workspaceID,
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
	return ctrl.workspaceResponse(c.Request, *workspaceRecord)
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

func qiniuRuntimeEnvs(credentials qiniuRuntimeCredentialSet) map[string]string {
	if credentials.MAASAPIKey == "" {
		return nil
	}
	return map[string]string{
		"ANTHROPIC_AUTH_TOKEN": credentials.MAASAPIKey,
		"ANTHROPIC_BASE_URL":   "https://api.qnaigc.com",
		"OPENAI_API_KEY":       credentials.MAASAPIKey,
		"OPENAI_BASE_URL":      "https://api.qnaigc.com/v1",
		"QINIU_MAAS_API_KEY":   credentials.MAASAPIKey,
	}
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

func (ctrl *Ctrl) workspaceResponse(req *http.Request, workspace entity.Workspace) workspaceResponse {
	return workspaceResponse{
		ID:            workspace.ID,
		CreatedAt:     workspace.CreatedAt,
		UpdatedAt:     workspace.UpdatedAt,
		Name:          workspace.Name,
		GitHubRepoID:  workspace.GitHubRepoID,
		RepoFullName:  workspace.RepoFullName,
		Region:        workspace.Region,
		SandboxID:     workspace.SandboxID,
		TemplateID:    workspace.TemplateID,
		State:         workspace.State,
		Endpoint:      workspace.Endpoint,
		WorkspacePath: workspace.WorkspacePath,
		IDEURL:        ctrl.ideProxyURL(req, workspace.AccountID, workspace.SandboxID, workspace.IDEURL),
	}
}

func workspaceRuntimePath(workspace *entity.Workspace) string {
	if workspace.WorkspacePath != "" {
		return workspace.WorkspacePath
	}
	if workspace.GitHubRepoID != nil {
		return "/workspace/" + safeWorkspaceName(workspace.RepoFullName)
	}
	return "/workspace/" + safeWorkspaceName(workspace.Name)
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
