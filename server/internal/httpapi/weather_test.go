// 这些测试固定天气代理的输入校验、缓存命中与上游失败边界。
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWeatherProxyCachesForecastWithoutExposingKey(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("key") != "secret" || r.URL.Query().Get("extensions") != "all" {
			t.Fatal("missing protected upstream parameters")
		}
		_, _ = w.Write([]byte(`{"status":"1","info":"OK","infocode":"10000","forecasts":[{"city":"示例市","adcode":"110101","province":"示例省","reporttime":"2026-07-24 18:00:00","casts":[{"date":"2026-07-24","dayweather":"晴","nightweather":"多云","daytemp":"30","nighttemp":"22","daywind":"南","nightwind":"南","daypower":"≤3","nightpower":"≤3"}]}]}`))
	}))
	defer upstream.Close()
	proxy := &weatherProxy{key: "secret", ttl: time.Minute, client: upstream.Client(), endpoint: upstream.URL, cache: map[string]weatherCacheEntry{}}
	first, err := proxy.get(context.Background(), "110101", false)
	if err != nil || first.City != "示例市" || first.Cached {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := proxy.get(context.Background(), "110101", false)
	if err != nil || !second.Cached || calls.Load() != 1 {
		t.Fatalf("second=%+v calls=%d err=%v", second, calls.Load(), err)
	}
	refreshed, err := proxy.get(context.Background(), "110101", true)
	if err != nil || refreshed.Cached || calls.Load() != 2 {
		t.Fatalf("refreshed=%+v calls=%d err=%v", refreshed, calls.Load(), err)
	}
}

func TestWeatherProxyRetriesRecoverableQPSLimit(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"status":"0","info":"CUQPS_HAS_EXCEEDED_THE_LIMIT","infocode":"10021","forecasts":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"1","info":"OK","infocode":"10000","forecasts":[{"city":"测试市","adcode":"110102","province":"示例省","reporttime":"2026-07-24 18:00:00","casts":[]}]}`))
	}))
	defer upstream.Close()
	proxy := &weatherProxy{
		key: "secret", ttl: time.Minute, client: upstream.Client(),
		endpoint: upstream.URL, cache: map[string]weatherCacheEntry{},
	}
	result, err := proxy.get(context.Background(), "110102", true)
	if err != nil || result.City != "测试市" || calls.Load() != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestWeatherProxyKeepsExpiredCacheWhenRefreshFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer upstream.Close()
	proxy := &weatherProxy{
		key: "secret", ttl: time.Minute, client: upstream.Client(),
		endpoint: upstream.URL,
		cache: map[string]weatherCacheEntry{
			"110101": {
				payload: weatherResponse{City: "示例市", Adcode: "110101", Freshness: "fresh"},
				expires: time.Now().Add(-time.Minute),
			},
		},
	}
	result, err := proxy.get(context.Background(), "110101", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Freshness != "stale" || !result.Cached {
		t.Fatalf("stale cache was not returned: %+v", result)
	}
}
