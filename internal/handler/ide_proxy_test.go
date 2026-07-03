package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type trackingReadCloser struct {
	*strings.Reader
	closed bool
}

func (rc *trackingReadCloser) Close() error {
	rc.closed = true
	return nil
}

func TestIDEProxyURLUsesOwnedSandboxPath(t *testing.T) {
	ctrl := newTestController(t)
	req := httptest.NewRequest(http.MethodGet, "https://playground.example.test/workspaces", nil)
	got := ctrl.ideProxyURL(req, "acct-1", "sandbox-1", "https://sandbox.example.test")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse IDE proxy URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "sandbox-1.ide.playground.example.test" {
		t.Fatalf("ideProxyURL() host = %s://%s, want isolated IDE origin", parsed.Scheme, parsed.Host)
	}
	if parsed.Path != "/api/v1/sandboxes/sandbox-1/ide/" {
		t.Fatalf("ideProxyURL() path = %q, want owned sandbox IDE path", parsed.Path)
	}
	accountID, sandboxID, err := ctrl.sessionSigner.VerifyIDE(parsed.Query().Get("ide_token"), time.Now())
	if err != nil {
		t.Fatalf("verify IDE token: %v", err)
	}
	if accountID != "acct-1" || sandboxID != "sandbox-1" {
		t.Fatalf("IDE token = account %q sandbox %q, want scoped token", accountID, sandboxID)
	}
	if got := ctrl.ideProxyURL(req, "acct-1", "sandbox-1", ""); got != "" {
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

func TestIDEProxyCookiePathOmitsTrailingSlash(t *testing.T) {
	if got := ideProxyCookiePath("sandbox/one"); got != "/api/v1/sandboxes/sandbox%2Fone/ide" {
		t.Fatalf("ideProxyCookiePath() = %q, want escaped path without trailing slash", got)
	}
}

func TestStripIDEProxyOutboundQueryRemovesToken(t *testing.T) {
	out := &url.URL{RawQuery: "ide_token=secret&folder=%2Fhome%2Fuser"}

	stripIDEProxyOutboundQuery(out)

	if strings.Contains(out.RawQuery, "ide_token") {
		t.Fatalf("RawQuery = %q, want ide_token stripped", out.RawQuery)
	}
	if got := out.Query().Get("folder"); got != "/home/user" {
		t.Fatalf("folder query = %q, want preserved", got)
	}
}

func TestIsolatedIDEHostPreservesIPv6RequestHost(t *testing.T) {
	for _, requestHost := range []string{"[::1]", "[::1]:19090", "127.0.0.1:19090", "localhost", "localhost:19090"} {
		if got := isolatedIDEHost("sandbox-1", requestHost); got != requestHost {
			t.Fatalf("isolatedIDEHost() = %q, want local request host %q preserved", got, requestHost)
		}
	}
}

func TestIsIsolatedIDEHostAllowsLocalFallbackHosts(t *testing.T) {
	for _, host := range []string{"sandbox.ide.example.test", "[::1]", "[::1]:19090", "127.0.0.1:19090", "localhost", "localhost:19090", ""} {
		if !isIsolatedIDEHost(host) {
			t.Fatalf("isIsolatedIDEHost(%q) = false, want true", host)
		}
	}
	if isIsolatedIDEHost("example.test") {
		t.Fatal("isIsolatedIDEHost(example.test) = true, want false")
	}
}

func TestInjectIDEChromeCustomizationHTML(t *testing.T) {
	got := string(injectIDEChromeCustomizationHTML([]byte("<html><head></head><body></body></html>"), "code-server-password"))
	for _, want := range []string{
		`id="qiniu-playground-ide-chrome"`,
		`id="qiniu-playground-ide-layout"`,
		`id="qiniu-playground-code-server-login"`,
		`code-server-password`,
		"document.querySelector('.error')",
		".monaco-workbench .part.auxiliarybar",
		".monaco-workbench .part.panel",
		".monaco-workbench .part.statusbar",
		".monaco-workbench .part.titlebar",
		`#workbench\.parts\.titlebar`,
		`aria-label*="Explorer"`,
		`aria-label*="Search"`,
		".global-activity",
		".layout-controls",
		".titlebar-center",
		".titlebar-right",
		".menubar.compact",
		".toolbar-toggle-more",
		".split-view-view:has(> .part.titlebar)",
		`[aria-label^="Explorer Section"]`,
		".pane-header.timeline-view",
		".split-view-view:has(> .part.auxiliarybar)",
		"closest('.split-view-view')",
		"classList.contains('auxiliarybar')",
		".pane-header",
		"OUTLINE|TIMELINE",
		"Explorer|Search",
		"runLayout",
		"setTimeout",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("customized HTML missing %q in %q", want, got)
		}
	}
	for _, reject := range []string{
		":has(.part.auxiliarybar)",
		":has(.part.activitybar.right)",
		"attributes: true",
		"--qiniu-playground-editor-left",
		"calc(100vw",
		".part.titlebar {\n  display: none",
		"MutationObserver",
		"requestAnimationFrame",
	} {
		if strings.Contains(got, reject) {
			t.Fatalf("customized HTML should not include broad selector %q in %q", reject, got)
		}
	}
}

func TestInjectIDEChromeCustomizationFindsMixedCaseHead(t *testing.T) {
	got := string(injectIDEChromeCustomizationHTML([]byte("<html><HEAD></HEAD><body></body></html>"), ""))
	if !strings.Contains(got, `id="qiniu-playground-ide-chrome"`) {
		t.Fatalf("customized HTML missing injected chrome: %s", got)
	}
}

func TestInjectIDEChromeCustomizationAllowsInjectedChrome(t *testing.T) {
	originalBody := &trackingReadCloser{Reader: strings.NewReader("<html><head></head><body></body></html>")}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                        []string{"text/html; charset=utf-8"},
			"Content-Security-Policy":             []string{"default-src 'self'"},
			"Content-Security-Policy-Report-Only": []string{"default-src 'self'"},
			"ETag":                                []string{`"upstream"`},
			"Last-Modified":                       []string{"Fri, 03 Jul 2026 10:00:00 GMT"},
		},
		Body: originalBody,
	}

	if err := injectIDEChromeCustomization(resp, "code-server-password", true); err != nil {
		t.Fatal(err)
	}
	if !originalBody.closed {
		t.Fatal("original response body should be closed after HTML injection")
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "" {
		t.Fatalf("Content-Security-Policy = %q, want empty", got)
	}
	if got := resp.Header.Get("Content-Security-Policy-Report-Only"); got != "" {
		t.Fatalf("Content-Security-Policy-Report-Only = %q, want empty", got)
	}
	if got := resp.Header.Get("ETag"); got != "" {
		t.Fatalf("ETag = %q, want empty after HTML injection", got)
	}
	if got := resp.Header.Get("Last-Modified"); got != "" {
		t.Fatalf("Last-Modified = %q, want empty after HTML injection", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `id="qiniu-playground-ide-chrome"`) {
		t.Fatalf("customized response missing injected chrome: %s", body)
	}
}

func TestInjectIDEChromeCustomizationSkipsNonOKResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusNotModified,
		Header: http.Header{
			"Content-Type": []string{"text/html; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader("<html><head></head><body></body></html>")),
	}

	if err := injectIDEChromeCustomization(resp, "", false); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `id="qiniu-playground-ide-chrome"`) {
		t.Fatalf("customized non-OK response: %s", body)
	}
}

func TestInjectIDEChromeCustomizationSkipsCompressedResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":     []string{"text/html; charset=utf-8"},
			"Content-Encoding": []string{"gzip"},
		},
		Body: io.NopCloser(strings.NewReader("compressed-body")),
	}

	if err := injectIDEChromeCustomization(resp, "", false); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "compressed-body" {
		t.Fatalf("body = %q, want unchanged compressed body", got)
	}
}

func TestStripIDEProxyRequestHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer app-token")
	header.Set("Cookie", "qiniu_playground_session=session-token")
	header.Set("If-Modified-Since", "Fri, 03 Jul 2026 10:00:00 GMT")
	header.Set("If-None-Match", `"etag"`)
	header.Set("Proxy-Authorization", "Basic proxy-token")
	header.Set("X-Request-ID", "req-1")

	stripIDEProxyRequestHeaders(header, false)

	for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if got := header.Get(name); got != "" {
			t.Fatalf("%s = %q, want stripped", name, got)
		}
	}
	if got := header.Get("If-Modified-Since"); got == "" {
		t.Fatal("If-Modified-Since should be preserved")
	}
	if got := header.Get("If-None-Match"); got == "" {
		t.Fatal("If-None-Match should be preserved")
	}
	if got := header.Get("X-Request-ID"); got != "req-1" {
		t.Fatalf("X-Request-ID = %q, want preserved", got)
	}
}

func TestStripIDEProxyRequestHeadersAllowsUpstreamCookiesForIsolatedOrigin(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer app-token")
	header.Add("Cookie", "qiniu_playground_session=session-token; code-server-session=upstream-token")
	header.Add("Cookie", "qiniu_playground_ide_session=ide-token; qiniu_playground_oauth_state=state-token; other=value")
	header.Set("Proxy-Authorization", "Basic proxy-token")
	header.Set("X-Request-ID", "req-1")

	stripIDEProxyRequestHeaders(header, true)

	for _, name := range []string{"Authorization", "Proxy-Authorization"} {
		if got := header.Get(name); got != "" {
			t.Fatalf("%s = %q, want stripped", name, got)
		}
	}
	gotCookies := strings.Join(header.Values("Cookie"), "; ")
	for _, reject := range []string{sessionCookieName, ideCookieName, oauthStateCookie} {
		if strings.Contains(gotCookies, reject) {
			t.Fatalf("Cookie = %q, want %s stripped", gotCookies, reject)
		}
	}
	for _, want := range []string{"code-server-session=upstream-token", "other=value"} {
		if !strings.Contains(gotCookies, want) {
			t.Fatalf("Cookie = %q, want preserved upstream cookie %q", gotCookies, want)
		}
	}
	if got := header.Get("X-Request-ID"); got != "req-1" {
		t.Fatalf("X-Request-ID = %q, want preserved", got)
	}
}

func TestShouldStripIDEProxyAcceptEncoding(t *testing.T) {
	htmlHeader := http.Header{}
	htmlHeader.Set("Accept", "text/html,application/xhtml+xml")
	if !shouldStripIDEProxyAcceptEncoding(htmlHeader) {
		t.Fatal("HTML requests should strip Accept-Encoding for body injection")
	}
	emptyHeader := http.Header{}
	if !shouldStripIDEProxyAcceptEncoding(emptyHeader) {
		t.Fatal("empty Accept requests should strip Accept-Encoding for body injection")
	}
	wildcardHeader := http.Header{}
	wildcardHeader.Set("Accept", "*/*")
	if !shouldStripIDEProxyAcceptEncoding(wildcardHeader) {
		t.Fatal("wildcard Accept requests should strip Accept-Encoding for body injection")
	}
	multiHeader := http.Header{}
	multiHeader.Add("Accept", "application/json")
	multiHeader.Add("Accept", "text/html")
	if !shouldStripIDEProxyAcceptEncoding(multiHeader) {
		t.Fatal("multi-value Accept requests should inspect all values")
	}

	assetHeader := http.Header{}
	assetHeader.Set("Accept", "text/css,*/*;q=0.1")
	if shouldStripIDEProxyAcceptEncoding(assetHeader) {
		t.Fatal("asset requests should preserve Accept-Encoding")
	}
}

func TestStripIDEProxyResponseHeaders(t *testing.T) {
	header := http.Header{}
	header.Add("Set-Cookie", "qiniu_playground_session=sandbox-token; Path=/")
	header.Add("Set-Cookie2", "legacy=sandbox-token; Path=/")
	header.Set("Content-Type", "text/html")
	header.Set("ETag", `"upstream"`)
	header.Set("Last-Modified", "Fri, 03 Jul 2026 10:00:00 GMT")

	stripIDEProxyResponseHeaders(header, true)

	for _, name := range []string{"Set-Cookie", "Set-Cookie2"} {
		if got := header.Values(name); len(got) != 0 {
			t.Fatalf("%s = %q, want stripped", name, got)
		}
	}
	if got := header.Get("ETag"); got != `"upstream"` {
		t.Fatalf("ETag = %q, want preserved", got)
	}
	if got := header.Get("Last-Modified"); got != "Fri, 03 Jul 2026 10:00:00 GMT" {
		t.Fatalf("Last-Modified = %q, want preserved", got)
	}
	if got := header.Get("Content-Type"); got != "text/html" {
		t.Fatalf("Content-Type = %q, want preserved", got)
	}
}

func TestStripIDEProxyResponseHeadersAllowsCookiesForIsolatedOrigin(t *testing.T) {
	header := http.Header{}
	header.Add("Set-Cookie", "code-server-session=token; Path=/")
	header.Set("ETag", `"upstream"`)

	stripIDEProxyResponseHeaders(header, false)

	if got := header.Values("Set-Cookie"); len(got) != 1 {
		t.Fatalf("Set-Cookie = %q, want preserved for isolated IDE origin", got)
	}
	if got := header.Get("ETag"); got != `"upstream"` {
		t.Fatalf("ETag = %q, want preserved", got)
	}
}
