//go:build development

package website

import "testing"

func TestDevServerURLFromEnvironment(t *testing.T) {
	t.Setenv("QINIU_PLAYGROUND_VITE_DEV_SERVER_URL", "http://127.0.0.1:3100")
	t.Setenv("QINIU_PLAYGROUND_VITE_PORT", "3101")

	got := devServerURLFromEnvironment()

	if got != "http://127.0.0.1:3100" {
		t.Fatalf("dev server URL = %q, want explicit URL", got)
	}
}

func TestDevServerURLFallsBackToConfiguredPort(t *testing.T) {
	t.Setenv("QINIU_PLAYGROUND_VITE_PORT", "3101")

	got := devServerURLFromEnvironment()

	if got != "http://localhost:3101" {
		t.Fatalf("dev server URL = %q, want URL from configured port", got)
	}
}

func TestDevServerURLDefaultsToVitePort(t *testing.T) {
	got := devServerURLFromEnvironment()

	if got != "http://localhost:19173" {
		t.Fatalf("dev server URL = %q, want default Vite URL", got)
	}
}
