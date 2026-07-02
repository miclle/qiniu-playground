package handler

import "testing"

func TestShellQuotePreventsExpansion(t *testing.T) {
	input := "/workspace/repo$(touch injected)' branch"
	got := shellQuote(input)
	want := "'/workspace/repo$(touch injected)'\\'' branch'"
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}
