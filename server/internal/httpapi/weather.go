// 天气代理按行政区划码缓存高德四日预报，且永不回写旅行计划。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"my-travel/server/internal/app"
)

const amapWeatherEndpoint = "https://restapi.amap.com/v3/weather/weatherInfo"

var adcodePattern = regexp.MustCompile(`^\d{6}$`)

type weatherProxy struct {
	key         string
	ttl         time.Duration
	minInterval time.Duration
	client      *http.Client
	endpoint    string
	mu          sync.Mutex
	cache       map[string]weatherCacheEntry
	requestMu   sync.Mutex
	nextRequest time.Time
}

type weatherCacheEntry struct {
	payload weatherResponse
	expires time.Time
}

type weatherResponse struct {
	City         string        `json:"city"`
	Adcode       string        `json:"adcode"`
	Province     string        `json:"province"`
	ReportedAt   string        `json:"reportedAt"`
	FetchedAt    time.Time     `json:"fetchedAt"`
	ExpiresAt    time.Time     `json:"expiresAt"`
	Freshness    string        `json:"freshness"`
	RefreshError string        `json:"refreshError,omitempty"`
	Forecasts    []weatherCast `json:"forecasts"`
	Cached       bool          `json:"cached"`
}

type weatherCast struct {
	Date         string `json:"date"`
	DayWeather   string `json:"dayWeather"`
	NightWeather string `json:"nightWeather"`
	DayTemp      string `json:"dayTemp"`
	NightTemp    string `json:"nightTemp"`
	DayWind      string `json:"dayWind"`
	NightWind    string `json:"nightWind"`
	DayPower     string `json:"dayPower"`
	NightPower   string `json:"nightPower"`
}

type amapWeatherEnvelope struct {
	Status    string `json:"status"`
	Info      string `json:"info"`
	Infocode  string `json:"infocode"`
	Forecasts []struct {
		City       string        `json:"city"`
		Adcode     string        `json:"adcode"`
		Province   string        `json:"province"`
		ReportTime string        `json:"reporttime"`
		Casts      []weatherCast `json:"casts"`
	} `json:"forecasts"`
}

func newWeatherProxy(config app.Config) *weatherProxy {
	return &weatherProxy{
		key: config.AmapWebKey, ttl: config.WeatherCacheTTL, minInterval: 400 * time.Millisecond,
		client: &http.Client{Timeout: 8 * time.Second}, endpoint: amapWeatherEndpoint,
		cache: map[string]weatherCacheEntry{},
	}
}

func (s *Server) weatherForecast(w http.ResponseWriter, r *http.Request) {
	if s.weather == nil || s.weather.key == "" {
		problem(w, http.StatusServiceUnavailable, "amap-weather-not-configured", "尚未配置高德天气服务。", nil)
		return
	}
	adcode := r.URL.Query().Get("adcode")
	if !adcodePattern.MatchString(adcode) {
		problem(w, http.StatusBadRequest, "invalid-adcode", "天气区域代码必须为六位数字。", map[string]string{"adcode": "请输入高德六位行政区划代码"})
		return
	}
	result, err := s.weather.get(r.Context(), adcode, r.URL.Query().Get("refresh") == "true")
	if err != nil {
		problem(w, http.StatusBadGateway, "amap-weather-unavailable", "天气暂时无法刷新，请稍后重试。", nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (proxy *weatherProxy) get(ctx context.Context, adcode string, refresh bool) (weatherResponse, error) {
	proxy.mu.Lock()
	entry, ok := proxy.cache[adcode]
	if !refresh && ok && time.Now().Before(entry.expires) {
		entry.payload.Cached = true
		entry.payload.Freshness = "fresh"
		proxy.mu.Unlock()
		return entry.payload, nil
	}
	proxy.mu.Unlock()

	var envelope amapWeatherEnvelope
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := proxy.waitForRequestSlot(ctx); err != nil {
			lastErr = err
			break
		}
		query := url.Values{"city": {adcode}, "extensions": {"all"}, "output": {"JSON"}, "key": {proxy.key}}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.endpoint+"?"+query.Encode(), nil)
		if err != nil {
			return weatherResponse{}, err
		}
		response, err := proxy.client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			lastErr = fmt.Errorf("amap weather status %d", response.StatusCode)
			if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
				continue
			}
			break
		}
		envelope = amapWeatherEnvelope{}
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope)
		response.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if envelope.Status == "1" && len(envelope.Forecasts) > 0 {
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("amap weather rejected request: %s (%s)", envelope.Info, envelope.Infocode)
		if envelope.Info != "CUQPS_HAS_EXCEEDED_THE_LIMIT" {
			break
		}
	}
	if lastErr != nil || len(envelope.Forecasts) == 0 {
		if ok {
			entry.payload.Cached = true
			entry.payload.Freshness = "stale"
			entry.payload.RefreshError = "weather refresh failed; showing the last cached forecast"
			return entry.payload, nil
		}
		if lastErr != nil {
			return weatherResponse{}, lastErr
		}
		return weatherResponse{}, fmt.Errorf("amap weather returned no forecast")
	}
	forecast := envelope.Forecasts[0]
	fetchedAt := time.Now().UTC()
	expiresAt := fetchedAt.Add(proxy.ttl)
	result := weatherResponse{
		City: forecast.City, Adcode: forecast.Adcode, Province: forecast.Province,
		ReportedAt: forecast.ReportTime, FetchedAt: fetchedAt, ExpiresAt: expiresAt,
		Freshness: "fresh", Forecasts: forecast.Casts,
	}
	proxy.mu.Lock()
	proxy.cache[adcode] = weatherCacheEntry{payload: result, expires: expiresAt}
	proxy.mu.Unlock()
	return result, nil
}

func (proxy *weatherProxy) waitForRequestSlot(ctx context.Context) error {
	proxy.requestMu.Lock()
	defer proxy.requestMu.Unlock()
	if delay := time.Until(proxy.nextRequest); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	proxy.nextRequest = time.Now().Add(proxy.minInterval)
	return nil
}
