package handler

import (
	"testing"
)

func TestIDEProxyURLUsesOwnedSandboxPath(t *testing.T) {
	ctrl := newTestController(t)
	got := ctrl.ideProxyURL("sandbox-1", "https://sandbox.example.test")
	want := "/api/v1/sandboxes/sandbox-1/ide/"
	if got != want {
		t.Fatalf("ideProxyURL() = %q, want %q", got, want)
	}
	if got := ctrl.ideProxyURL("sandbox-1", ""); got != "" {
		t.Fatalf("ideProxyURL() = %q, want empty when code-server is unavailable", got)
	}
}

func TestSandboxIDEHostRequiresHTTPSURL(t *testing.T) {
	if _, err := sandboxIDEHost("http://sandbox.example.test"); err == nil {
		t.Fatal("sandboxIDEHost should reject non-HTTPS URLs")
	}
	got, err := sandboxIDEHost("https://sandbox.example.test/path")
	if err != nil {
		t.Fatalf("sandboxIDEHost returned error: %v", err)
	}
	if got != "sandbox.example.test" {
		t.Fatalf("sandboxIDEHost() = %q, want sandbox.example.test", got)
	}
}
