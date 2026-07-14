package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShellQuotePreventsExpansion(t *testing.T) {
	input := "/workspace/repo$(touch injected)' branch"
	got := shellQuote(input)
	want := "'/workspace/repo$(touch injected)'\\'' branch'"
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func TestCodeServerCommandDisablesChatByDefault(t *testing.T) {
	got := codeServerCommand("/workspace/repo", "ide-password")
	for _, want := range []string{
		"if ! pgrep -x code-server >/dev/null; then",
		"mkdir -p ~/.local/share/code-server/User",
		"if [ ! -f ~/.local/share/code-server/User/settings.json ]; then",
		`"chat.disableAIFeatures": true`,
		`"breadcrumbs.enabled": false`,
		`"editor.minimap.enabled": false`,
		`"extensions.ignoreRecommendations": true`,
		`"security.workspace.trust.enabled": false`,
		`"workbench.commandCenter": false`,
		`"workbench.secondarySideBar.defaultVisibility": "hidden"`,
		`"workbench.startupEditor": "none"`,
		`"workbench.statusBar.visible": false`,
		"fi",
		"PASSWORD=",
		"ide-password",
		"code-server --bind-addr 0.0.0.0:8080 --auth password",
		"/workspace/repo",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("codeServerCommand() missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "pkill -x code-server") {
		t.Fatalf("codeServerCommand() should not kill an active IDE session: %q", got)
	}
	if strings.Contains(got, "--auth none") {
		t.Fatalf("codeServerCommand() should not leave the upstream IDE unauthenticated: %q", got)
	}
	for _, reject := range []string{"state.vscdb", "workspaceStorage"} {
		if strings.Contains(got, reject) {
			t.Fatalf("codeServerCommand() should preserve VS Code session state and not remove %q: %q", reject, got)
		}
	}
}

func TestCloneOrUpdateRepositoryCommandPreservesExistingClone(t *testing.T) {
	got := cloneOrUpdateRepositoryCommand(sandboxRuntimeRepositoryRequest{
		FullName:      "octocat/hello-world",
		DefaultBranch: "main",
		Token:         "token",
	}, "/workspace/octocat-hello-world")

	for _, reject := range []string{"fetch --all", "checkout ", "pull --ff-only"} {
		if strings.Contains(got, reject) {
			t.Fatalf("clone command should not run %q for existing clones: %q", reject, got)
		}
	}
	for _, want := range []string{
		"if [ -d '/workspace/octocat-hello-world/.git' ]; then",
		"remote set-url origin",
		"remote add origin",
		"clone --branch 'main'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("clone command missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "|| true") {
		t.Fatalf("clone command should not ignore remote configuration failures: %q", got)
	}
}

func TestShellExportCommandsDoNotEmbedSecretValues(t *testing.T) {
	got := shellExportCommands(map[string]string{
		"ANTHROPIC_AUTH_TOKEN": "maas-secret-value",
		"ANTHROPIC_BASE_URL":   "https://api.qnaigc.com",
		"QINIU_MAAS_API_KEY":   "maas-secret-value",
	}, "~/.config/qiniu-playground/env")

	if strings.Contains(got, "maas-secret-value") {
		t.Fatalf("shellExportCommands() should not embed secret values: %q", got)
	}
	want := "export -p | grep -E '^(export |declare -x )?(ANTHROPIC_AUTH_TOKEN|ANTHROPIC_BASE_URL|QINIU_MAAS_API_KEY)=' > ~/.config/qiniu-playground/env"
	if got != want {
		t.Fatalf("shellExportCommands() = %q, want %q", got, want)
	}
}

func TestAIChatCommandPrefersClaudeThenCodexAndFallsBackToExistingDirectory(t *testing.T) {
	got := aiChatCommand()
	for _, want := range []string{
		`workspace_path="${QINIU_PLAYGROUND_CHAT_WORKSPACE_PATH:-}"`,
		`cd "$workspace_path"`,
		"elif [ -d /home/user ]; then cd /home/user",
		"command -v claude",
		`claude --print --bare --dangerously-skip-permissions -- "$QINIU_PLAYGROUND_CHAT_PROMPT" 2>&1`,
		"command -v codex",
		`codex exec --skip-git-repo-check -- "$QINIU_PLAYGROUND_CHAT_PROMPT" 2>&1`,
		"Neither claude nor codex CLI is available",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("aiChatCommand() missing %q in %q", want, got)
		}
	}
}

func TestCodeRunnerCommandUsesEnvEncodedPythonScript(t *testing.T) {
	got := codeRunnerCommand("python")
	for _, want := range []string{
		`workspace="${QINIU_PLAYGROUND_CODE_WORKSPACE:-}"`,
		`fallback="${HOME:-/tmp}/qiniu-playground/$(basename "$workspace")"`,
		`printf '%s' "$QINIU_PLAYGROUND_CODE_B64" | base64 -d > "$tmpdir/main.py"`,
		`printf '%s' "$QINIU_PLAYGROUND_STDIN_B64" | base64 -d > "$tmpdir/stdin.txt"`,
		`python3 "$tmpdir/main.py" < "$tmpdir/stdin.txt"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("codeRunnerCommand() missing %q in %q", want, got)
		}
	}
	for _, reject := range []string{"$QINIU_PLAYGROUND_CODE ", "eval", "python3 -c"} {
		if strings.Contains(got, reject) {
			t.Fatalf("codeRunnerCommand() should not execute user code through %q: %q", reject, got)
		}
	}
}

func TestCodeRunnerCommandSupportsE2BLanguages(t *testing.T) {
	tests := []struct {
		language string
		file     string
		command  string
	}{
		{"python", "main.py", `python3 "$tmpdir/main.py"`},
		{"javascript", "main.js", `node "$tmpdir/main.js"`},
		{"typescript", "main.ts", `tsx "$tmpdir/main.ts"`},
		{"r", "main.R", `Rscript "$tmpdir/main.R"`},
		{"java", "Main.java", `java "$tmpdir/Main.java"`},
		{"bash", "main.sh", `bash "$tmpdir/main.sh"`},
	}
	for _, tt := range tests {
		got := codeRunnerCommand(tt.language)
		if !strings.Contains(got, `base64 -d > "$tmpdir/`+tt.file+`"`) {
			t.Fatalf("codeRunnerCommand(%q) missing file %q in %q", tt.language, tt.file, got)
		}
		if !strings.Contains(got, tt.command) {
			t.Fatalf("codeRunnerCommand(%q) missing command %q in %q", tt.language, tt.command, got)
		}
	}
}

func TestCodeRunnerCommandErrorReturnsTimeoutResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done()

	result, err := codeRunnerCommandError(ctx, context.DeadlineExceeded)
	if err != nil {
		t.Fatalf("codeRunnerCommandError() error = %v, want nil", err)
	}
	if result == nil || result.Error != "Execution timed out" || result.ExitCode != -1 {
		t.Fatalf("result = %+v, want timeout result", result)
	}
}

func TestStripAIChatProviderMarker(t *testing.T) {
	got := stripAIChatProviderMarker("__qiniu_playground_provider__:codex\nhello\n")
	if got != "hello" {
		t.Fatalf("stripAIChatProviderMarker() = %q, want hello", got)
	}
	if provider := aiChatProviderFromOutput("__qiniu_playground_provider__:claude\nhello\n"); provider != "claude" {
		t.Fatalf("provider = %q, want claude", provider)
	}
}

func TestAIChatOutputFilterPreservesUTF8AcrossChunks(t *testing.T) {
	var got strings.Builder
	filter := newAIChatOutputFilter(func(delta string) {
		got.WriteString(delta)
	})
	message := []byte("你好，世界")

	filter.WriteStdout([]byte("__qiniu_playground_provider__:codex\n"))
	filter.WriteStdout(message[:1])
	filter.WriteStdout(message[1:5])
	filter.WriteStdout(message[5:])
	filter.Flush()

	if got.String() != "你好，世界" {
		t.Fatalf("streamed output = %q, want UTF-8 preserved", got.String())
	}
}

func TestAIChatOutputFilterFlushesOutputWithoutProviderNewline(t *testing.T) {
	var got strings.Builder
	filter := newAIChatOutputFilter(func(delta string) {
		got.WriteString(delta)
	})

	filter.WriteStdout([]byte("short answer"))
	filter.Flush()

	if got.String() != "short answer" {
		t.Fatalf("streamed output = %q, want un-newline-terminated output", got.String())
	}
}

func TestAIChatOutputFilterStreamsLongFirstLineWithoutProviderMarker(t *testing.T) {
	var got strings.Builder
	filter := newAIChatOutputFilter(func(delta string) {
		got.WriteString(delta)
	})

	filter.WriteStdout([]byte(strings.Repeat("a", 64)))

	if got.String() != strings.Repeat("a", 64) {
		t.Fatalf("streamed output = %q, want long first line emitted without waiting for newline", got.String())
	}
}

func TestAIChatOutputFilterPreservesTrailingUTF8AfterInvalidBytes(t *testing.T) {
	var got []byte
	filter := newAIChatOutputFilter(func(delta string) {
		got = append(got, []byte(delta)...)
	})
	message := []byte("你")

	filter.WriteStdout([]byte("__qiniu_playground_provider__:codex\n"))
	filter.WriteStdout(append([]byte{0xff}, message[:1]...))
	filter.WriteStdout(message[1:])
	filter.Flush()

	want := append([]byte{0xff}, message...)
	if !bytes.Equal(got, want) {
		t.Fatalf("streamed bytes = %v, want invalid prefix plus complete UTF-8 bytes %v", got, want)
	}
}

func TestGetSandboxMetricsUsesReadOnlyAPI(t *testing.T) {
	var gotPath, gotStart, gotEnd, gotAPIKey, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotStart = req.URL.Query().Get("start")
		gotEnd = req.URL.Query().Get("end")
		gotAPIKey = req.Header.Get("X-API-Key")
		gotAuthorization = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"cpu_count": 4,
			"cpu_used_pct": 12.5,
			"mem_total": 1024,
			"mem_used": 512,
			"disk_total": 2048,
			"disk_used": 256,
			"timestamp_unix": 1780000000
		}]`))
	}))
	defer server.Close()

	start := int64(1780000000)
	end := int64(1780000600)
	runtime := &qiniuSandboxRuntime{}
	metrics, err := runtime.GetMetrics(context.Background(), "api-key", "sandbox-2", server.URL, sandboxMetricsParams{
		Start: &start,
		End:   &end,
	})
	if err != nil {
		t.Fatalf("GetMetrics() error = %v", err)
	}
	if gotPath != "/sandboxes/sandbox-2/metrics" {
		t.Fatalf("path = %q, want metrics read API path", gotPath)
	}
	if gotStart != "1780000000" || gotEnd != "1780000600" {
		t.Fatalf("query = start:%q end:%q, want requested range", gotStart, gotEnd)
	}
	if gotAPIKey != "api-key" || gotAuthorization != "Bearer api-key" {
		t.Fatalf("auth headers = X-API-Key:%q Authorization:%q", gotAPIKey, gotAuthorization)
	}
	if len(metrics) != 1 || metrics[0].CPUUsedPct != 12.5 || metrics[0].TimestampUnix != 1780000000 {
		t.Fatalf("metrics = %+v, want decoded response", metrics)
	}
}
