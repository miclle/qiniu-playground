package handler

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/miclle/qiniu-playground/internal/config"
	"github.com/miclle/qiniu-playground/internal/service"
)

const githubAPIBaseURL = "https://api.github.com"
const maxGitHubResponseBytes = 1 << 20

type githubAppClient interface {
	InstallationToken(ctx context.Context, installationID int64) (string, error)
	ListAppInstallations(ctx context.Context) ([]service.GitHubInstallationInput, error)
	ListInstallationRepositories(ctx context.Context, installationID int64) ([]service.GitHubRepositoryInput, error)
}

type realGitHubAppClient struct {
	appID          int64
	privateKeyPath string
	httpClient     *http.Client
}

func newGitHubAppClient(cfg config.GitHubConfig) githubAppClient {
	return &realGitHubAppClient{
		appID:          cfg.AppID,
		privateKeyPath: cfg.AppPrivateKeyPath,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *realGitHubAppClient) ListAppInstallations(ctx context.Context) ([]service.GitHubInstallationInput, error) {
	appJWT, err := c.appJWT()
	if err != nil {
		return nil, err
	}
	var installations []service.GitHubInstallationInput
	pageURL := githubAPIBaseURL + "/app/installations?per_page=100"
	for pageURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+appJWT)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list github app installations: %w", err)
		}
		var payload []struct {
			ID                  int64  `json:"id"`
			TargetType          string `json:"target_type"`
			RepositorySelection string `json:"repository_selection"`
			Account             struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"account"`
		}
		if err := decodeGitHubResponse(resp, &payload); err != nil {
			return nil, err
		}
		for _, installation := range payload {
			targetType := installation.TargetType
			if targetType == "" {
				targetType = installation.Account.Type
			}
			installations = append(installations, service.GitHubInstallationInput{
				InstallationID:      installation.ID,
				TargetType:          targetType,
				TargetLogin:         installation.Account.Login,
				RepositorySelection: installation.RepositorySelection,
			})
		}
		pageURL = nextLink(resp.Header.Get("Link"))
	}
	return installations, nil
}

func (c *realGitHubAppClient) ListInstallationRepositories(ctx context.Context, installationID int64) ([]service.GitHubRepositoryInput, error) {
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	var repos []service.GitHubRepositoryInput
	pageURL := githubAPIBaseURL + "/installation/repositories?per_page=100"
	for pageURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list installation repositories: %w", err)
		}
		var payload struct {
			Repositories []struct {
				ID            int64  `json:"id"`
				Name          string `json:"name"`
				FullName      string `json:"full_name"`
				Private       bool   `json:"private"`
				DefaultBranch string `json:"default_branch"`
				HTMLURL       string `json:"html_url"`
				Owner         struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"repositories"`
		}
		if err := decodeGitHubResponse(resp, &payload); err != nil {
			return nil, err
		}
		for _, repo := range payload.Repositories {
			repos = append(repos, service.GitHubRepositoryInput{
				GitHubRepoID:  repo.ID,
				Owner:         repo.Owner.Login,
				Name:          repo.Name,
				FullName:      repo.FullName,
				Private:       repo.Private,
				DefaultBranch: repo.DefaultBranch,
				HTMLURL:       repo.HTMLURL,
			})
		}
		pageURL = nextLink(resp.Header.Get("Link"))
	}
	return repos, nil
}

func (c *realGitHubAppClient) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	appJWT, err := c.appJWT()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+appJWT)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create installation token: %w", err)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := decodeGitHubResponse(resp, &payload); err != nil {
		return "", err
	}
	if payload.Token == "" {
		return "", fmt.Errorf("github installation token response did not include a token")
	}
	return payload.Token, nil
}

func (c *realGitHubAppClient) appJWT() (string, error) {
	if c.appID == 0 {
		return "", fmt.Errorf("github app id is required")
	}
	privateKey, err := loadRSAPrivateKey(c.privateKeyPath)
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(c.appID, 10),
	})
	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return signed, nil
}

func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return nil, fmt.Errorf("github app private key path is required")
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read github app private key: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("decode github app private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github app private key must be RSA")
	}
	return key, nil
}

func decodeGitHubResponse(resp *http.Response, out any) error {
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubResponseBytes))
	if err != nil {
		return fmt.Errorf("read github response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}
	return nil
}

func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		sections := strings.Split(part, ";")
		if len(sections) < 2 {
			continue
		}
		isNext := false
		for _, section := range sections[1:] {
			if strings.TrimSpace(section) == `rel="next"` {
				isNext = true
				break
			}
		}
		if isNext {
			return strings.Trim(strings.TrimSpace(sections[0]), "<>")
		}
	}
	return ""
}
