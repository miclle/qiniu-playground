package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/miclle/qiniu-playground/internal/config"
	"github.com/miclle/qiniu-playground/internal/service"
	"github.com/miclle/qiniu-playground/pkg/secret"
)

const githubUserAPI = "https://api.github.com/user"
const githubOrgMembershipAPI = "https://api.github.com/user/memberships/orgs/"

type githubOAuthClient interface {
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	FetchUser(ctx context.Context, token *oauth2.Token) (*service.OAuthUser, error)
	OrgMembership(ctx context.Context, accessToken, org string) (*githubOrgMembership, error)
}

type realGitHubOAuthClient struct {
	config *oauth2.Config
	client *http.Client
}

func newGitHubOAuthClient(cfg config.GitHubConfig) githubOAuthClient {
	return &realGitHubOAuthClient{
		config: &oauth2.Config{
			ClientID:     cfg.OAuthClientID,
			ClientSecret: cfg.OAuthClientSecret,
			RedirectURL:  cfg.OAuthRedirectURL,
			Scopes:       []string{"read:user", "user:email", "read:org"},
			Endpoint:     github.Endpoint,
		},
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type githubOrgMembership struct {
	State string
	Role  string
}

func (c *realGitHubOAuthClient) AuthCodeURL(state string) string {
	return c.config.AuthCodeURL(state)
}

func (c *realGitHubOAuthClient) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return c.config.Exchange(ctx, code)
}

func (c *realGitHubOAuthClient) FetchUser(ctx context.Context, token *oauth2.Token) (*service.OAuthUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch github user: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch github user: status %d", resp.StatusCode)
	}

	var payload struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode github user: %w", err)
	}
	return &service.OAuthUser{
		ProviderSubject: strconv.FormatInt(payload.ID, 10),
		Login:           payload.Login,
		DisplayName:     payload.Name,
		AvatarURL:       payload.AvatarURL,
		Email:           payload.Email,
	}, nil
}

func (c *realGitHubOAuthClient) OrgMembership(ctx context.Context, accessToken, org string) (*githubOrgMembership, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubOrgMembershipAPI+url.PathEscape(org), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch github org membership: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, httperrors.New(http.StatusForbidden, "organization installation is not authorized for this account")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch github org membership: status %d", resp.StatusCode)
	}
	var payload struct {
		State string `json:"state"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode github org membership: %w", err)
	}
	return &githubOrgMembership{State: payload.State, Role: payload.Role}, nil
}

func (ctrl *Ctrl) GitHubLogin(c *fox.Context) any {
	if !ctrl.githubOAuthEnabled() {
		return httperrors.New(http.StatusServiceUnavailable, "GitHub OAuth is not configured")
	}
	state, err := secret.RandomURLSafe(32)
	if err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, state, int((10 * time.Minute).Seconds()), "/api/v1/auth/github", "", false, true)
	c.Redirect(http.StatusFound, ctrl.githubOAuth.AuthCodeURL(state))
	return nil
}

func (ctrl *Ctrl) GitHubCallback(c *fox.Context) any {
	if !ctrl.githubOAuthEnabled() {
		return httperrors.New(http.StatusServiceUnavailable, "GitHub OAuth is not configured")
	}
	state := c.Query("state")
	code := c.Query("code")
	if state == "" || code == "" {
		return httperrors.New(http.StatusBadRequest, "missing OAuth state or code")
	}
	storedState, err := c.Cookie(oauthStateCookie)
	if err != nil || storedState != state {
		return httperrors.New(http.StatusBadRequest, "invalid OAuth state")
	}

	token, err := ctrl.githubOAuth.Exchange(c.Request.Context(), code)
	if err != nil {
		c.Logger.Errorf("exchange github oauth code: %v", err)
		return httperrors.New(http.StatusBadGateway, "failed to exchange OAuth code")
	}
	oauthUser, err := ctrl.githubOAuth.FetchUser(c.Request.Context(), token)
	if err != nil {
		c.Logger.Errorf("fetch github user: %v", err)
		return httperrors.New(http.StatusBadGateway, "failed to fetch GitHub user")
	}
	authUser, err := ctrl.service.UpsertGitHubIdentity(c.Request.Context(), *oauthUser)
	if err != nil {
		c.Logger.Errorf("upsert github identity: %v", err)
		return httperrors.New(http.StatusInternalServerError, "failed to authenticate user")
	}
	encryptedToken, err := ctrl.credentialBox.Encrypt(token.AccessToken)
	if err != nil {
		return err
	}
	if err := ctrl.service.SaveGitHubAccessToken(c.Request.Context(), authUser.AccountID, encryptedToken); err != nil {
		return err
	}

	c.SetCookie(oauthStateCookie, "", -1, "/api/v1/auth/github", "", false, true)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, ctrl.sessionSigner.Sign(authUser.AccountID, time.Now()), int(sessionMaxAge.Seconds()), "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
	return nil
}

func (ctrl *Ctrl) Me(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	user, err := ctrl.service.AuthUserByAccountID(c.Request.Context(), accountID)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	return user
}

func (ctrl *Ctrl) Logout(c *fox.Context) any {
	c.SetCookie(sessionCookieName, "", -1, "/", "", false, true)
	return map[string]bool{"ok": true}
}

func (ctrl *Ctrl) accountIDFromRequest(c *fox.Context) (string, error) {
	cookie, err := c.Cookie(sessionCookieName)
	if err != nil {
		return "", err
	}
	return ctrl.sessionSigner.Verify(cookie, time.Now())
}

func (ctrl *Ctrl) githubOAuthEnabled() bool {
	if ctrl.githubOAuth == nil {
		return false
	}
	authURL := ctrl.githubOAuth.AuthCodeURL("state-check")
	parsed, err := url.Parse(authURL)
	if err != nil {
		return false
	}
	return parsed.Query().Get("client_id") != ""
}
