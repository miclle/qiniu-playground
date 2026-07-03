package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
)

func (ctrl *Ctrl) SandboxIDEProxy(c *fox.Context) any {
	sandboxID := c.Param("sandboxID")
	if sandboxID == "" {
		return httperrors.New(http.StatusBadRequest, "sandbox id is required")
	}
	accountID, err := ctrl.accountIDFromIDEProxyRequest(c, sandboxID)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
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
	codeServerPassword := ctrl.codeServerPassword(sandboxID)
	allowUpstreamCookies := isIsolatedIDEHost(c.Request.Host)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = ideProxyTargetPath(c.Param("proxyPath"))
			stripIDEProxyOutboundQuery(pr.Out.URL)
			pr.Out.Host = target.Host
			if shouldStripIDEProxyAcceptEncoding(pr.In.Header) {
				pr.Out.Header.Del("Accept-Encoding")
			}
			stripIDEProxyRequestHeaders(pr.Out.Header, allowUpstreamCookies)
		},
		ModifyResponse: func(resp *http.Response) error {
			return injectIDEChromeCustomization(resp, codeServerPassword, allowUpstreamCookies)
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
	c.Abort()
	return nil
}

func (ctrl *Ctrl) accountIDFromIDEProxyRequest(c *fox.Context, sandboxID string) (string, error) {
	if accountID, err := ctrl.accountIDFromRequest(c); err == nil {
		return accountID, nil
	}
	if cookie, err := c.Request.Cookie(ideCookieName); err == nil {
		accountID, tokenSandboxID, err := ctrl.sessionSigner.VerifyIDE(cookie.Value, time.Now())
		if err == nil && tokenSandboxID == sandboxID {
			return accountID, nil
		}
	}
	token := c.Request.URL.Query().Get("ide_token")
	if token == "" {
		return "", httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	accountID, tokenSandboxID, err := ctrl.sessionSigner.VerifyIDE(token, time.Now())
	if err != nil || tokenSandboxID != sandboxID {
		return "", httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	ctrl.setIDEProxyCookie(c, sandboxID, token)
	return accountID, nil
}

func (ctrl *Ctrl) setIDEProxyCookie(c *fox.Context, sandboxID, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     ideCookieName,
		Value:    token,
		Path:     ideProxyCookiePath(sandboxID),
		MaxAge:   int(ideTokenMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   c.Request.TLS != nil || strings.EqualFold(c.Request.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
}

func ideProxyCookiePath(sandboxID string) string {
	return "/api/v1/sandboxes/" + url.PathEscape(sandboxID) + "/ide"
}

func injectIDEChromeCustomization(resp *http.Response, codeServerPassword string, allowUpstreamCookies bool) error {
	stripIDEProxyResponseHeaders(resp.Header, !allowUpstreamCookies)
	if resp.StatusCode != http.StatusOK || resp.Body == nil || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil
	}
	resp.Header.Del("ETag")
	resp.Header.Del("Last-Modified")
	resp.Header.Del("Content-Security-Policy")
	resp.Header.Del("Content-Security-Policy-Report-Only")
	originalBody := resp.Body
	defer func() {
		_ = originalBody.Close()
	}()
	body, err := io.ReadAll(originalBody)
	if err != nil {
		return err
	}
	body = injectIDEChromeCustomizationHTML(body, codeServerPassword)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

func stripIDEProxyOutboundQuery(out *url.URL) {
	query := out.Query()
	query.Del("ide_token")
	out.RawQuery = query.Encode()
}

func injectIDEChromeCustomizationHTML(body []byte, codeServerPassword string) []byte {
	insertAt := htmlHeadCloseIndex(body)
	if insertAt < 0 {
		return body
	}
	injected := []byte(`<style id="qiniu-playground-ide-chrome">
.monaco-workbench .part.auxiliarybar,
.monaco-workbench .part.activitybar.right,
.monaco-workbench .part.panel,
.monaco-workbench .part.statusbar {
  display: none !important;
  visibility: hidden !important;
  pointer-events: none !important;
  width: 0 !important;
  min-width: 0 !important;
}
.monaco-workbench .activitybar.left .actions-container .action-item:not(:has([aria-label*="Explorer"])):not(:has([aria-label*="Search"])),
.monaco-workbench .activitybar.left .global-activity {
  display: none !important;
}
.monaco-workbench .titlebar .command-center,
.monaco-workbench .titlebar .layout-controls,
.monaco-workbench .titlebar .window-appicon,
.monaco-workbench .titlebar .menubar,
.monaco-workbench .titlebar .titlebar-center,
.monaco-workbench .titlebar .titlebar-right,
.monaco-workbench .menubar.compact,
.monaco-workbench .menubar-menu-button,
.monaco-workbench .toolbar-toggle-more {
  display: none !important;
}
.monaco-workbench .part.titlebar,
.monaco-workbench #workbench\.parts\.titlebar {
  height: 0 !important;
  min-height: 0 !important;
  max-height: 0 !important;
  border: 0 !important;
  opacity: 0 !important;
  overflow: hidden !important;
}
.monaco-workbench .part.titlebar > *,
.monaco-workbench #workbench\.parts\.titlebar > * {
  display: none !important;
}
.monaco-workbench .part.sidebar .split-view-container:has(.pane-header[aria-label^="Explorer Section"]) > .split-view-view:nth-child(n+2),
.monaco-workbench .part.sidebar .split-view-view:has(.pane-header.timeline-view) {
  display: none !important;
  visibility: hidden !important;
  height: 0 !important;
  min-height: 0 !important;
  max-height: 0 !important;
  flex-basis: 0 !important;
}
.monaco-workbench .split-view-view:has(> .part.auxiliarybar) {
  display: none !important;
  visibility: hidden !important;
  width: 0 !important;
  min-width: 0 !important;
  flex-basis: 0 !important;
}
.monaco-workbench .split-view-view:has(> .part.titlebar),
.monaco-workbench .split-view-view:has(> #workbench\.parts\.titlebar) {
  height: 0 !important;
  min-height: 0 !important;
  flex-basis: 0 !important;
}
</style>`)
	script := []byte(`<script id="qiniu-playground-ide-layout">
(() => {
  const applyLayout = () => {
    document.querySelectorAll('.monaco-workbench .part.auxiliarybar, .monaco-workbench .part.activitybar.right, .monaco-workbench .part.panel, .monaco-workbench .part.statusbar').forEach((part) => {
      part.style.setProperty('display', 'none', 'important');
      part.style.setProperty('width', '0', 'important');
      part.style.setProperty('min-width', '0', 'important');
      if (part.classList.contains('auxiliarybar')) {
        part.closest('.split-view-view')?.style.setProperty('display', 'none', 'important');
        part.closest('.split-view-view')?.style.setProperty('width', '0', 'important');
        part.closest('.split-view-view')?.style.setProperty('min-width', '0', 'important');
        part.closest('.split-view-view')?.style.setProperty('flex-basis', '0', 'important');
      }
    });
    document.querySelectorAll('.monaco-workbench .part.titlebar, .monaco-workbench #workbench\\.parts\\.titlebar').forEach((part) => {
      part.style.setProperty('height', '0', 'important');
      part.style.setProperty('min-height', '0', 'important');
      part.style.setProperty('max-height', '0', 'important');
      part.style.setProperty('border', '0', 'important');
      part.style.setProperty('opacity', '0', 'important');
      part.style.setProperty('overflow', 'hidden', 'important');
      part.closest('.split-view-view')?.style.setProperty('height', '0', 'important');
      part.closest('.split-view-view')?.style.setProperty('min-height', '0', 'important');
      part.closest('.split-view-view')?.style.setProperty('max-height', '0', 'important');
      part.closest('.split-view-view')?.style.setProperty('flex-basis', '0', 'important');
      Array.from(part.children).forEach((child) => child.style.setProperty('display', 'none', 'important'));
    });
    document.querySelectorAll('.monaco-workbench .activitybar.left .actions-container .action-item').forEach((item) => {
      const label = item.textContent + ' ' + Array.from(item.querySelectorAll('[aria-label]')).map((el) => el.getAttribute('aria-label')).join(' ');
      if (!/Explorer|Search/i.test(label)) {
        item.style.setProperty('display', 'none', 'important');
      }
    });
    document.querySelectorAll('.monaco-workbench .activitybar.left .global-activity, .monaco-workbench .titlebar .command-center, .monaco-workbench .titlebar .layout-controls, .monaco-workbench .titlebar .window-appicon, .monaco-workbench .titlebar .menubar').forEach((part) => {
      part.style.setProperty('display', 'none', 'important');
    });
    document.querySelectorAll('.monaco-workbench .titlebar .titlebar-center, .monaco-workbench .titlebar .titlebar-right, .monaco-workbench .menubar.compact, .monaco-workbench .menubar-menu-button, .monaco-workbench .toolbar-toggle-more').forEach((part) => {
      part.style.setProperty('display', 'none', 'important');
    });
    document.querySelectorAll('.monaco-workbench .pane').forEach((pane) => {
      const header = pane.querySelector('.pane-header');
      const label = header?.textContent?.trim() || '';
      if (/^(OUTLINE|TIMELINE)$/i.test(label)) {
        pane.closest('.split-view-view')?.style.setProperty('display', 'none', 'important');
        pane.closest('.split-view-view')?.style.setProperty('height', '0', 'important');
        pane.closest('.split-view-view')?.style.setProperty('min-height', '0', 'important');
        pane.closest('.split-view-view')?.style.setProperty('flex-basis', '0', 'important');
        pane.style.setProperty('display', 'none', 'important');
      }
    });
	  };
	  const runLayout = () => {
	    applyLayout();
	    window.dispatchEvent(new Event('resize'));
	  };
	  if (document.readyState !== 'loading') {
	    runLayout();
	  }
	  window.addEventListener('load', runLayout, { once: true });
	  [100, 500, 1500, 3000].forEach((delay) => window.setTimeout(runLayout, delay));
	})();
	</script>`)
	loginScript := []byte{}
	if codeServerPassword != "" {
		password, _ := json.Marshal(codeServerPassword)
		loginScript = []byte(`<script id="qiniu-playground-code-server-login">
(() => {
  const password = ` + string(password) + `;
  const submitLogin = () => {
    const input = document.querySelector('input[type="password"], input[name="password"]');
    const form = input?.closest('form') || document.querySelector('form');
    if (!input || !form || input.dataset.qiniuPlaygroundFilled === 'true') {
      return;
    }
    if (document.querySelector('.error')) {
      return;
    }
    input.value = password;
    input.dataset.qiniuPlaygroundFilled = 'true';
    form.requestSubmit ? form.requestSubmit() : form.submit();
  };
  if (/\/login\/?$/.test(window.location.pathname)) {
    if (document.readyState !== 'loading') {
      submitLogin();
    }
    window.addEventListener('load', submitLogin, { once: true });
    [100, 500, 1000].forEach((delay) => window.setTimeout(submitLogin, delay));
  }
})();
</script>`)
	}
	out := make([]byte, 0, len(body)+len(injected)+len(script)+len(loginScript))
	out = append(out, body[:insertAt]...)
	out = append(out, injected...)
	out = append(out, script...)
	out = append(out, loginScript...)
	out = append(out, body[insertAt:]...)
	return out
}

func stripIDEProxyRequestHeaders(header http.Header, allowUpstreamCookies bool) {
	header.Del("Authorization")
	header.Del("Proxy-Authorization")
	if !allowUpstreamCookies {
		header.Del("Cookie")
		return
	}
	filterIDEProxyCookieHeader(header)
}

func filterIDEProxyCookieHeader(header http.Header) {
	cookies := header.Values("Cookie")
	header.Del("Cookie")
	for _, line := range cookies {
		kept := make([]string, 0)
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			name, _, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(name) {
			case sessionCookieName, ideCookieName, oauthStateCookie:
				continue
			default:
				kept = append(kept, part)
			}
		}
		if len(kept) > 0 {
			header.Add("Cookie", strings.Join(kept, "; "))
		}
	}
}

func shouldStripIDEProxyAcceptEncoding(header http.Header) bool {
	accepts := header.Values("Accept")
	if len(accepts) == 0 {
		return true
	}
	for _, accept := range accepts {
		if accept == "*/*" || strings.Contains(accept, "text/html") {
			return true
		}
	}
	return false
}

func stripIDEProxyResponseHeaders(header http.Header, stripCookies bool) {
	if stripCookies {
		header.Del("Set-Cookie")
		header.Del("Set-Cookie2")
	}
}

func htmlHeadCloseIndex(body []byte) int {
	tag := []byte("</head>")
	for start := 0; start < len(body); {
		offset := bytes.IndexByte(body[start:], '<')
		if offset < 0 {
			return -1
		}
		index := start + offset
		end := index + len(tag)
		if end <= len(body) && bytes.EqualFold(body[index:end], tag) {
			return index
		}
		start = index + 1
	}
	return -1
}

func (ctrl *Ctrl) ideProxyURL(req *http.Request, accountID, sandboxID, ideURL string) string {
	if sandboxID == "" || ideURL == "" {
		return ""
	}
	proxyPath := "/api/v1/sandboxes/" + url.PathEscape(sandboxID) + "/ide/"
	if accountID != "" {
		query := url.Values{}
		query.Set("ide_token", ctrl.sessionSigner.SignIDE(accountID, sandboxID, time.Now()))
		proxyPath += "?" + query.Encode()
	}
	if req == nil || req.Host == "" {
		return proxyPath
	}
	scheme := "http"
	if req.TLS != nil || strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + isolatedIDEHost(sandboxID, req.Host) + proxyPath
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

func isolatedIDEHost(sandboxID, requestHost string) string {
	host, port := splitHostPort(requestHost)
	if isLocalIDEProxyHost(host) {
		return requestHost
	}
	prefix := dnsLabelUnsafeChars.ReplaceAllString(strings.ToLower(sandboxID), "-")
	prefix = strings.Trim(prefix, "-")
	if prefix == "" {
		prefix = "sandbox"
	}
	return prefix + ".ide." + host + port
}

func splitHostPort(hostport string) (string, string) {
	host, port, err := net.SplitHostPort(hostport)
	if err == nil {
		return host, ":" + port
	}
	return hostport, ""
}

func isIsolatedIDEHost(hostport string) bool {
	host, _ := splitHostPort(hostport)
	return strings.Contains(host, ".ide.") || isLocalIDEProxyHost(host)
}

func isLocalIDEProxyHost(host string) bool {
	ipHost := host
	if strings.HasPrefix(ipHost, "[") && strings.HasSuffix(ipHost, "]") {
		ipHost = ipHost[1 : len(ipHost)-1]
	}
	return net.ParseIP(ipHost) != nil || strings.EqualFold(host, "localhost") || host == ""
}

func (ctrl *Ctrl) codeServerPassword(sandboxID string) string {
	return ctrl.sessionSigner.sign("code-server." + sandboxID)
}
