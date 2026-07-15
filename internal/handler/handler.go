// Package handler provides HTTP handlers and route registration.
package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/qiniu-playground/internal/config"
	"github.com/miclle/qiniu-playground/internal/service"
	"github.com/miclle/qiniu-playground/pkg/cryptobox"
	"github.com/miclle/qiniu-playground/website"
)

// Ctrl is the controller that holds service dependencies and registers routes.
type Ctrl struct {
	service                      *service.Service
	githubOAuth                  githubOAuthClient
	githubApp                    githubAppClient
	githubAppSlug                string
	sessionSigner                sessionSigner
	credentialBox                *cryptobox.Box
	sandboxRuntime               sandboxRuntime
	defaultSandboxTemplateID     string
	codeInterpreterTemplateID    string
	defaultSandboxTimeoutSeconds int32
}

// New creates a new Ctrl instance.
func New(svc *service.Service, cfg *config.Config) *Ctrl {
	box, err := cryptobox.New(cfg.Auth.EncryptionKey)
	if err != nil {
		panic(err)
	}
	templateID := cfg.Sandbox.DefaultTemplateID
	if templateID == "" {
		templateID = "base"
	}
	codeInterpreterTemplateID := cfg.Sandbox.CodeInterpreterTemplateID
	if codeInterpreterTemplateID == "" {
		codeInterpreterTemplateID = "code-interpreter-v1"
	}
	timeoutSeconds := cfg.Sandbox.DefaultTimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = config.DefaultSandboxTimeoutSeconds
	}
	return &Ctrl{
		service:                      svc,
		githubOAuth:                  newGitHubOAuthClient(cfg.GitHub),
		githubApp:                    newGitHubAppClient(cfg.GitHub),
		githubAppSlug:                cfg.GitHub.AppSlug,
		sessionSigner:                newSessionSigner(cfg.Auth.SessionSecret),
		credentialBox:                box,
		sandboxRuntime:               newSandboxRuntime(cfg.Sandbox),
		defaultSandboxTemplateID:     templateID,
		codeInterpreterTemplateID:    codeInterpreterTemplateID,
		defaultSandboxTimeoutSeconds: timeoutSeconds,
	}
}

// RegisterRoutes registers all API routes on the given engine.
func (ctrl *Ctrl) RegisterRoutes(r *fox.Engine) {
	// embed website assets
	website.EmbedAssets(r)

	// ── Health check ────────────────────────────────────────────────────
	r.GET("/health", ctrl.Health)

	// ── API routes ──────────────────────────────────────────────────────
	api := r.Group("/api/v1")
	api.GET("/hello", ctrl.Hello)
	api.GET("/auth/github/login", ctrl.GitHubLogin)
	api.GET("/auth/github/callback", ctrl.GitHubCallback)
	api.GET("/auth/me", ctrl.Me)
	api.POST("/auth/logout", ctrl.Logout)
	api.GET("/github/app/install", ctrl.GitHubAppInstall)
	api.GET("/github/app/callback", ctrl.GitHubAppCallback)
	api.GET("/github/installations", ctrl.GitHubInstallations)
	api.GET("/github/repositories", ctrl.GitHubRepositories)
	api.GET("/workspaces", ctrl.Workspaces)
	api.POST("/workspaces", ctrl.CreateWorkspace)
	api.POST("/workspaces/:workspaceID/connect", ctrl.ConnectWorkspace)
	api.POST("/workspaces/:workspaceID/heartbeat", ctrl.WorkspaceHeartbeat)
	api.POST("/workspaces/:workspaceID/pause", ctrl.PauseWorkspaceSandbox)
	api.GET("/workspaces/:workspaceID/preview/*previewPath", ctrl.WorkspaceFilePreview)
	api.GET("/workspaces/:workspaceID/chat/messages", ctrl.WorkspaceChatMessages)
	api.POST("/workspaces/:workspaceID/chat/messages", ctrl.SendWorkspaceChatMessage)
	api.GET("/code-runner/sessions", ctrl.CodeRunnerSessions)
	api.POST("/code-runner/sessions", ctrl.CreateCodeRunnerSession)
	api.POST("/code-runner/sessions/:sessionID/connect", ctrl.ConnectCodeRunnerSession)
	api.POST("/code-runner/sessions/:sessionID/heartbeat", ctrl.CodeRunnerSessionHeartbeat)
	api.POST("/code-runner/sessions/:sessionID/kill", ctrl.KillCodeRunnerSession)
	api.GET("/code-runner/sessions/:sessionID/runs", ctrl.CodeRuns)
	api.POST("/code-runner/sessions/:sessionID/runs", ctrl.RunCode)
	api.POST("/repositories/:repositoryID/open", ctrl.OpenRepository)
	api.GET("/qiniu/credentials", ctrl.QiniuCredentialStatus)
	api.PUT("/qiniu/credentials", ctrl.SaveQiniuCredential)
	api.DELETE("/qiniu/credentials", ctrl.DeleteQiniuCredential)
	api.GET("/templates", ctrl.SandboxTemplates)
	api.GET("/sandboxes", ctrl.SandboxSessions)
	api.POST("/sandboxes", ctrl.CreateSandbox)
	api.Any("/sandboxes/:sandboxID/ide/*proxyPath", ctrl.SandboxIDEProxy)
	api.GET("/sandboxes/:sandboxID/preview/*previewPath", ctrl.SandboxFilePreview)
	api.GET("/sandboxes/:sandboxID/filesystem", ctrl.SandboxFiles)
	api.GET("/sandboxes/:sandboxID/filesystem/content", ctrl.SandboxFileContent)
	api.GET("/sandboxes/:sandboxID/metrics", ctrl.SandboxMetrics)
	api.GET("/sandboxes/:sandboxID/pty", ctrl.SandboxPTY)
	api.POST("/sandboxes/:sandboxID/connect", ctrl.ConnectSandbox)
}

// Health returns a simple health check response.
func (ctrl *Ctrl) Health(c *fox.Context) string {
	return "ok"
}

// Hello returns a greeting message.
func (ctrl *Ctrl) Hello(c *fox.Context) any {
	return map[string]string{"message": "Qiniu Playground API is ready."}
}
