package handler

import (
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
