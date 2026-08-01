// Web 进程托管 React SPA，并为没有外层 Nginx 的本地容器提供同源 API 代理。
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	address := getenv("WEB_LISTEN_ADDR", "127.0.0.1:8501")
	basePath := "/" + strings.Trim(getenv("APP_BASE_PATH", "travel"), "/")
	distDir := getenv("WEB_DIST_DIR", "/srv/web")
	index := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		log.Fatalf("React build is unavailable at %s: %v", index, err)
	}
	handler, err := newWebHandler(
		basePath,
		distDir,
		getenv("API_UPSTREAM_URL", "http://127.0.0.1:8502"),
	)
	if err != nil {
		log.Fatalf("API upstream is invalid: %v", err)
	}
	log.Printf("React web listens on %s%s", address, basePath)
	log.Fatal(http.ListenAndServe(address, handler))
}

func newWebHandler(basePath, distDir, apiUpstream string) (http.Handler, error) {
	target, err := url.Parse(apiUpstream)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, fmt.Errorf("API_UPSTREAM_URL must be an absolute HTTP URL")
	}
	basePath = "/" + strings.Trim(basePath, "/")
	index := filepath.Join(distDir, "index.html")
	files := http.FileServer(http.Dir(distDir))
	apiPrefix := basePath + "/api"
	upstreamProxy := httputil.NewSingleHostReverseProxy(target)
	apiProxy := http.StripPrefix(apiPrefix, upstreamProxy)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setBrowserSecurityHeaders(w)
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_AMapService/") {
			upstreamProxy.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, apiPrefix+"/") {
			w.Header().Set("Cache-Control", "no-store")
			apiProxy.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == basePath || r.URL.Path == basePath+"/" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, index)
			return
		}
		if !strings.HasPrefix(r.URL.Path, basePath+"/") {
			http.NotFound(w, r)
			return
		}
		relative := strings.TrimPrefix(r.URL.Path, basePath+"/")
		if strings.Contains(relative, "..") {
			http.NotFound(w, r)
			return
		}
		candidate := filepath.Join(distDir, relative)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if strings.HasPrefix(relative, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			r.URL.Path = "/" + relative
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
	return handler, nil
}

func setBrowserSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set(
		"Permissions-Policy",
		"camera=(), microphone=(), geolocation=(self)",
	)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
