package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

const defaultSandboxPollInterval = 2 * time.Second

type createSandboxRequest struct {
	TemplateID     string `json:"template_id"`
	TimeoutSeconds int32  `json:"timeout_seconds"`
}

type sandboxSessionResponse struct {
	ID              string `json:"id"`
	SandboxID       string `json:"sandbox_id"`
	TemplateID      string `json:"template_id"`
	State           string `json:"state"`
	Endpoint        string `json:"endpoint,omitempty"`
	GitHubRepoID    *int64 `json:"github_repo_id,omitempty"`
	RepoFullName    string `json:"repo_full_name,omitempty"`
	WorkspacePath   string `json:"workspace_path,omitempty"`
	Region          string `json:"region,omitempty"`
	CPUCount        int32  `json:"cpu_count,omitempty"`
	MemoryGB        int32  `json:"memory_gb,omitempty"`
	IDEURL          string `json:"ide_url,omitempty"`
	LastConnectedAt string `json:"last_connected_at,omitempty"`
}

func (ctrl *Ctrl) SandboxSessions(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	sessions, err := ctrl.service.ListSandboxSessions(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	out := make([]sandboxSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, ctrl.sandboxSessionResponse(session))
	}
	return map[string]any{"sandboxes": out}
}

func (ctrl *Ctrl) CreateSandbox(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	var req createSandboxRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return httperrors.New(http.StatusBadRequest, "invalid request body")
		}
	}
	if req.TemplateID == "" {
		req.TemplateID = ctrl.defaultSandboxTemplateID
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = ctrl.defaultSandboxTimeoutSeconds
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	info, err := ctrl.sandboxRuntime.Create(c.Request.Context(), apiKey, sandboxRuntimeCreateRequest{
		TemplateID:      req.TemplateID,
		TimeoutSeconds:  req.TimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
	})
	if err != nil {
		return err
	}
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:  info.SandboxID,
		TemplateID: info.TemplateID,
		State:      info.State,
		Endpoint:   info.Endpoint,
	})
	if err != nil {
		return err
	}
	return ctrl.sandboxSessionResponse(*session)
}

func (ctrl *Ctrl) ConnectSandbox(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	sandboxID := c.Param("sandboxID")
	if sandboxID == "" {
		return httperrors.New(http.StatusBadRequest, "sandbox id is required")
	}
	var endpoint string
	if session, err := ctrl.service.SandboxSession(c.Request.Context(), accountID, sandboxID); err == nil {
		endpoint = session.Region
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	info, err := ctrl.sandboxRuntime.Connect(c.Request.Context(), apiKey, sandboxID, ctrl.defaultSandboxTimeoutSeconds, endpoint)
	if err != nil {
		return err
	}
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:  info.SandboxID,
		TemplateID: info.TemplateID,
		State:      info.State,
		Endpoint:   info.Endpoint,
		Region:     endpoint,
	})
	if err != nil {
		return err
	}
	return ctrl.sandboxSessionResponse(*session)
}

func (ctrl *Ctrl) qiniuAPIKey(c *fox.Context, accountID string) (string, error) {
	credential, err := ctrl.service.QiniuCredential(c.Request.Context(), accountID)
	if err != nil {
		return "", httperrors.New(http.StatusPreconditionRequired, "Qiniu API key is not configured")
	}
	apiKey, err := ctrl.credentialBox.Decrypt(credential.EncryptedAPIKey)
	if err != nil {
		return "", err
	}
	return apiKey, nil
}

func (ctrl *Ctrl) githubAccessToken(c *fox.Context, accountID string) (string, error) {
	encryptedToken, err := ctrl.service.GitHubAccessToken(c.Request.Context(), accountID)
	if err != nil {
		return "", err
	}
	token, err := ctrl.credentialBox.Decrypt(encryptedToken)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (ctrl *Ctrl) sandboxSessionResponse(session entity.SandboxSession) sandboxSessionResponse {
	out := sandboxSessionResponse{
		ID:            session.ID,
		SandboxID:     session.SandboxID,
		TemplateID:    session.TemplateID,
		State:         session.State,
		Endpoint:      session.Endpoint,
		GitHubRepoID:  session.GitHubRepoID,
		RepoFullName:  session.RepoFullName,
		WorkspacePath: session.WorkspacePath,
		Region:        session.Region,
		CPUCount:      session.CPUCount,
		MemoryGB:      session.MemoryGB,
		IDEURL:        ctrl.ideProxyURL(session.SandboxID, session.IDEURL),
	}
	if session.LastConnectedAt != nil {
		out.LastConnectedAt = session.LastConnectedAt.Format(time.RFC3339)
	}
	return out
}
