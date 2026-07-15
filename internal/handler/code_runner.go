package handler

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/internal/service"
)

const (
	defaultCodeRunTimeout                 = 30 * time.Second
	maxCodeRunTimeout                     = 2 * time.Minute
	codeRunnerProvisionTimeout            = 2 * time.Minute
	codeRunnerWriteTimeout                = 10 * time.Second
	codeRunnerSandboxTimeoutSeconds int32 = 30 * 60
	codeRunnerCleanupLifetime             = 60
	maxCodeRunnerSessionName              = 100
)

var codeRunnerSessionNamePattern = regexp.MustCompile(`^[A-Za-z0-9 _.-]+$`)

type codeRunnerSessionsResponse struct {
	Sessions []codeRunnerSessionResponse `json:"sessions"`
}

type codeRunnerLatestRunResponse struct {
	Language   string    `json:"language"`
	Succeeded  bool      `json:"succeeded"`
	DurationMS int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type codeRunnerHeartbeatResponse struct {
	OK             bool  `json:"ok"`
	TimeoutSeconds int32 `json:"timeout_seconds"`
}

type codeRunnerSessionRequest struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

type runCodeRequest struct {
	Language       string `json:"language"`
	Code           string `json:"code"`
	Stdin          string `json:"stdin"`
	TimeoutSeconds int32  `json:"timeout_seconds"`
}

type codeRunnerSessionResponse struct {
	ID            string                       `json:"id"`
	CreatedAt     time.Time                    `json:"created_at"`
	UpdatedAt     time.Time                    `json:"updated_at"`
	Name          string                       `json:"name"`
	Region        string                       `json:"region"`
	SandboxID     string                       `json:"sandbox_id,omitempty"`
	TemplateID    string                       `json:"template_id"`
	State         string                       `json:"state,omitempty"`
	Endpoint      string                       `json:"endpoint,omitempty"`
	WorkspacePath string                       `json:"workspace_path,omitempty"`
	LatestRun     *codeRunnerLatestRunResponse `json:"latest_run,omitempty"`
}

type codeRunsResponse struct {
	Runs []codeRunResponse `json:"runs"`
}

type codeRunResponse struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	SessionID  string    `json:"session_id"`
	SandboxID  string    `json:"sandbox_id,omitempty"`
	Language   string    `json:"language"`
	Code       string    `json:"code"`
	Stdin      string    `json:"stdin,omitempty"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	Error      string    `json:"error"`
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
}

func (ctrl *Ctrl) CodeRunnerSessions(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	sessions, err := ctrl.service.ListCodeRunnerSessions(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return codeRunnerSessionsResponse{Sessions: []codeRunnerSessionResponse{}}
	}
	latestRuns, err := ctrl.service.LatestCodeRuns(c.Request.Context(), accountID)
	if err != nil {
		return err
	}
	out := make([]codeRunnerSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		response := codeRunnerSessionResponseFromEntity(session)
		if latestRun, ok := latestRuns[session.ID]; ok {
			response.LatestRun = &codeRunnerLatestRunResponse{
				Language:   latestRun.Language,
				Succeeded:  latestRun.ExitCode == 0 && latestRun.Error == "",
				DurationMS: latestRun.DurationMS,
				CreatedAt:  latestRun.CreatedAt,
			}
		}
		out = append(out, response)
	}
	return codeRunnerSessionsResponse{Sessions: out}
}

func (ctrl *Ctrl) CreateCodeRunnerSession(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	var req codeRunnerSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return httperrors.New(http.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) > maxCodeRunnerSessionName {
		return httperrors.New(http.StatusBadRequest, "session name must be 100 characters or less")
	}
	if req.Name == "." || req.Name == ".." || (req.Name != "" && !codeRunnerSessionNamePattern.MatchString(req.Name)) {
		return httperrors.New(http.StatusBadRequest, "session name contains invalid characters")
	}
	req.Region = strings.TrimSpace(req.Region)
	if req.Region == "" {
		return httperrors.New(http.StatusBadRequest, "region is required")
	}
	if !isSupportedCodeRunnerRegion(req.Region) {
		return httperrors.New(http.StatusBadRequest, "unsupported code runner region")
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	name := workspaceName(req.Name, "code-runner")
	metadata := sandboxMetadata("code-runner", map[string]string{
		"session_name": name,
		"region":       req.Region,
	})
	info, err := ctrl.sandboxRuntime.Create(c.Request.Context(), credentials.SandboxAPIKey, sandboxRuntimeCreateRequest{
		TemplateID:      ctrl.codeInterpreterTemplateID,
		TimeoutSeconds:  codeRunnerSandboxTimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
		Endpoint:        req.Region,
		Metadata:        metadata,
	})
	if err != nil {
		return err
	}
	workspacePath := "/workspace/" + safeWorkspaceName(name)
	provisionCtx, cancelProvision := codeRunnerProvisionContext(c.Request.Context())
	defer cancelProvision()
	runtimeWorkspace, err := ctrl.sandboxRuntime.PrepareWorkspace(provisionCtx, credentials.SandboxAPIKey, sandboxRuntimeWorkspaceRequest{
		SandboxID:      info.SandboxID,
		TimeoutSeconds: codeRunnerSandboxTimeoutSeconds,
		Endpoint:       req.Region,
		WorkspacePath:  workspacePath,
		IDEPassword:    ctrl.codeServerPassword(info.SandboxID),
	})
	if err != nil {
		ctrl.shortenCodeRunnerSandboxLifetime(credentials.SandboxAPIKey, info.SandboxID, req.Region)
		return err
	}
	writeCtx, cancelWrite := codeRunnerWriteContext(c.Request.Context())
	defer cancelWrite()
	session, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(writeCtx, accountID, service.CodeRunnerSessionInput{
		Name:          name,
		Region:        req.Region,
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    runtimeWorkspace.TemplateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
	}, service.SandboxSessionInput{
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    runtimeWorkspace.TemplateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
		Region:        req.Region,
		Metadata: sandboxMetadata("code-runner", map[string]string{
			"session_name":   name,
			"workspace_path": runtimeWorkspace.WorkspacePath,
		}),
	})
	if err != nil {
		ctrl.shortenCodeRunnerSandboxLifetime(credentials.SandboxAPIKey, runtimeWorkspace.SandboxID, req.Region)
		return err
	}
	return codeRunnerSessionResponseFromEntity(*session)
}

func (ctrl *Ctrl) ConnectCodeRunnerSession(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	session, err := ctrl.codeRunnerSessionFromRequest(c, accountID)
	if err != nil {
		return err
	}
	if session.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "code runner sandbox is not connected")
	}
	if !isSupportedCodeRunnerRegion(session.Region) {
		return httperrors.New(http.StatusBadRequest, "unsupported code runner region")
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	info, err := ctrl.sandboxRuntime.Connect(c.Request.Context(), credentials.SandboxAPIKey, session.SandboxID, codeRunnerSandboxTimeoutSeconds, session.Region)
	if err != nil {
		if isSandboxNotFoundError(err) {
			return httperrors.New(http.StatusConflict, "code runner sandbox no longer exists")
		}
		return err
	}
	workspacePath := codeRunnerWorkspacePath(session)
	provisionCtx, cancelProvision := codeRunnerProvisionContext(c.Request.Context())
	defer cancelProvision()
	runtimeWorkspace, err := ctrl.sandboxRuntime.PrepareWorkspace(provisionCtx, credentials.SandboxAPIKey, sandboxRuntimeWorkspaceRequest{
		SandboxID:      info.SandboxID,
		TimeoutSeconds: codeRunnerSandboxTimeoutSeconds,
		Endpoint:       session.Region,
		WorkspacePath:  workspacePath,
		IDEPassword:    ctrl.codeServerPassword(info.SandboxID),
	})
	if err != nil {
		return err
	}
	writeCtx, cancelWrite := codeRunnerWriteContext(c.Request.Context())
	defer cancelWrite()
	updated, err := ctrl.service.SaveCodeRunnerSessionWithSandbox(writeCtx, accountID, service.CodeRunnerSessionInput{
		ID:            session.ID,
		Name:          session.Name,
		Region:        session.Region,
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    runtimeWorkspace.TemplateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
	}, service.SandboxSessionInput{
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    runtimeWorkspace.TemplateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
		Region:        session.Region,
		Metadata: sandboxMetadata("code-runner", map[string]string{
			"session_name":   session.Name,
			"workspace_path": runtimeWorkspace.WorkspacePath,
		}),
	})
	if err != nil {
		return err
	}
	return codeRunnerSessionResponseFromEntity(*updated)
}

func (ctrl *Ctrl) CodeRunnerSessionHeartbeat(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	session, err := ctrl.codeRunnerSessionFromRequest(c, accountID)
	if err != nil {
		return err
	}
	if session.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "code runner sandbox is not connected")
	}
	if !isSupportedCodeRunnerRegion(session.Region) {
		return httperrors.New(http.StatusBadRequest, "unsupported code runner region")
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	if err := ctrl.sandboxRuntime.SetTimeout(
		c.Request.Context(),
		credentials.SandboxAPIKey,
		session.SandboxID,
		session.Region,
		codeRunnerSandboxTimeoutSeconds,
	); err != nil {
		if isSandboxNotFoundError(err) {
			return httperrors.New(http.StatusConflict, "code runner sandbox no longer exists")
		}
		return err
	}
	return codeRunnerHeartbeatResponse{OK: true, TimeoutSeconds: codeRunnerSandboxTimeoutSeconds}
}

func (ctrl *Ctrl) KillCodeRunnerSession(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	session, err := ctrl.codeRunnerSessionFromRequest(c, accountID)
	if err != nil {
		return err
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	for range 2 {
		if session.State == "killed" {
			return codeRunnerSessionResponseFromEntity(*session)
		}
		if session.SandboxID == "" {
			return httperrors.New(http.StatusPreconditionRequired, "code runner sandbox is not connected")
		}
		if !isSupportedCodeRunnerRegion(session.Region) {
			return httperrors.New(http.StatusBadRequest, "unsupported code runner region")
		}
		killCtx, cancelKill := codeRunnerWriteContext(c.Request.Context())
		killErr := ctrl.sandboxRuntime.Kill(killCtx, credentials.SandboxAPIKey, session.SandboxID, session.Region)
		cancelKill()
		if killErr != nil && !isSandboxNotFoundError(killErr) {
			return killErr
		}
		killedSandboxInput := service.SandboxSessionInput{
			SandboxID:     session.SandboxID,
			TemplateID:    session.TemplateID,
			State:         "killed",
			Endpoint:      session.Endpoint,
			WorkspacePath: session.WorkspacePath,
			Region:        session.Region,
			Metadata: sandboxMetadata("code-runner", map[string]string{
				"session_id":     session.ID,
				"session_name":   session.Name,
				"workspace_path": session.WorkspacePath,
			}),
		}
		writeCtx, cancelWrite := codeRunnerWriteContext(c.Request.Context())
		updated, updateErr := ctrl.service.UpdateCodeRunnerSessionWithSandboxIfCurrent(writeCtx, accountID, service.CodeRunnerSessionCondition{
			SandboxID: session.SandboxID,
			State:     session.State,
		}, service.CodeRunnerSessionInput{
			ID:            session.ID,
			Name:          session.Name,
			Region:        session.Region,
			SandboxID:     session.SandboxID,
			TemplateID:    session.TemplateID,
			State:         "killed",
			Endpoint:      session.Endpoint,
			WorkspacePath: session.WorkspacePath,
		}, killedSandboxInput)
		cancelWrite()
		if updateErr == nil {
			return codeRunnerSessionResponseFromEntity(*updated)
		}
		if !errors.Is(updateErr, service.ErrCodeRunnerSessionChanged) {
			return updateErr
		}
		staleWriteCtx, cancelStaleWrite := codeRunnerWriteContext(c.Request.Context())
		_, staleWriteErr := ctrl.service.SaveSandboxSession(staleWriteCtx, accountID, killedSandboxInput)
		cancelStaleWrite()
		if staleWriteErr != nil {
			return staleWriteErr
		}
		session, err = ctrl.service.CodeRunnerSession(c.Request.Context(), accountID, session.ID)
		if err != nil {
			return httperrors.New(http.StatusNotFound, "code runner session not found")
		}
	}
	return httperrors.New(http.StatusConflict, "code runner session changed while stopping sandbox")
}

func (ctrl *Ctrl) CodeRuns(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	session, err := ctrl.codeRunnerSessionFromRequest(c, accountID)
	if err != nil {
		return err
	}
	runs, err := ctrl.service.ListCodeRuns(c.Request.Context(), accountID, session.ID, 50)
	if err != nil {
		return err
	}
	out := make([]codeRunResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, codeRunResponseFromEntity(run))
	}
	return codeRunsResponse{Runs: out}
}

func (ctrl *Ctrl) RunCode(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	session, err := ctrl.codeRunnerSessionFromRequest(c, accountID)
	if err != nil {
		return err
	}
	if session.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "code runner sandbox is not connected")
	}
	if !isSupportedCodeRunnerRegion(session.Region) {
		return httperrors.New(http.StatusBadRequest, "unsupported code runner region")
	}
	var req runCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return httperrors.New(http.StatusBadRequest, "invalid request body")
	}
	req.Language = strings.TrimSpace(req.Language)
	if req.Language == "" {
		req.Language = service.CodeRunnerLanguagePython
	}
	if !service.IsSupportedCodeRunnerLanguage(req.Language) {
		return httperrors.New(http.StatusBadRequest, "unsupported language")
	}
	if strings.TrimSpace(req.Code) == "" {
		return httperrors.New(http.StatusBadRequest, "code is required")
	}
	if req.TimeoutSeconds > int32(maxCodeRunTimeout/time.Second) {
		return httperrors.New(http.StatusBadRequest, "timeout_seconds must be 120 or less")
	}
	timeout := defaultCodeRunTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	credentials, err := ctrl.qiniuRuntimeCredentials(c, accountID)
	if err != nil {
		return err
	}
	activeSession := session
	recreated := false
	if session.State == "killed" {
		activeSession, err = ctrl.recreateCodeRunnerSandbox(c.Request.Context(), accountID, credentials.SandboxAPIKey, session)
		if err != nil {
			return err
		}
		recreated = true
	}
	runInSession := func(current *entity.CodeRunnerSession) (*sandboxRuntimeCommandResult, int64, error) {
		startedAt := time.Now()
		result, runErr := ctrl.sandboxRuntime.RunCommand(c.Request.Context(), credentials.SandboxAPIKey, current.SandboxID, current.Region, sandboxRuntimeCommandRequest{
			WorkspacePath:         codeRunnerWorkspacePath(current),
			Language:              req.Language,
			Code:                  req.Code,
			Stdin:                 req.Stdin,
			Timeout:               timeout,
			SandboxTimeoutSeconds: codeRunnerSandboxTimeoutSeconds,
		})
		durationMS := time.Since(startedAt).Milliseconds()
		var refreshErr error
		if runErr == nil || !isSandboxNotFoundError(runErr) {
			refreshCtx, cancelRefresh := codeRunnerWriteContext(c.Request.Context())
			refreshErr = ctrl.sandboxRuntime.SetTimeout(
				refreshCtx,
				credentials.SandboxAPIKey,
				current.SandboxID,
				current.Region,
				codeRunnerSandboxTimeoutSeconds,
			)
			cancelRefresh()
		}
		if refreshErr != nil {
			c.Logger.Warnf("refresh code runner sandbox timeout: %v", refreshErr)
		}
		return result, durationMS, runErr
	}
	result, durationMS, err := runInSession(activeSession)
	if err != nil && !recreated && isSandboxNotFoundError(err) {
		current, loadErr := ctrl.service.CodeRunnerSession(c.Request.Context(), accountID, session.ID)
		if loadErr != nil {
			return httperrors.New(http.StatusNotFound, "code runner session not found")
		}
		if current.SandboxID != session.SandboxID || current.State != session.State {
			return httperrors.New(http.StatusConflict, "code runner session changed while restoring sandbox")
		}
		activeSession, err = ctrl.recreateCodeRunnerSandbox(c.Request.Context(), accountID, credentials.SandboxAPIKey, current)
		if err != nil {
			return err
		}
		result, durationMS, err = runInSession(activeSession)
	}
	if err != nil {
		return err
	}
	writeCtx, cancelWrite := codeRunnerWriteContext(c.Request.Context())
	defer cancelWrite()
	run, err := ctrl.service.SaveCodeRun(writeCtx, accountID, service.CodeRunInput{
		SessionID:  session.ID,
		SandboxID:  activeSession.SandboxID,
		Language:   req.Language,
		Code:       req.Code,
		Stdin:      req.Stdin,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Error:      result.Error,
		ExitCode:   result.ExitCode,
		DurationMS: durationMS,
	})
	if err != nil {
		return err
	}
	return codeRunResponseFromEntity(*run)
}

func (ctrl *Ctrl) recreateCodeRunnerSandbox(
	ctx context.Context,
	accountID string,
	apiKey string,
	session *entity.CodeRunnerSession,
) (*entity.CodeRunnerSession, error) {
	metadata := sandboxMetadata("code-runner", map[string]string{
		"session_name": session.Name,
		"region":       session.Region,
	})
	info, err := ctrl.sandboxRuntime.Create(ctx, apiKey, sandboxRuntimeCreateRequest{
		TemplateID:      session.TemplateID,
		TimeoutSeconds:  codeRunnerSandboxTimeoutSeconds,
		PollingInterval: defaultSandboxPollInterval,
		Endpoint:        session.Region,
		Metadata:        metadata,
	})
	if err != nil {
		return nil, err
	}
	workspacePath := codeRunnerWorkspacePath(session)
	provisionCtx, cancelProvision := codeRunnerProvisionContext(ctx)
	defer cancelProvision()
	runtimeWorkspace, err := ctrl.sandboxRuntime.PrepareWorkspace(provisionCtx, apiKey, sandboxRuntimeWorkspaceRequest{
		SandboxID:      info.SandboxID,
		TimeoutSeconds: codeRunnerSandboxTimeoutSeconds,
		Endpoint:       session.Region,
		WorkspacePath:  workspacePath,
		IDEPassword:    ctrl.codeServerPassword(info.SandboxID),
	})
	if err != nil {
		ctrl.shortenCodeRunnerSandboxLifetime(apiKey, info.SandboxID, session.Region)
		return nil, err
	}
	writeCtx, cancelWrite := codeRunnerWriteContext(ctx)
	defer cancelWrite()
	updated, err := ctrl.service.UpdateCodeRunnerSessionWithSandboxIfCurrent(writeCtx, accountID, service.CodeRunnerSessionCondition{
		SandboxID: session.SandboxID,
		State:     session.State,
	}, service.CodeRunnerSessionInput{
		ID:            session.ID,
		Name:          session.Name,
		Region:        session.Region,
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    runtimeWorkspace.TemplateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
	}, service.SandboxSessionInput{
		SandboxID:     runtimeWorkspace.SandboxID,
		TemplateID:    runtimeWorkspace.TemplateID,
		State:         runtimeWorkspace.State,
		Endpoint:      runtimeWorkspace.Endpoint,
		WorkspacePath: runtimeWorkspace.WorkspacePath,
		Region:        session.Region,
		Metadata: sandboxMetadata("code-runner", map[string]string{
			"session_name":   session.Name,
			"workspace_path": runtimeWorkspace.WorkspacePath,
		}),
	})
	if err != nil {
		ctrl.shortenCodeRunnerSandboxLifetime(apiKey, runtimeWorkspace.SandboxID, session.Region)
		if errors.Is(err, service.ErrCodeRunnerSessionChanged) {
			return nil, httperrors.New(http.StatusConflict, "code runner session changed while restoring sandbox")
		}
		return nil, err
	}
	return updated, nil
}

func (ctrl *Ctrl) shortenCodeRunnerSandboxLifetime(apiKey, sandboxID, region string) {
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), codeRunnerWriteTimeout)
	defer cancelCleanup()
	_ = ctrl.sandboxRuntime.SetTimeout(cleanupCtx, apiKey, sandboxID, region, codeRunnerCleanupLifetime)
}

func isSupportedCodeRunnerRegion(region string) bool {
	switch region {
	case "https://cn-yangzhou-1-sandbox.qiniuapi.com", "https://us-south-1-sandbox.qiniuapi.com":
		return true
	default:
		return false
	}
}

func codeRunnerWriteContext(requestContext context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(requestContext), codeRunnerWriteTimeout)
}

func codeRunnerProvisionContext(requestContext context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(requestContext), codeRunnerProvisionTimeout)
}

func (ctrl *Ctrl) codeRunnerSessionFromRequest(c *fox.Context, accountID string) (*entity.CodeRunnerSession, error) {
	sessionID := c.Param("sessionID")
	if sessionID == "" {
		return nil, httperrors.New(http.StatusBadRequest, "session id is required")
	}
	session, err := ctrl.service.CodeRunnerSession(c.Request.Context(), accountID, sessionID)
	if err != nil {
		return nil, httperrors.New(http.StatusNotFound, "code runner session not found")
	}
	return session, nil
}

func codeRunnerWorkspacePath(session *entity.CodeRunnerSession) string {
	if session.WorkspacePath != "" {
		return session.WorkspacePath
	}
	return "/workspace/" + safeWorkspaceName(session.Name)
}

func codeRunnerSessionResponseFromEntity(session entity.CodeRunnerSession) codeRunnerSessionResponse {
	return codeRunnerSessionResponse{
		ID:            session.ID,
		CreatedAt:     session.CreatedAt,
		UpdatedAt:     session.UpdatedAt,
		Name:          session.Name,
		Region:        session.Region,
		SandboxID:     session.SandboxID,
		TemplateID:    session.TemplateID,
		State:         session.State,
		Endpoint:      session.Endpoint,
		WorkspacePath: session.WorkspacePath,
	}
}

func codeRunResponseFromEntity(run entity.CodeRun) codeRunResponse {
	return codeRunResponse{
		ID:         run.ID,
		CreatedAt:  run.CreatedAt,
		SessionID:  run.SessionID,
		SandboxID:  run.SandboxID,
		Language:   run.Language,
		Code:       run.Code,
		Stdin:      run.Stdin,
		Stdout:     run.Stdout,
		Stderr:     run.Stderr,
		Error:      run.Error,
		ExitCode:   run.ExitCode,
		DurationMS: run.DurationMS,
	}
}
