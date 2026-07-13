package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qiniu/go-sdk/v7/reqid"
	qiniusb "github.com/qiniu/go-sdk/v7/sandbox"

	"github.com/miclle/qiniu-playground/internal/config"
)

type sandboxRuntime interface {
	ListTemplates(ctx context.Context, apiKey, endpoint string) ([]sandboxRuntimeTemplate, error)
	ListSandboxes(ctx context.Context, apiKey, endpoint string) ([]sandboxRuntimeListedSandbox, error)
	Create(ctx context.Context, apiKey string, req sandboxRuntimeCreateRequest) (*sandboxRuntimeInfo, error)
	Connect(ctx context.Context, apiKey, sandboxID string, timeoutSeconds int32, endpoint string) (*sandboxRuntimeInfo, error)
	PrepareWorkspace(ctx context.Context, apiKey string, req sandboxRuntimeWorkspaceRequest) (*sandboxRuntimeWorkspace, error)
	PrepareRepository(ctx context.Context, apiKey string, req sandboxRuntimeRepositoryRequest) (*sandboxRuntimeWorkspace, error)
	StartPTY(ctx context.Context, apiKey, sandboxID string, endpoint string, size sandboxPTYSize, onData func([]byte)) (sandboxPTYSession, error)
	ListFiles(ctx context.Context, apiKey, sandboxID, endpoint, filePath string, depth uint32) ([]sandboxRuntimeFileEntry, error)
	ReadFileStream(ctx context.Context, apiKey, sandboxID, endpoint, filePath string) (io.ReadCloser, error)
	GetMetrics(ctx context.Context, apiKey, sandboxID, endpoint string, params sandboxMetricsParams) ([]sandboxRuntimeMetric, error)
	RunAIChat(ctx context.Context, apiKey, sandboxID, endpoint string, req sandboxRuntimeAIChatRequest) (*sandboxRuntimeAIChatResult, error)
	SetTimeout(ctx context.Context, apiKey, sandboxID, endpoint string, timeoutSeconds int32) error
	Pause(ctx context.Context, apiKey, sandboxID, endpoint string) error
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

type sandboxRuntimeListedSandbox struct {
	SandboxID  string
	TemplateID string
	State      string
	CPUCount   int32
	MemoryMB   int32
	DiskSizeMB int32
	Metadata   map[string]string
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
	Envs           map[string]string
}

type sandboxRuntimeWorkspaceRequest struct {
	SandboxID      string
	TimeoutSeconds int32
	Endpoint       string
	WorkspacePath  string
	IDEPassword    string
	Envs           map[string]string
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

type sandboxRuntimeAIChatRequest struct {
	WorkspacePath string
	Prompt        string
	Envs          map[string]string
	Timeout       time.Duration
	OnOutput      func(string)
}

type sandboxRuntimeAIChatResult struct {
	Provider string
	Stdout   string
	Stderr   string
	Error    string
	Thought  string
	ExitCode int
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

func (r *qiniuSandboxRuntime) ListSandboxes(ctx context.Context, apiKey, endpoint string) ([]sandboxRuntimeListedSandbox, error) {
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
	sandboxes, err := client.List(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]sandboxRuntimeListedSandbox, 0, len(sandboxes))
	for _, sandbox := range sandboxes {
		var metadata map[string]string
		if sandbox.Metadata != nil {
			metadata = map[string]string(*sandbox.Metadata)
		}
		out = append(out, sandboxRuntimeListedSandbox{
			SandboxID:  sandbox.SandboxID,
			TemplateID: sandbox.TemplateID,
			State:      string(sandbox.State),
			CPUCount:   sandbox.CPUCount,
			MemoryMB:   sandbox.MemoryMB,
			DiskSizeMB: sandbox.DiskSizeMB,
			Metadata:   metadata,
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

func (r *qiniuSandboxRuntime) SetTimeout(ctx context.Context, apiKey, sandboxID, endpoint string, timeoutSeconds int32) error {
	body, err := json.Marshal(map[string]int32{"timeout": timeoutSeconds})
	if err != nil {
		return err
	}
	return r.doSandboxControlRequest(ctx, apiKey, endpoint, sandboxID, "timeout", bytes.NewReader(body), "application/json")
}

func (r *qiniuSandboxRuntime) Pause(ctx context.Context, apiKey, sandboxID, endpoint string) error {
	return r.doSandboxControlRequest(ctx, apiKey, endpoint, sandboxID, "pause", nil, "")
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
	if err := prepareRuntimeEnvironment(ctx, sb, req.Envs); err != nil {
		return nil, err
	}
	if _, err := sb.Commands().Run(ctx, "mkdir -p "+shellQuote(workspacePath), qiniusb.WithTimeout(time.Minute)); err != nil {
		return nil, fmt.Errorf("prepare workspace: %w", err)
	}
	if _, err := sb.Commands().Start(ctx, codeServerCommand(workspacePath, req.IDEPassword), qiniusb.WithTag("code-server"), qiniusb.WithEnvs(req.Envs)); err != nil {
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
	if err := prepareRuntimeEnvironment(ctx, sb, req.Envs); err != nil {
		return nil, err
	}
	if err := cloneOrUpdateRepository(ctx, sb, req, workspacePath); err != nil {
		return nil, err
	}
	if _, err := sb.Commands().Start(ctx, codeServerCommand(workspacePath, req.IDEPassword), qiniusb.WithTag("code-server"), qiniusb.WithEnvs(req.Envs)); err != nil {
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
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, config.DefaultSandboxTimeoutSeconds)
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
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, config.DefaultSandboxTimeoutSeconds)
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
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, config.DefaultSandboxTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	return sb.Files().ReadStream(ctx, filePath)
}

func (r *qiniuSandboxRuntime) GetMetrics(ctx context.Context, apiKey, sandboxID, endpoint string, params sandboxMetricsParams) ([]sandboxRuntimeMetric, error) {
	return r.getSandboxMetrics(ctx, apiKey, sandboxID, endpoint, params)
}

func (r *qiniuSandboxRuntime) RunAIChat(ctx context.Context, apiKey, sandboxID, endpoint string, req sandboxRuntimeAIChatRequest) (*sandboxRuntimeAIChatResult, error) {
	sb, err := r.connectSandbox(ctx, apiKey, sandboxID, endpoint, config.DefaultSandboxTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	envs := map[string]string{
		"QINIU_PLAYGROUND_CHAT_PROMPT":         req.Prompt,
		"QINIU_PLAYGROUND_CHAT_WORKSPACE_PATH": req.WorkspacePath,
	}
	for key, value := range req.Envs {
		if value != "" {
			envs[key] = value
		}
	}
	command := aiChatCommand()
	outputFilter := newAIChatOutputFilter(req.OnOutput)
	result, err := sb.Commands().Run(
		ctx,
		command,
		qiniusb.WithTimeout(timeout),
		qiniusb.WithEnvs(envs),
		qiniusb.WithOnStdout(func(data []byte) {
			outputFilter.WriteStdout(data)
		}),
		qiniusb.WithOnStderr(func(data []byte) {
			outputFilter.WriteStderr(data)
		}),
	)
	outputFilter.Flush()
	if err != nil {
		return nil, err
	}
	provider := aiChatProviderFromOutput(result.Stdout)
	return &sandboxRuntimeAIChatResult{
		Provider: provider,
		Stdout:   stripAIChatProviderMarker(result.Stdout),
		Stderr:   result.Stderr,
		Error:    result.Error,
		ExitCode: result.ExitCode,
	}, nil
}

type aiChatOutputFilter struct {
	mu              sync.Mutex
	onOutput        func(string)
	providerChecked bool
	prefix          []byte
	stdoutPending   []byte
	stderrPending   []byte
}

func newAIChatOutputFilter(onOutput func(string)) *aiChatOutputFilter {
	return &aiChatOutputFilter{onOutput: onOutput}
}

func (f *aiChatOutputFilter) WriteStdout(chunk []byte) {
	if f.onOutput == nil || len(chunk) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.providerChecked {
		f.emitStdout(chunk)
		return
	}
	f.prefix = append(f.prefix, chunk...)
	const maxAIChatProviderMarkerLen = 64
	if len(f.prefix) >= maxAIChatProviderMarkerLen &&
		!bytes.HasPrefix(bytes.TrimSpace(f.prefix), []byte("__qiniu_playground_provider__:")) {
		f.providerChecked = true
		f.emitStdout(f.prefix)
		f.prefix = nil
		return
	}
	index := bytes.IndexByte(f.prefix, '\n')
	if index < 0 {
		return
	}
	f.providerChecked = true
	line := append([]byte(nil), f.prefix[:index]...)
	rest := append([]byte(nil), f.prefix[index+1:]...)
	f.prefix = nil
	if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("__qiniu_playground_provider__:")) {
		line = append(line, '\n')
		f.emitStdout(line)
	}
	if len(rest) > 0 {
		f.emitStdout(rest)
	}
}

func (f *aiChatOutputFilter) WriteStderr(chunk []byte) {
	if f.onOutput == nil || len(chunk) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emitStderr(chunk)
}

func (f *aiChatOutputFilter) Flush() {
	if f.onOutput == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.providerChecked && len(f.prefix) > 0 {
		f.providerChecked = true
		line := f.prefix
		f.prefix = nil
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("__qiniu_playground_provider__:")) {
			f.emitStdout(line)
		}
	}
	if len(f.stdoutPending) > 0 {
		f.onOutput(string(f.stdoutPending))
		f.stdoutPending = nil
	}
	if len(f.stderrPending) > 0 {
		f.onOutput(string(f.stderrPending))
		f.stderrPending = nil
	}
}

func (f *aiChatOutputFilter) emitStdout(chunk []byte) {
	f.emitUTF8(chunk, &f.stdoutPending)
}

func (f *aiChatOutputFilter) emitStderr(chunk []byte) {
	f.emitUTF8(chunk, &f.stderrPending)
}

func (f *aiChatOutputFilter) emitUTF8(chunk []byte, pending *[]byte) {
	data := append(*pending, chunk...)
	complete, rest := splitCompleteUTF8(data)
	if len(complete) > 0 {
		f.onOutput(string(complete))
	}
	*pending = append((*pending)[:0], rest...)
}

func splitCompleteUTF8(data []byte) ([]byte, []byte) {
	if len(data) == 0 {
		return nil, nil
	}
	for offset := 1; offset <= 3; offset++ {
		index := len(data) - offset
		if index < 0 {
			break
		}
		value := data[index]
		if value >= 0xC0 {
			expectedLen := 2
			if value >= 0xF0 {
				expectedLen = 4
			} else if value >= 0xE0 {
				expectedLen = 3
			}
			if offset < expectedLen {
				return data[:index], data[index:]
			}
			break
		}
		if value < 0x80 {
			break
		}
	}
	return data, nil
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
	metricsURL, err := sandboxControlURL(endpoint, sandboxID, "metrics")
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

func (r *qiniuSandboxRuntime) doSandboxControlRequest(ctx context.Context, apiKey, endpoint, sandboxID, action string, body io.Reader, contentType string) error {
	if endpoint == "" {
		endpoint = r.endpoint
	}
	requestURL, err := sandboxControlURL(endpoint, sandboxID, action)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if id, ok := reqid.ReqidFromContext(ctx); ok {
		req.Header.Set("X-Reqid", id)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := sandboxMetricsHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &sandboxMetricsAPIError{StatusCode: resp.StatusCode, Body: responseBody, Reqid: resp.Header.Get("X-Reqid")}
	}
	return nil
}

func sandboxControlURL(endpoint, sandboxID, action string) (*url.URL, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	return baseURL.Parse("./sandboxes/" + url.PathEscape(sandboxID) + "/" + strings.TrimLeft(action, "/"))
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

func prepareRuntimeEnvironment(ctx context.Context, sb *qiniusb.Sandbox, envs map[string]string) error {
	if len(envs) == 0 {
		return nil
	}
	command := strings.Join([]string{
		"set -e",
		"mkdir -p ~/.config/qiniu-playground",
		": > ~/.config/qiniu-playground/env",
		shellExportCommands(envs, "~/.config/qiniu-playground/env"),
		"chmod 600 ~/.config/qiniu-playground/env",
		`grep -Fq 'qiniu-playground/env' ~/.profile 2>/dev/null || printf '\n[ -f "$HOME/.config/qiniu-playground/env" ] && . "$HOME/.config/qiniu-playground/env"\n' >> ~/.profile`,
	}, "\n")
	if _, err := sb.Commands().Run(ctx, command, qiniusb.WithTimeout(time.Minute), qiniusb.WithEnvs(envs)); err != nil {
		return fmt.Errorf("prepare runtime environment: %w", err)
	}
	return nil
}

func shellExportCommands(envs map[string]string, targetPath string) string {
	names := make([]string, 0, len(envs))
	for name := range envs {
		if envs[name] == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	pattern := strings.Join(names, "|")
	return fmt.Sprintf("export -p | grep -E '^(export |declare -x )?(%s)=' > %s", pattern, targetPath)
}

func aiChatCommand() string {
	return strings.Join([]string{
		"set -u",
		`workspace_path="${QINIU_PLAYGROUND_CHAT_WORKSPACE_PATH:-}"`,
		`if [ -n "$workspace_path" ] && [ -d "$workspace_path" ]; then cd "$workspace_path"; elif [ -d /home/user ]; then cd /home/user; elif [ -d /workspace ]; then cd /workspace; fi`,
		"if command -v claude >/dev/null 2>&1; then",
		"printf '__qiniu_playground_provider__:claude\\n'",
		`claude --print --bare --dangerously-skip-permissions -- "$QINIU_PLAYGROUND_CHAT_PROMPT" 2>&1`,
		"elif command -v codex >/dev/null 2>&1; then",
		"printf '__qiniu_playground_provider__:codex\\n'",
		`codex exec --skip-git-repo-check -- "$QINIU_PLAYGROUND_CHAT_PROMPT" 2>&1`,
		"else",
		`printf 'Neither claude nor codex CLI is available in this sandbox.\n'`,
		"exit 127",
		"fi",
	}, "\n")
}

func aiChatProviderFromOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		switch strings.TrimSpace(line) {
		case "__qiniu_playground_provider__:codex":
			return "codex"
		case "__qiniu_playground_provider__:claude":
			return "claude"
		}
	}
	return ""
}

func stripAIChatProviderMarker(output string) string {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "__qiniu_playground_provider__:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
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
