package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestWebHandlerProxiesSameOriginAPIRequest(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/trips/trip-1" {
			t.Errorf("upstream path=%q", r.URL.Path)
		}
		if r.URL.RawQuery != "date=2026-09-01&view=today" {
			t.Errorf("upstream query=%q", r.URL.RawQuery)
		}
		cookie, err := r.Cookie("travel_session")
		if err != nil || cookie.Value != "session-1" {
			t.Errorf("upstream cookie=%v err=%v", cookie, err)
		}
		w.Header().Set("Set-Cookie", "travel_session=renewed; Path=/; HttpOnly")
		w.WriteHeader(http.StatusTeapot)
		_, _ = fmt.Fprint(w, "proxied")
	}))
	defer upstream.Close()

	handler, err := newWebHandler("/travel", testDistDir(t), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/travel/api/trips/trip-1?date=2026-09-01&view=today",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: "travel_session", Value: "session-1"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTeapot || recorder.Body.String() != "proxied" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Set-Cookie") == "" {
		t.Fatal("upstream response cookie was not forwarded")
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
}

func TestWebHandlerProxiesFirstLevelAmapServiceRequest(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/_AMapService/v3/direction/driving" {
			t.Errorf("upstream path=%q", r.URL.Path)
		}
		if r.URL.RawQuery != "origin=1,1&destination=2,2" {
			t.Errorf("upstream query=%q", r.URL.RawQuery)
		}
		if _, err := r.Cookie("travel_session"); err != nil {
			t.Errorf("upstream cookie err=%v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	handler, err := newWebHandler("/travel", testDistDir(t), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/_AMapService/v3/direction/driving?origin=1,1&destination=2,2",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: "travel_session", Value: "session-1"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
}

func TestWebHandlerKeepsNonAPIRequestsInSPA(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer upstream.Close()

	handler, err := newWebHandler("/travel", testDistDir(t), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/travel/day/2026-09-01", nil),
	)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "spa" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatalf("non-API request reached upstream %d times", calls.Load())
	}
	if recorder.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("SPA cache policy=%q", recorder.Header().Get("Cache-Control"))
	}
	assertBrowserSecurityHeaders(t, recorder.Header())
}

func TestWebHandlerKeepsIndependentHealthEndpoint(t *testing.T) {
	handler, err := newWebHandler("/travel", testDistDir(t), "http://127.0.0.1:8502")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"status":"ok"}` {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health cache policy=%q", recorder.Header().Get("Cache-Control"))
	}
	assertBrowserSecurityHeaders(t, recorder.Header())
}

func TestWebHandlerCachesFingerprintAssetsImmutably(t *testing.T) {
	distDir := testDistDir(t)
	assetDir := filepath.Join(distDir, "assets")
	if err := os.Mkdir(assetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(assetDir, "app-a1b2c3.js"),
		[]byte("export {};"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebHandler(
		"/travel",
		distDir,
		"http://127.0.0.1:8502",
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/travel/assets/app-a1b2c3.js",
			nil,
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") !=
		"public, max-age=31536000, immutable" {
		t.Fatalf("asset cache policy=%q", recorder.Header().Get("Cache-Control"))
	}
	assertBrowserSecurityHeaders(t, recorder.Header())
}

func assertBrowserSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	for key, expected := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=(self)",
	} {
		if header.Get(key) != expected {
			t.Fatalf("%s=%q want %q", key, header.Get(key), expected)
		}
	}
}

func testDistDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("spa"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
