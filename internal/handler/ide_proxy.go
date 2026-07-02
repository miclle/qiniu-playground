package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
)

func (ctrl *Ctrl) SandboxIDEProxy(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	sandboxID := c.Param("sandboxID")
	if sandboxID == "" {
		return httperrors.New(http.StatusBadRequest, "sandbox id is required")
	}
	session, err := ctrl.service.SandboxSession(c.Request.Context(), accountID, sandboxID)
	if err != nil {
		return httperrors.New(http.StatusNotFound, "sandbox session not found")
	}
	targetHost, err := sandboxIDEHost(session.IDEURL)
	if err != nil {
		return err
	}
	target := &url.URL{Scheme: "https", Host: targetHost}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = ideProxyTargetPath(c.Param("proxyPath"))
			pr.Out.Host = target.Host
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
	c.Abort()
	return nil
}

func (ctrl *Ctrl) ideProxyURL(sandboxID, ideURL string) string {
	if sandboxID == "" || ideURL == "" {
		return ""
	}
	return "/api/v1/sandboxes/" + url.PathEscape(sandboxID) + "/ide/"
}

func sandboxIDEHost(ideURL string) (string, error) {
	parsed, err := url.Parse(ideURL)
	if err != nil || parsed.Host == "" {
		return "", httperrors.New(http.StatusPreconditionFailed, "sandbox IDE is not available")
	}
	if parsed.Scheme != "https" {
		return "", httperrors.New(http.StatusPreconditionFailed, "sandbox IDE URL is invalid")
	}
	return parsed.Host, nil
}

func ideProxyTargetPath(proxyPath string) string {
	if proxyPath == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(proxyPath, "/"))
}
