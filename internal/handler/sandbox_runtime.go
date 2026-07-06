package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/qiniu/go-sdk/v7/reqid"
	qiniusb "github.com/qiniu/go-sdk/v7/sandbox"

	"github.com/miclle/qiniu-playground/internal/config"
)

type sandboxRuntime interface {
	ListTemplates(ctx context.Context, apiKey, endpoint string) ([]sandboxRuntimeTemplate, error)
	Create(ctx context.Context, apiKey string, req sandboxRuntimeCreateRequest) (*sandboxRuntimeInfo, error)
	Connect(ctx context.Context, apiKey, sandboxID string, timeoutSeconds int32, endpoint string) (*sandboxRuntimeInfo, error)
	PrepareWorkspace(ctx context.Context, apiKey string, req sandboxRuntimeWorkspaceRequest) (*sandboxRuntimeWorkspace, error)
	PrepareRepository(ctx context.Context, apiKey string, req sandboxRuntimeRepositoryRequest) (*sandboxRuntimeWorkspace, error)
	StartPTY(ctx context.Context, apiKey, sandboxID string, endpoint string, size sandboxPTYSize, onData func([]byte)) (sandboxPTYSession, error)
	ListFiles(ctx context.Context, apiKey, sandboxID, endpoint, filePath string, depth uint32) ([]sandboxRuntimeFileEntry, error)
	ReadFileStream(ctx context.Context, apiKey, sandboxID, endpoint, filePath string) (io.ReadCloser, error)
	GetMetrics(ctx context.Context, apiKey, sandboxID, endpoint string, params sandboxMetricsParams) ([]sandboxRuntimeMetric, error)
}

type sandboxRuntimeTemplate struct {
	TemplateID  string
	Aliases     []string
	BuildStatus string
	CPUCount    int32
	MemoryMB    int32
	DiskSizeMB  int32
	Public      bool
}

type sandboxRuntimeCreateRequest struct {
	TemplateID      string
	TimeoutSeconds  int32
	PollingInterval time.Duration
	Endpoint        string
	Metadata        map[string]string
}

type sandboxRuntimeInfo struct {
	SandboxID  string
	TemplateID string
	State      string
	Endpoint   string
}

type sandboxRuntimeRepositoryRequest struct {
	SandboxID      string
	TimeoutSeconds int32
	Endpoint       string
	FullName       string
	DefaultBranch  string
	Token          string
	WorkspacePath  string
	IDEPassword    string
}

type sandboxRuntimeWorkspaceRequest struct {
	SandboxID      string
	TimeoutSeconds int32
	Endpoint       string
	WorkspacePath  string
	IDEPassword    string
}

type sandboxRuntimeWorkspace struct {
	SandboxID     string
	TemplateID    string
	State         string
	Endpoint      string
	WorkspacePath string
	IDEURL        string
}

type sandboxPTYSize struct {
	Cols uint32
	Rows uint32
}

type sandboxRuntimeFileEntry struct {
	Name          string
	Type          string
	Path          string
	Size          int64
	Mode          uint32
	Permissions   string
	Owner         string
	Group         string
	ModifiedTime  time.Time
	SymlinkTarget *string
}

type sandboxMetricsParams struct {
	Start *int64
	End   *int64
}

type sandboxRuntimeMetric struct {
	CPUCount      int32     `json:"cpu_count"`
	CPUUsedPct    float32   `json:"cpu_used_pct"`
	MemTotal      int64     `json:"mem_total"`
	MemUsed       int64     `json:"mem_used"`
	DiskTotal     int64     `json:"disk_total"`
	DiskUsed      int64     `json:"disk_used"`
	Timestamp     time.Time `json:"timestamp"`
	TimestampUnix int64     `json:"timestamp_unix"`
}

type sandboxMetricsAPIError struct {
	StatusCode int
	Body       []byte
	Reqid      string
}

func (e *sandboxMetricsAPIError) Error() string {
	prefix := fmt.Sprintf("api error: status %d", e.StatusCode)
	if e.Reqid != "" {
		prefix += ", reqid: " + e.Reqid
	}
	if len(e.Body) > 0 {
		return prefix + ", body: " + string(e.Body)
	}
	return prefix
}

type sandboxPTYSession interface {
	Send(ctx context.Context, data []byte) error
	Resize(ctx context.Context, size sandboxPTYSize) error
	Close(ctx context.Context) error
}

type qiniuPTYSession struct {
	sandbox *qiniusb.Sandbox
	handle  *qiniusb.CommandHandle
}

type qiniuSandboxRuntime struct {
	endpoint string
}

var sandboxMetricsHTTPClient = &http.Client{Timeout: 15 * time.Second}

func newSandboxRuntime(cfg config.SandboxConfig) sandboxRuntime {
	return &qiniuSandboxRuntime{endpoint: cfg.Endpoint}
}

func (r *qiniuSandboxRuntime) ListTemplates(ctx context.Context, apiKey, endpoint string) ([]sandboxRuntimeTemplate, error) {
	if endpoint == "" {
		endpoint = r.endpoint
	}
	client, err := qiniusb.NewClient(&qiniusb.Config{
		APIKey:   apiKey,
		Endpoint: endpoint,
	})
	if err != nil {
		return nil, err
	}
	templates, err := client.ListTemplates(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]sandboxRuntimeTemplate, 0, len(templates))
	for _, template := range templates {
		out = append(out, sandboxRuntimeTemplate{
			TemplateID:  template.TemplateID,
			Aliases:     template.Aliases,
			BuildStatus: string(template.BuildStatus),
			CPUCount:    template.CPUCount,
			MemoryMB:    template.MemoryMB,
			DiskSizeMB:  template.DiskSizeMB,
			Public:      template.Public,
		})
	}
	return out, nil
}

func (r *qiniuSandboxRuntime) Create(ctx context.Context, apiKey string, req sandboxRuntimeCreateRequest) (*sandboxRuntimeInfo, error) {
	endpoint := r.endpoint
	if req.Endpoint != "" {
		endpoint = req.Endpoint
	}
	client, err := qiniusb.NewClient(&qiniusb.Config{
		APIKey:   apiKey,
		Endpoint: endpoint,
	})
	if err != nil {
		return nil, err
	}
	timeout := req.TimeoutSeconds
	params := qiniusb.CreateParams{
		TemplateID: req.TemplateID,
		Timeout:    &timeout,
	}
	if len(req.Metadata) > 0 {
		metadata := qiniusb.Metadata(req.Metadata)
		params.Metadata = &metadata
	}
	sb, info, err := client.CreateAndWait(ctx, params, qiniusb.WithPollInterval(req.PollingInterval))
	if err != nil {
		return nil, err
	}
	return runtimeInfoFromSDK(sb, info), nil
}

func (r *qiniuSandboxRuntime) Connect(ctx context.Context, apiKey, sandboxID string, timeoutSeconds int32, endpoint string) (*sandboxRuntimeInfo, error) {
	if endpoint == "" {
		endpoint = r.endpoint
	}
	client, err := qiniusb.NewClient(&qiniusb.Config{
		APIKey:   apiKey,
		Endpoint: endpoint,
	})
	if err != nil {
		return nil, err
	}
	sb, err := client.Connect(ctx, sandboxID, qiniusb.ConnectParams{Timeout: timeoutSeconds})
	if err != nil {
		return nil, err
	}
	info, err := sb.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return runtimeInfoFromSDK(sb, info), nil
}

func (r *qiniuSandboxRuntime) PrepareWorkspace(ctx context.Context, apiKey string, req sandboxRuntimeWorkspaceRequest) (*sandboxRuntimeWorkspace, error) {
	endpoint := r.endpoint
	if req.Endpoint != "" {
		endpoint = req.Endpoint
	}
	client, err := qiniusb.NewClient(&qiniusb.Config{
		APIKey:   apiKey,
		Endpoint: endpoint,
	})
	if err != nil {
		return nil, err
	}
	sb, err := client.Connect(ctx, req.SandboxID, qiniusb.ConnectParams{Timeout: req.TimeoutSeconds})
	if err != nil {
		return nil, err
	}
	workspacePath := req.WorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace"
	}
	if _, err := sb.Commands().Run(ctx, "mkdir -p "+shellQuote(workspacePath), qiniusb.WithTimeout(time.Minute)); err != nil {
		return nil, fmt.Errorf("prepare workspace: %w", err)
	}
	if _, err := sb.Commands().Start(ctx, codeServerCommand(workspacePath, req.IDEPassword), qiniusb.WithTag("code-server")); err != nil {
		return nil, err
	}
	info, err := sb.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	runtimeInfo := runtimeInfoFromSDK(sb, info)
	return &sandboxRuntimeWorkspace{
		SandboxID:     runtimeInfo.SandboxID,
		TemplateID:    runtimeInfo.TemplateID,
		State:         runtimeInfo.State,
		Endpoint:      runtimeInfo.Endpoint,
		WorkspacePath: workspacePath,
		IDEURL:        "https://" + sb.GetHost(8080),
	}, nil
}

func (r *qiniuSandboxRuntime) PrepareRepository(ctx context.Context, apiKey string, req sandboxRuntimeRepositoryRequest) (*sandboxRuntimeWorkspace, error) {
	endpoint := r.endpoint
	if req.Endpoint != "" {
		endpoint = req.Endpoint
	}
	client, err := qiniusb.NewClient(&qiniusb.Config{
		APIKey:   apiKey,
		Endpoint: endpoint,
	})
	if err != nil {
		return nil, err
	}
	sb, err := client.Connect(ctx, req.SandboxID, qiniusb.ConnectParams{Timeout: req.TimeoutSeconds})
	if err != nil {
		return nil, err
	}
	workspacePath := req.WorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace/" + safeWorkspaceName(req.FullName)
	}
	if err := cloneOrUpdateRepository(ctx, sb, req, workspacePath); err != nil {
		return nil, err
	}
	if _, err := sb.Commands().Start(ctx, codeServerCommand(workspacePath, req.IDEPassword), qiniusb.WithTag("code-server")); err != nil {
		return nil, err
	}
	info, err := sb.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	runtimeInfo := runtimeInfoFromSDK(sb, info)
	return &sandboxRuntimeWorkspace{
		SandboxID:     runtimeInfo.SandboxID,
		TemplateID:    runtimeInfo.TemplateID,
		State:         runtimeInfo.State,
		Endpoint:      runtimeInfo.Endpoint,
		WorkspacePath: workspacePath,
		IDEURL:        "https://" + sb.GetHost(8080),
	}, nil
}

func (r *qiniuSandboxRuntime) StartPTY(ctx context.Context, apiKey, sandboxID string, endpoint string, size sandboxPTYSize, onData func([]byte)) (sandboxPTYSession, error) {
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, 120)
	if err != nil {
		return nil, err
	}
	handle, err := sb.Pty().Create(ctx, qiniusb.PtySize{Cols: size.Cols, Rows: size.Rows}, qiniusb.WithOnPtyData(onData))
	if err != nil {
		return nil, err
	}
	if _, err := handle.WaitPID(ctx); err != nil {
		return nil, err
	}
	return &qiniuPTYSession{sandbox: sb, handle: handle}, nil
}

func (r *qiniuSandboxRuntime) ListFiles(ctx context.Context, apiKey, sandboxID, endpoint, filePath string, depth uint32) ([]sandboxRuntimeFileEntry, error) {
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, 120)
	if err != nil {
		return nil, err
	}
	entries, err := sb.Files().List(ctx, filePath, qiniusb.WithDepth(depth))
	if err != nil {
		return nil, err
	}
	out := make([]sandboxRuntimeFileEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, sandboxRuntimeFileEntry{
			Name:          entry.Name,
			Type:          string(entry.Type),
			Path:          entry.Path,
			Size:          entry.Size,
			Mode:          entry.Mode,
			Permissions:   entry.Permissions,
			Owner:         entry.Owner,
			Group:         entry.Group,
			ModifiedTime:  entry.ModifiedTime,
			SymlinkTarget: entry.SymlinkTarget,
		})
	}
	return out, nil
}

func (r *qiniuSandboxRuntime) ReadFileStream(ctx context.Context, apiKey, sandboxID, endpoint, filePath string) (io.ReadCloser, error) {
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, 120)
	if err != nil {
		return nil, err
	}
	return sb.Files().ReadStream(ctx, filePath)
}

func (r *qiniuSandboxRuntime) GetMetrics(ctx context.Context, apiKey, sandboxID, endpoint string, params sandboxMetricsParams) ([]sandboxRuntimeMetric, error) {
	return r.getSandboxMetrics(ctx, apiKey, sandboxID, endpoint, params)
}

func (r *qiniuSandboxRuntime) getSandboxMetrics(ctx context.Context, apiKey, sandboxID, endpoint string, params sandboxMetricsParams) ([]sandboxRuntimeMetric, error) {
	if endpoint == "" {
		endpoint = r.endpoint
	}
	metricsURL, err := sandboxMetricsURL(endpoint, sandboxID, params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if id, ok := reqid.ReqidFromContext(ctx); ok {
		req.Header.Set("X-Reqid", id)
	}
	resp, err := sandboxMetricsHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &sandboxMetricsAPIError{StatusCode: resp.StatusCode, Body: body, Reqid: resp.Header.Get("X-Reqid")}
	}
	var metrics []sandboxRuntimeMetric
	if err := json.Unmarshal(body, &metrics); err != nil {
		return nil, err
	}
	return metrics, nil
}

func sandboxMetricsURL(endpoint, sandboxID string, params sandboxMetricsParams) (string, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	metricsURL, err := baseURL.Parse("./sandboxes/" + url.PathEscape(sandboxID) + "/metrics")
	if err != nil {
		return "", err
	}
	values := metricsURL.Query()
	if params.Start != nil {
		values.Set("start", strconv.FormatInt(*params.Start, 10))
	}
	if params.End != nil {
		values.Set("end", strconv.FormatInt(*params.End, 10))
	}
	metricsURL.RawQuery = values.Encode()
	return metricsURL.String(), nil
}

func (r *qiniuSandboxRuntime) connectSandbox(ctx context.Context, apiKey, sandboxID, endpoint string, timeoutSeconds int32) (*qiniusb.Sandbox, error) {
	if endpoint == "" {
		endpoint = r.endpoint
	}
	client, err := qiniusb.NewClient(&qiniusb.Config{
		APIKey:   apiKey,
		Endpoint: endpoint,
	})
	if err != nil {
		return nil, err
	}
	return client.Connect(ctx, sandboxID, qiniusb.ConnectParams{Timeout: timeoutSeconds})
}

func (s *qiniuPTYSession) Send(ctx context.Context, data []byte) error {
	return s.sandbox.Pty().SendInput(ctx, s.handle.PID(), data)
}

func (s *qiniuPTYSession) Resize(ctx context.Context, size sandboxPTYSize) error {
	return s.sandbox.Pty().Resize(ctx, s.handle.PID(), qiniusb.PtySize{Cols: size.Cols, Rows: size.Rows})
}

func (s *qiniuPTYSession) Close(ctx context.Context) error {
	return s.sandbox.Pty().Kill(ctx, s.handle.PID())
}

func runtimeInfoFromSDK(sb *qiniusb.Sandbox, info *qiniusb.SandboxInfo) *sandboxRuntimeInfo {
	out := &sandboxRuntimeInfo{
		SandboxID:  sb.ID(),
		TemplateID: sb.TemplateID(),
		State:      "running",
	}
	if info != nil {
		out.SandboxID = info.SandboxID
		out.TemplateID = info.TemplateID
		out.State = string(info.State)
	}
	if domain := sb.Domain(); domain != nil {
		out.Endpoint = *domain
	}
	return out
}

func cloneOrUpdateRepository(ctx context.Context, sb *qiniusb.Sandbox, req sandboxRuntimeRepositoryRequest, workspacePath string) error {
	command := cloneOrUpdateRepositoryCommand(req, workspacePath)
	_, err := sb.Commands().Run(ctx, "sh -lc "+shellQuote(command), qiniusb.WithTimeout(5*time.Minute))
	if err != nil {
		return fmt.Errorf("clone repository: %w", err)
	}
	return nil
}

func cloneOrUpdateRepositoryCommand(req sandboxRuntimeRepositoryRequest, workspacePath string) string {
	repositoryURL := githubRepositoryURL(req.FullName)
	authHeader := githubAuthHeader(req.Token)
	branch := req.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	command := strings.Join([]string{
		"set -e",
		"mkdir -p /workspace",
		"if [ -d " + shellQuote(workspacePath+"/.git") + " ]; then",
		"git -C " + shellQuote(workspacePath) + " remote set-url origin " + shellQuote(repositoryURL) + " 2>/dev/null || git -C " + shellQuote(workspacePath) + " remote add origin " + shellQuote(repositoryURL),
		"else",
		"git -c http.extraheader=" + shellQuote(authHeader) + " clone --branch " + shellQuote(branch) + " " + shellQuote(repositoryURL) + " " + shellQuote(workspacePath),
		"git -C " + shellQuote(workspacePath) + " remote set-url origin " + shellQuote(repositoryURL),
		"fi",
	}, "\n")
	return command
}

func githubRepositoryURL(fullName string) string {
	return "https://github.com/" + fullName + ".git"
}

func githubAuthHeader(token string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return "AUTHORIZATION: basic " + encoded
}

func safeWorkspaceName(fullName string) string {
	cleaned := strings.ReplaceAll(fullName, "/", "__")
	return path.Clean(cleaned)
}

func codeServerCommand(workspacePath, password string) string {
	if password == "" {
		password = "qiniu-playground"
	}
	settingsJSON := strings.Join([]string{
		"{",
		`  "chat.disableAIFeatures": true,`,
		`  "breadcrumbs.enabled": false,`,
		`  "editor.minimap.enabled": false,`,
		`  "extensions.ignoreRecommendations": true,`,
		`  "extensions.showRecommendationsOnlyOnDemand": true,`,
		`  "security.workspace.trust.enabled": false,`,
		`  "security.workspace.trust.startupPrompt": "never",`,
		`  "telemetry.telemetryLevel": "off",`,
		`  "workbench.activityBar.visible": true,`,
		`  "workbench.commandCenter": false,`,
		`  "workbench.editor.empty.hint": "hidden",`,
		`  "workbench.layoutControl.enabled": false,`,
		`  "workbench.secondarySideBar.defaultVisibility": "hidden",`,
		`  "workbench.sideBar.location": "left",`,
		`  "workbench.startupEditor": "none",`,
		`  "workbench.statusBar.visible": false,`,
		`  "workbench.tips.enabled": false,`,
		`  "workbench.welcomePage.walkthroughs.openOnInstall": false,`,
		`  "workbench.editorAssociations": { "*.md": "default" }`,
		"}",
	}, "\n")
	command := strings.Join([]string{
		"if ! pgrep -x code-server >/dev/null; then",
		"mkdir -p ~/.local/share/code-server/User",
		"if [ ! -f ~/.local/share/code-server/User/settings.json ]; then",
		"cat > ~/.local/share/code-server/User/settings.json <<'JSON'",
		settingsJSON,
		"JSON",
		"fi",
		"PASSWORD=" + shellQuote(password) + " code-server --bind-addr 0.0.0.0:8080 --auth password " + shellQuote(workspacePath),
		"fi",
	}, "\n")
	return "sh -lc " + shellQuote(command)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
