// 导航路线由服务端使用 Web Service Key 查询并返回可绘制的坐标序列。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type routePlanResponse struct {
	DistanceMeters  int64        `json:"distanceMeters"`
	DurationSeconds int64        `json:"durationSeconds"`
	Path            [][2]float64 `json:"path"`
}

type amapRouteEnvelope struct {
	Status  string              `json:"status"`
	Info    string              `json:"info"`
	ErrCode int                 `json:"errcode"`
	ErrMsg  string              `json:"errmsg"`
	Route   amapRouteCollection `json:"route"`
	Data    amapRouteCollection `json:"data"`
}

type amapRouteCollection struct {
	Paths []amapRoutePath `json:"paths"`
}

type amapRoutePath struct {
	Distance flexibleNumber `json:"distance"`
	Duration flexibleNumber `json:"duration"`
	Steps    []struct {
		Polyline string `json:"polyline"`
	} `json:"steps"`
}

type flexibleNumber float64

func (value *flexibleNumber) UnmarshalJSON(data []byte) error {
	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*value = flexibleNumber(number)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*value = flexibleNumber(parsed)
	return nil
}

func (s *Server) routePlan(w http.ResponseWriter, r *http.Request) {
	if s.amap == nil || s.config.AmapWebKey == "" {
		problem(w, http.StatusServiceUnavailable, "amap-route-not-configured", "尚未配置高德导航服务。", nil)
		return
	}
	mode, targetPath, ok := routeEndpoint(r.URL.Query().Get("mode"))
	if !ok {
		problem(w, http.StatusBadRequest, "invalid-route-mode", "该交通方式不支持导航路线。", nil)
		return
	}
	origin, err := normalizedRouteLocation(r.URL.Query().Get("origin"))
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid-route-origin", "导航起点坐标不正确。", nil)
		return
	}
	destination, err := normalizedRouteLocation(r.URL.Query().Get("destination"))
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid-route-destination", "导航终点坐标不正确。", nil)
		return
	}
	plan, err := s.fetchRoutePlan(r.Context(), targetPath, mode, origin, destination)
	if err != nil {
		problem(w, http.StatusBadGateway, "amap-route-unavailable", "导航路线暂时无法规划。", nil)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) fetchRoutePlan(
	ctx context.Context,
	targetPath, mode, origin, destination string,
) (routePlanResponse, error) {
	query := url.Values{
		"key":         {s.config.AmapWebKey},
		"origin":      {origin},
		"destination": {destination},
		"output":      {"JSON"},
	}
	if mode != "bicycle" {
		query.Set("extensions", "base")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(s.amap.endpoint, "/")+targetPath+"?"+query.Encode(),
		nil,
	)
	if err != nil {
		return routePlanResponse{}, err
	}
	response, err := s.amap.client.Do(request)
	if err != nil {
		return routePlanResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return routePlanResponse{}, fmt.Errorf("amap route status %d", response.StatusCode)
	}
	var envelope amapRouteEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return routePlanResponse{}, err
	}
	paths := envelope.Route.Paths
	if mode == "bicycle" {
		if envelope.ErrCode != 0 {
			return routePlanResponse{}, fmt.Errorf("amap route rejected request: %s", envelope.ErrMsg)
		}
		paths = envelope.Data.Paths
	} else if envelope.Status != "1" {
		return routePlanResponse{}, fmt.Errorf("amap route rejected request: %s", envelope.Info)
	}
	if len(paths) == 0 {
		return routePlanResponse{}, fmt.Errorf("amap route returned no path")
	}
	path := routeCoordinates(paths[0])
	if len(path) < 2 {
		return routePlanResponse{}, fmt.Errorf("amap route returned no drawable path")
	}
	return routePlanResponse{
		DistanceMeters:  int64(paths[0].Distance),
		DurationSeconds: int64(paths[0].Duration),
		Path:            path,
	}, nil
}

func routeEndpoint(mode string) (string, string, bool) {
	switch strings.TrimSpace(mode) {
	case "drive", "taxi", "rideshare":
		return "drive", "/v3/direction/driving", true
	case "walk":
		return "walk", "/v3/direction/walking", true
	case "bicycle":
		return "bicycle", "/v4/direction/bicycling", true
	default:
		return "", "", false
	}
}

func normalizedRouteLocation(raw string) (string, error) {
	longitude, latitude, err := parseLocation(raw)
	if err != nil {
		return "", err
	}
	return strconv.FormatFloat(longitude, 'f', 6, 64) + "," +
		strconv.FormatFloat(latitude, 'f', 6, 64), nil
}

func routeCoordinates(path amapRoutePath) [][2]float64 {
	result := make([][2]float64, 0)
	for _, step := range path.Steps {
		for _, raw := range strings.Split(step.Polyline, ";") {
			longitude, latitude, err := parseLocation(raw)
			if err != nil {
				continue
			}
			point := [2]float64{longitude, latitude}
			if len(result) == 0 || result[len(result)-1] != point {
				result = append(result, point)
			}
		}
	}
	return result
}
