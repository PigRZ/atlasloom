// 这些测试固定高德代理的路径白名单、密钥注入和敏感参数覆盖行为。
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"my-travel/server/internal/app"
)

func TestAllowedAmapPathOnlyAcceptsRequiredMapServices(t *testing.T) {
	if _, ok := allowedAmapPath([]string{"v5", "direction", "driving"}); !ok {
		t.Fatal("valid AMap route path was rejected")
	}
	if _, ok := allowedAmapPath([]string{"v3", "direction", "transit", "integrated"}); !ok {
		t.Fatal("valid AMap transit path was rejected")
	}
	if _, ok := allowedAmapPath([]string{"v3", "place", "text"}); !ok {
		t.Fatal("valid AMap place search path was rejected")
	}
	if _, ok := allowedAmapPath([]string{"v3", "geocode", "regeo"}); !ok {
		t.Fatal("valid AMap reverse geocode path was rejected")
	}
	for _, parts := range [][]string{
		{"v6", "direction", "driving"},
		{"v5", "..", "admin"},
		{"v5"},
		{"v3", "place", "around"},
		{"v5", "geocode", "regeo"},
	} {
		if target, ok := allowedAmapPath(parts); ok {
			t.Fatalf("unsafe path accepted: %s", target)
		}
	}
}

func TestAmapServiceRouteRequiresSession(t *testing.T) {
	server := &Server{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/_AMapService/v3/direction/driving?origin=1,1&destination=2,2",
		nil,
	)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestAmapProxyAppendsServerSecurityCode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v5/direction/driving" {
			t.Fatalf("path=%s", got)
		}
		if got := r.URL.Query().Get("jscode"); got != "server-only" {
			t.Fatalf("jscode=%s", got)
		}
		if got := r.URL.Query().Get("origin"); got != "102.1,30.2" {
			t.Fatalf("origin=%s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"1"}`))
	}))
	defer upstream.Close()

	proxy := &amapServiceProxy{
		securityCode: "server-only",
		client:       &http.Client{Timeout: time.Second},
		endpoint:     upstream.URL,
	}
	response, err := proxy.forward(
		context.Background(),
		http.MethodGet,
		"/v5/direction/driving",
		url.Values{"origin": {"102.1,30.2"}, "jscode": {"browser-value"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestMapConfigOnlyReturnsPublicBrowserConfiguration(t *testing.T) {
	server := &Server{config: app.Config{
		AmapJSAPIKey:   "public-web-key",
		AmapJSSecurity: "server-only",
	}}
	recorder := httptest.NewRecorder()

	server.mapConfig(recorder)

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["enabled"] != true || payload["jsapiKey"] != "public-web-key" {
		t.Fatalf("payload=%v", payload)
	}
	if _, leaked := payload["securityCode"]; leaked {
		t.Fatal("security code leaked in map config")
	}
}
