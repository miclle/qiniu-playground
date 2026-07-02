//go:build development

package website

import (
	"embed"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
)

//go:embed public/*
var embedFS embed.FS

var (
	origin      *url.URL
	proxyRoutes = []string{"/", "static/*filepath"}
)

func init() {
	var err error
	origin, err = url.Parse(devServerURLFromEnvironment())
	if err != nil {
		fmt.Printf("Fail to parse url: %+v", err)
		os.Exit(1)
	}

	entries, err := embedFS.ReadDir("public")
	if err != nil {
		fmt.Printf("Fail to read public dir: %+v", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fp := entry.Name()
		proxyRoutes = append(proxyRoutes, fp)
	}
}

func devServerURLFromEnvironment() string {
	if devServerURL := os.Getenv("QINIU_PLAYGROUND_VITE_DEV_SERVER_URL"); devServerURL != "" {
		return devServerURL
	}
	port := os.Getenv("QINIU_PLAYGROUND_VITE_PORT")
	if port == "" {
		port = "19173"
	}
	return fmt.Sprintf("http://localhost:%s", port)
}

// EmbedAssets proxies requests to the Vite dev server in development mode.
func EmbedAssets(router *fox.Engine) {
	director := func(req *http.Request) {
		req.Header.Add("X-Forwarded-Host", req.Host)
		req.Header.Add("X-Origin-Host", origin.Host)
		req.URL.Scheme = origin.Scheme
		req.URL.Host = origin.Host
	}

	proxy := &httputil.ReverseProxy{
		Director: director,
	}

	proxyHandler := func(c *fox.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}

	for _, path := range proxyRoutes {
		router.GET(path, proxyHandler)
		router.HEAD(path, proxyHandler)
	}

	router.NotFound(func(c *fox.Context) any {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return httperrors.ErrNotFound
		}

		if isAPIRoute(c.Request.URL.Path) {
			return httperrors.ErrNotFound
		}

		c.Logger.Debugf("NotFound, use proxy: %s", c.Request.URL)
		proxy.ServeHTTP(c.Writer, c.Request)

		return nil
	})
}
