package handler

import (
	"net/http"
	"strconv"
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
	Region         string `json:"region"`
}

type connectSandboxRequest struct {
	Region string `json:"region"`
}

type sandboxSessionResponse struct {
	ID              string            `json:"id"`
	SandboxID       string            `json:"sandbox_id"`
	TemplateID      string            `json:"template_id"`
	State           string            `json:"state"`
	Endpoint        string            `json:"endpoint,omitempty"`
	GitHubRepoID    *int64            `json:"github_repo_id,omitempty"`
	RepoFullName    string            `json:"repo_full_name,omitempty"`
	WorkspacePath   string            `json:"workspace_path,omitempty"`
	Region          string            `json:"region,omitempty"`
	CPUCount        int32             `json:"cpu_count,omitempty"`
	MemoryGB        int32             `json:"memory_gb,omitempty"`
	DiskSizeMB      int32             `json:"disk_size_mb,omitempty"`
	IDEURL          string            `json:"ide_url,omitempty"`
	LocalSession    bool              `json:"local_session"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	LastConnectedAt string            `json:"last_connected_at,omitempty"`
}

type sandboxMetricsResponse struct {
	SandboxID string                  `json:"sandbox_id"`
	Metrics   []sandboxMetricResponse `json:"metrics"`
}

type sandboxMetricResponse struct {
	Timestamp     string  `json:"timestamp"`
	TimestampUnix int64   `json:"timestamp_unix"`
	CPUCount      int32   `json:"cpu_count"`
	CPUUsedPct    float32 `json:"cpu_used_pct"`
	MemTotal      int64   `json:"mem_total"`
	MemUsed       int64   `json:"mem_used"`
	DiskTotal     int64   `json:"disk_total"`
	DiskUsed      int64   `json:"disk_used"`
}

func (ctrl *Ctrl) SandboxSessions(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	region := c.Query("region")
	remoteSandboxes, err := ctrl.sandboxRuntime.ListSandboxes(c.Request.Context(), apiKey, region)
	if err != nil {
		return err
	}
	sessions, err := ctrl.service.ListSandboxSessions(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	sessionsBySandboxID := make(map[string]entity.SandboxSession, len(sessions))
	for _, session := range sessions {
		sessionsBySandboxID[session.SandboxID] = session
	}
	out := make([]sandboxSessionResponse, 0, len(remoteSandboxes))
	for _, sandbox := range remoteSandboxes {
		local, ok := sessionsBySandboxID[sandbox.SandboxID]
		out = append(out, ctrl.listedSandboxSessionResponse(c.Request, accountID, sandbox, local, ok, region))
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
		Endpoint:        req.Region,
		Metadata: sandboxMetadata("standalone", map[string]string{
			"template_id": req.TemplateID,
		}),
	})
	if err != nil {
		return err
	}
	session, err := ctrl.service.SaveSandboxSession(c.Request.Context(), accountID, service.SandboxSessionInput{
		SandboxID:  info.SandboxID,
		TemplateID: info.TemplateID,
		State:      info.State,
		Endpoint:   info.Endpoint,
		Region:     req.Region,
		Metadata: sandboxMetadata("standalone", map[string]string{
			"template_id": req.TemplateID,
		}),
	})
	if err != nil {
		return err
	}
	return ctrl.sandboxSessionResponse(c.Request, *session)
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
	var req connectSandboxRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return httperrors.New(http.StatusBadRequest, "invalid request body")
		}
	}
	var endpoint string
	if session, err := ctrl.service.SandboxSession(c.Request.Context(), accountID, sandboxID); err == nil {
		endpoint = session.Region
	}
	if req.Region != "" {
		endpoint = req.Region
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
	return ctrl.sandboxSessionResponse(c.Request, *session)
}

func (ctrl *Ctrl) SandboxMetrics(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	sandboxID := c.Param("sandboxID")
	if sandboxID == "" {
		return httperrors.New(http.StatusBadRequest, "sandbox id is required")
	}
	session, err := ctrl.service.SandboxSession(c.Request.Context(), accountID, sandboxID)
	if err != nil {
		return err
	}
	params, err := sandboxMetricsQuery(c.Request)
	if err != nil {
		return err
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	metrics, err := ctrl.sandboxRuntime.GetMetrics(c.Request.Context(), apiKey, sandboxID, session.Region, params)
	if err != nil {
		return err
	}
	out := make([]sandboxMetricResponse, 0, len(metrics))
	for _, metric := range metrics {
		timestamp := ""
		if !metric.Timestamp.IsZero() {
			timestamp = metric.Timestamp.Format(time.RFC3339)
		}
		out = append(out, sandboxMetricResponse{
			Timestamp:     timestamp,
			TimestampUnix: metric.TimestampUnix,
			CPUCount:      metric.CPUCount,
			CPUUsedPct:    metric.CPUUsedPct,
			MemTotal:      metric.MemTotal,
			MemUsed:       metric.MemUsed,
			DiskTotal:     metric.DiskTotal,
			DiskUsed:      metric.DiskUsed,
		})
	}
	return sandboxMetricsResponse{SandboxID: sandboxID, Metrics: out}
}

func (ctrl *Ctrl) qiniuAPIKey(c *fox.Context, accountID string) (string, error) {
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return "", err
	}
	return credentials.SandboxAPIKey, nil
}

type qiniuRuntimeCredentialSet struct {
	SandboxAPIKey string
	MAASAPIKey    string
}

func (ctrl *Ctrl) qiniuRuntimeCredentials(c *fox.Context, accountID string) (qiniuRuntimeCredentialSet, error) {
	credential, err := ctrl.service.QiniuCredential(c.Request.Context(), accountID)
	if err != nil {
		return qiniuRuntimeCredentialSet{}, httperrors.New(http.StatusPreconditionRequired, "Qiniu API key is not configured")
	}
	apiKey, err := ctrl.credentialBox.Decrypt(credential.EncryptedAPIKey)
	if err != nil {
		return qiniuRuntimeCredentialSet{}, err
	}
	var maasAPIKey string
	if credential.EncryptedMAASAPIKey != "" {
		maasAPIKey, err = ctrl.credentialBox.Decrypt(credential.EncryptedMAASAPIKey)
		if err != nil {
			return qiniuRuntimeCredentialSet{}, err
		}
	}
	return qiniuRuntimeCredentialSet{SandboxAPIKey: apiKey, MAASAPIKey: maasAPIKey}, nil
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

func (ctrl *Ctrl) sandboxSessionResponse(req *http.Request, session entity.SandboxSession) sandboxSessionResponse {
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
		IDEURL:        ctrl.ideProxyURL(req, session.AccountID, session.SandboxID, session.IDEURL),
		LocalSession:  true,
		Metadata:      map[string]string(session.Metadata),
	}
	if session.LastConnectedAt != nil {
		out.LastConnectedAt = session.LastConnectedAt.Format(time.RFC3339)
	}
	return out
}

func (ctrl *Ctrl) listedSandboxSessionResponse(req *http.Request, accountID string, sandbox sandboxRuntimeListedSandbox, local entity.SandboxSession, hasLocal bool, region string) sandboxSessionResponse {
	metadata := make(map[string]string)
	for key, value := range sandbox.Metadata {
		metadata[key] = value
	}
	if hasLocal {
		for key, value := range local.Metadata {
			metadata[key] = value
		}
		if region == "" {
			region = local.Region
		}
	}
	memoryGB := int32(0)
	if sandbox.MemoryMB > 0 {
		memoryGB = (sandbox.MemoryMB + 1023) / 1024
	}
	out := sandboxSessionResponse{
		ID:              sandbox.SandboxID,
		SandboxID:       sandbox.SandboxID,
		TemplateID:      sandbox.TemplateID,
		State:           sandbox.State,
		Region:          region,
		CPUCount:        sandbox.CPUCount,
		MemoryGB:        memoryGB,
		DiskSizeMB:      sandbox.DiskSizeMB,
		LocalSession:    hasLocal,
		Metadata:        metadata,
		LastConnectedAt: "",
	}
	if hasLocal {
		out.ID = local.ID
		out.Endpoint = local.Endpoint
		out.GitHubRepoID = local.GitHubRepoID
		out.RepoFullName = local.RepoFullName
		out.WorkspacePath = local.WorkspacePath
		out.IDEURL = ctrl.ideProxyURL(req, accountID, sandbox.SandboxID, local.IDEURL)
		if local.LastConnectedAt != nil {
			out.LastConnectedAt = local.LastConnectedAt.Format(time.RFC3339)
		}
	}
	return out
}

func sandboxMetricsQuery(req *http.Request) (sandboxMetricsParams, error) {
	query := req.URL.Query()
	start, err := optionalInt64Query(query.Get("start"), "start")
	if err != nil {
		return sandboxMetricsParams{}, err
	}
	end, err := optionalInt64Query(query.Get("end"), "end")
	if err != nil {
		return sandboxMetricsParams{}, err
	}
	if start != nil && end != nil && *start > *end {
		return sandboxMetricsParams{}, httperrors.New(http.StatusBadRequest, "start must be before end")
	}
	return sandboxMetricsParams{Start: start, End: end}, nil
}

func optionalInt64Query(value, name string) (*int64, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return nil, httperrors.New(http.StatusBadRequest, name+" must be a non-negative unix timestamp")
	}
	return &parsed, nil
}

func sandboxMetadata(kind string, values map[string]string) map[string]string {
	metadata := map[string]string{
		"created_by": "qiniu-playground",
		"kind":       kind,
	}
	for key, value := range values {
		if value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func workspaceSandboxMetadata(workspaceID, name string, githubRepoID *int64, repoFullName, region, workspacePath string) map[string]string {
	values := map[string]string{
		"workspace_id":   workspaceID,
		"workspace_name": name,
		"repo_full_name": repoFullName,
		"region":         region,
		"workspace_path": workspacePath,
	}
	if githubRepoID != nil {
		values["github_repo_id"] = strconv.FormatInt(*githubRepoID, 10)
	}
	return sandboxMetadata("workspace", values)
}
