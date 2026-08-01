// 这些测试固定所有 HTTP 错误响应都可通过稳定请求 ID 追踪。
package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProblemIncludesStableRequestID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("X-Request-Id", "test-request-123")
	recorder := httptest.NewRecorder()
	(&Server{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
	if recorder.Header().Get("X-Request-Id") != "test-request-123" {
		t.Fatal("request id was not preserved")
	}
	for key, expected := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if recorder.Header().Get(key) != expected {
			t.Fatalf("%s=%q want %q", key, recorder.Header().Get(key), expected)
		}
	}
	if !strings.Contains(recorder.Body.String(), `"requestId":"test-request-123"`) {
		t.Fatalf("problem body lacks request id: %s", recorder.Body.String())
	}
}

func TestIdempotencyKeyRequiresCollisionResistantLength(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/trips", nil)
	request.Header.Set("Idempotency-Key", "short")
	recorder := httptest.NewRecorder()
	if _, ok := (&Server{}).idempotencyKey(recorder, request); ok {
		t.Fatal("short idempotency key was accepted")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestDecodeRejectsTrailingJSONValue(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/trips",
		bytes.NewBufferString(`{"id":"a"}{"id":"b"}`),
	)
	recorder := httptest.NewRecorder()
	var target map[string]any
	if decode(recorder, request, &target) {
		t.Fatal("multiple JSON values were accepted")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestTripCreateInputRejectsReadOnlyFields(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/trips",
		bytes.NewBufferString(
			`{"id":"trip-a","title":"旅行","startDate":"2026-09-01","endDate":"2026-09-02","version":1}`,
		),
	)
	recorder := httptest.NewRecorder()
	var target tripCreateInput
	if decode(recorder, request, &target) {
		t.Fatal("Trip 创建请求接受了服务端只读字段")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProvisionUserRouteRejectsMissingProvisionToken(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/users",
		bytes.NewBufferString(
			`{"displayName":"同行成员","mobile":"13800138000","password":"long-enough-password"}`,
		),
	)
	recorder := httptest.NewRecorder()
	(&Server{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProfileRouteRequiresSession(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/profile", nil)
	recorder := httptest.NewRecorder()
	(&Server{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminTripManagementIsLimitedToExportAndDelete(t *testing.T) {
	for name, test := range map[string]struct {
		admin  bool
		method string
		parts  []string
		want   bool
	}{
		"admin delete":       {true, http.MethodDelete, []string{"trips", "trip-a"}, true},
		"admin export":       {true, http.MethodGet, []string{"trips", "trip-a", "backup"}, true},
		"member delete":      {false, http.MethodDelete, []string{"trips", "trip-a"}, false},
		"admin read":         {true, http.MethodGet, []string{"trips", "trip-a"}, false},
		"admin edit":         {true, http.MethodPatch, []string{"trips", "trip-a"}, false},
		"admin snapshots":    {true, http.MethodGet, []string{"trips", "trip-a", "snapshots"}, false},
		"admin backup write": {true, http.MethodPost, []string{"trips", "trip-a", "backup"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isAdminTripManagementRoute(test.admin, test.method, test.parts); got != test.want {
				t.Fatalf("got=%v want=%v", got, test.want)
			}
		})
	}
}
