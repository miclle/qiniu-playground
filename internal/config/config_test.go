package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadDefaultsDriver(t *testing.T) {
	path := writeConfig(t, `addr: "127.0.0.1:19090"
dsn: "host=localhost port=5432 user=postgres password=postgres dbname=app sslmode=disable"
auth:
  session_secret: "test-session-secret"
  encryption_key: "test-encryption-key"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Driver != "postgres" {
		t.Fatalf("Driver = %q, want postgres", cfg.Driver)
	}
}

func TestLoadSupportsMySQL(t *testing.T) {
	path := writeConfig(t, `addr: "127.0.0.1:19090"
driver: mysql
dsn: "root:password@tcp(localhost:3306)/app?charset=utf8mb4&parseTime=True&loc=Local"
auth:
  session_secret: "test-session-secret"
  encryption_key: "test-encryption-key"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Driver != "mysql" {
		t.Fatalf("Driver = %q, want mysql", cfg.Driver)
	}
}

func TestLoadRejectsUnsupportedDriver(t *testing.T) {
	path := writeConfig(t, `addr: "127.0.0.1:19090"
driver: sqlite
dsn: "app.db"
auth:
  session_secret: "test-session-secret"
  encryption_key: "test-encryption-key"
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load should reject unsupported driver")
	}
}

func TestLoadExpandsEnvironmentWithFallback(t *testing.T) {
	t.Setenv("QINIU_PLAYGROUND_TEST_DSN", "host=db user=app password=secret dbname=app sslmode=disable")
	path := writeConfig(t, `addr: "${QINIU_PLAYGROUND_TEST_ADDR:-127.0.0.1:19090}"
dsn: "${QINIU_PLAYGROUND_TEST_DSN:-fallback}"
auth:
  session_secret: "test-session-secret"
  encryption_key: "test-encryption-key"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Addr != "127.0.0.1:19090" {
		t.Fatalf("Addr = %q, want fallback", cfg.Addr)
	}
	if cfg.DSN != "host=db user=app password=secret dbname=app sslmode=disable" {
		t.Fatalf("DSN = %q, want environment value", cfg.DSN)
	}
}

func TestLoadRequiresAddrAndDSN(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing addr", body: `dsn: "postgres://example"
auth:
  session_secret: "test-session-secret"
  encryption_key: "test-encryption-key"`},
		{name: "missing dsn", body: `addr: "127.0.0.1:19090"
auth:
  session_secret: "test-session-secret"
  encryption_key: "test-encryption-key"`},
		{name: "missing session secret", body: `addr: "127.0.0.1:19090"
dsn: "postgres://example"
auth:
  encryption_key: "test-encryption-key"`},
		{name: "missing encryption key", body: `addr: "127.0.0.1:19090"
dsn: "postgres://example"
auth:
  session_secret: "test-session-secret"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tc.body)); err == nil {
				t.Fatal("Load should reject incomplete config")
			}
		})
	}
}
