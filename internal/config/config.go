// Package config provides application configuration loading and structures.
package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the application configuration.
type Config struct {
	Addr    string        `mapstructure:"addr"`   // listen address, e.g. "0.0.0.0:19090"
	Driver  string        `mapstructure:"driver"` // database driver: "postgres" (default) or "mysql"
	DSN     string        `mapstructure:"dsn"`    // database connection string
	Auth    AuthConfig    `mapstructure:"auth"`
	GitHub  GitHubConfig  `mapstructure:"github"`
	Sandbox SandboxConfig `mapstructure:"sandbox"`
}

// AuthConfig contains application-owned authentication settings.
type AuthConfig struct {
	SessionSecret string `mapstructure:"session_secret"`
	EncryptionKey string `mapstructure:"encryption_key"`
}

// GitHubConfig contains GitHub OAuth and GitHub App settings.
type GitHubConfig struct {
	OAuthClientID     string `mapstructure:"oauth_client_id"`
	OAuthClientSecret string `mapstructure:"oauth_client_secret"`
	OAuthRedirectURL  string `mapstructure:"oauth_redirect_url"`
	AppID             int64  `mapstructure:"app_id"`
	AppSlug           string `mapstructure:"app_slug"`
	AppPrivateKeyPath string `mapstructure:"app_private_key_path"`
}

// SandboxConfig contains Qiniu Sandbox defaults.
type SandboxConfig struct {
	Endpoint              string `mapstructure:"endpoint"`
	DefaultTemplateID     string `mapstructure:"default_template_id"`
	DefaultTimeoutSeconds int32  `mapstructure:"default_timeout_seconds"`
}

// Load reads configuration from the given file path.
func Load(path string) (*Config, error) {
	cfgFile, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	cfgFile = []byte(expandEnv(string(cfgFile)))

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(cfgFile)); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Addr == "" {
		return nil, fmt.Errorf("addr is required")
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	if cfg.Driver == "" {
		cfg.Driver = "postgres"
	}
	if cfg.Driver != "postgres" && cfg.Driver != "mysql" {
		return nil, fmt.Errorf("unsupported driver: %s (supported: postgres, mysql)", cfg.Driver)
	}
	if cfg.Auth.SessionSecret == "" {
		return nil, fmt.Errorf("auth.session_secret is required")
	}
	if cfg.Auth.EncryptionKey == "" {
		return nil, fmt.Errorf("auth.encryption_key is required")
	}
	if cfg.Sandbox.DefaultTemplateID == "" {
		cfg.Sandbox.DefaultTemplateID = "base"
	}
	if cfg.Sandbox.DefaultTimeoutSeconds == 0 {
		cfg.Sandbox.DefaultTimeoutSeconds = 120
	}

	return &cfg, nil
}

func expandEnv(s string) string {
	return os.Expand(s, func(name string) string {
		key, fallback, ok := strings.Cut(name, ":-")
		if !ok {
			return os.Getenv(name)
		}
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fallback
	})
}
