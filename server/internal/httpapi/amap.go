// 本文件只代理允许的高德 JSAPI 服务路径，安全密钥始终留在服务端。
package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"my-travel/server/internal/app"
)

const amapServiceEndpoint = "https://restapi.amap.com"

type amapServiceProxy struct {
	securityCode string
	client       *http.Client
	endpoint     string
}

func newAmapServiceProxy(config app.Config) *amapServiceProxy {
	return &amapServiceProxy{
		securityCode: config.AmapJSSecurity,
		client:       &http.Client{Timeout: 8 * time.Second},
		endpoint:     amapServiceEndpoint,
	}
}

func (s *Server) mapConfig(w http.ResponseWriter) {
	enabled := s.config.AmapJSAPIKey != "" && s.config.AmapJSSecurity != ""
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  enabled,
		"jsapiKey": s.config.AmapJSAPIKey,
	})
}

func (s *Server) amapServiceRoute(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireSession(w, r, false); !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/_AMapService/")
	s.amapService(w, r, strings.Split(strings.Trim(path, "/"), "/"))
}

func (s *Server) amapService(w http.ResponseWriter, r *http.Request, parts []string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		problem(w, http.StatusMethodNotAllowed, "method-not-allowed", "高德代理只支持读取操作。", nil)
		return
	}
	if s.amap == nil || s.amap.securityCode == "" {
		problem(w, http.StatusServiceUnavailable, "amap-jsapi-not-configured", "尚未配置高德地图服务。", nil)
		return
	}
	targetPath, ok := allowedAmapPath(parts)
	if !ok {
		problem(w, http.StatusBadRequest, "invalid-amap-path", "不允许访问该高德服务路径。", nil)
		return
	}
	response, err := s.amap.forward(r.Context(), r.Method, targetPath, r.URL.Query())
	if err != nil {
		problem(w, http.StatusBadGateway, "amap-jsapi-unavailable", "暂时无法连接高德地图服务。", nil)
		return
	}
	defer response.Body.Close()
	for _, header := range []string{"Content-Type", "Cache-Control", "Expires"} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 8<<20))
}

func allowedAmapPath(parts []string) (string, bool) {
	candidate := "/" + strings.Join(parts, "/")
	allowed := map[string]struct{}{
		"/v3/direction/driving":            {},
		"/v3/direction/walking":            {},
		"/v3/direction/transit/integrated": {},
		"/v5/direction/driving":            {},
		"/v5/direction/walking":            {},
		"/v3/place/text":                   {},
		"/v3/geocode/regeo":                {},
	}
	_, ok := allowed[candidate]
	return candidate, ok
}

func (proxy *amapServiceProxy) forward(
	ctx context.Context,
	method string,
	targetPath string,
	query url.Values,
) (*http.Response, error) {
	if proxy == nil || proxy.securityCode == "" {
		return nil, fmt.Errorf("AMap security code is missing")
	}
	safeQuery := url.Values{}
	for key, values := range query {
		if strings.EqualFold(key, "jscode") {
			continue
		}
		for _, value := range values {
			safeQuery.Add(key, value)
		}
	}
	safeQuery.Set("jscode", proxy.securityCode)
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		strings.TrimRight(proxy.endpoint, "/")+targetPath+"?"+safeQuery.Encode(),
		nil,
	)
	if err != nil {
		return nil, err
	}
	return proxy.client.Do(request)
}
