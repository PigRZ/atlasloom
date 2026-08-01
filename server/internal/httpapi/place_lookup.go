// 地点查询通过服务端高德 Web Service Key 返回可直接保存的地址与坐标。
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
	"sync"
	"time"

	"my-travel/server/internal/app"
)

const (
	amapPlaceSearchEndpoint = "https://restapi.amap.com/v3/place/text"
	amapReverseGeoEndpoint  = "https://restapi.amap.com/v3/geocode/regeo"
)

type placeLookupProxy struct {
	key             string
	client          *http.Client
	searchEndpoint  string
	reverseEndpoint string
	requestMu       sync.Mutex
	nextRequest     time.Time
}

type placeCandidate struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	Adcode    string  `json:"adcode,omitempty"`
	Region    string  `json:"region,omitempty"`
}

type reverseGeoResult struct {
	Address string `json:"address"`
	Adcode  string `json:"adcode,omitempty"`
	Region  string `json:"region,omitempty"`
}

type amapPlaceEnvelope struct {
	Status   string `json:"status"`
	Info     string `json:"info"`
	Infocode string `json:"infocode"`
	POIs     []struct {
		ID       string         `json:"id"`
		Name     string         `json:"name"`
		Address  flexibleString `json:"address"`
		Location string         `json:"location"`
		Adcode   string         `json:"adcode"`
		Province flexibleString `json:"pname"`
		City     flexibleString `json:"cityname"`
		District flexibleString `json:"adname"`
	} `json:"pois"`
}

type amapReverseEnvelope struct {
	Status    string `json:"status"`
	Info      string `json:"info"`
	Infocode  string `json:"infocode"`
	Regeocode struct {
		Address   string `json:"formatted_address"`
		Component struct {
			Adcode   string         `json:"adcode"`
			Province flexibleString `json:"province"`
			City     flexibleString `json:"city"`
			District flexibleString `json:"district"`
		} `json:"addressComponent"`
	} `json:"regeocode"`
}

type flexibleString string

func (value *flexibleString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = flexibleString(text)
		return nil
	}
	var parts []string
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	*value = flexibleString(strings.Join(parts, ""))
	return nil
}

func newPlaceLookupProxy(config app.Config) *placeLookupProxy {
	return &placeLookupProxy{
		key: config.AmapWebKey, client: &http.Client{Timeout: 8 * time.Second},
		searchEndpoint: amapPlaceSearchEndpoint, reverseEndpoint: amapReverseGeoEndpoint,
	}
}

func (s *Server) placeSearch(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("keywords"))
	if keyword == "" || len([]rune(keyword)) > 80 {
		problem(w, http.StatusBadRequest, "invalid-place-keyword", "请输入不超过 80 个字的地点关键词。", nil)
		return
	}
	if s.places == nil || s.places.key == "" {
		problem(w, http.StatusServiceUnavailable, "amap-place-not-configured", "尚未配置高德地点服务。", nil)
		return
	}
	candidates, err := s.places.search(r.Context(), keyword)
	if err != nil {
		problem(w, http.StatusBadGateway, "amap-place-unavailable", "地点搜索暂时不可用，请稍后重试。", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

func (s *Server) reverseGeocode(w http.ResponseWriter, r *http.Request) {
	longitude, latitude, err := parseLocation(r.URL.Query().Get("location"))
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid-location", "坐标格式不正确。", nil)
		return
	}
	if s.places == nil || s.places.key == "" {
		problem(w, http.StatusServiceUnavailable, "amap-place-not-configured", "尚未配置高德地点服务。", nil)
		return
	}
	result, err := s.places.reverse(r.Context(), longitude, latitude)
	if err != nil {
		problem(w, http.StatusBadGateway, "amap-place-unavailable", "详细地址暂时无法确认，请稍后重试。", nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (proxy *placeLookupProxy) search(
	ctx context.Context,
	keyword string,
) ([]placeCandidate, error) {
	var envelope amapPlaceEnvelope
	query := url.Values{
		"key": {proxy.key}, "keywords": {keyword}, "offset": {"8"},
		"page": {"1"}, "extensions": {"base"}, "output": {"JSON"},
	}
	if err := proxy.getJSON(ctx, proxy.searchEndpoint, query, &envelope); err != nil {
		return nil, err
	}
	if envelope.Status != "1" {
		return nil, fmt.Errorf("amap place rejected request: %s (%s)", envelope.Info, envelope.Infocode)
	}
	candidates := make([]placeCandidate, 0, len(envelope.POIs))
	for _, poi := range envelope.POIs {
		longitude, latitude, err := parseLocation(poi.Location)
		if err != nil || strings.TrimSpace(poi.Name) == "" {
			continue
		}
		candidates = append(candidates, placeCandidate{
			ID: poi.ID, Name: poi.Name, Address: string(poi.Address),
			Longitude: longitude, Latitude: latitude, Adcode: poi.Adcode,
			Region: joinRegion(string(poi.Province), string(poi.City), string(poi.District)),
		})
	}
	return candidates, nil
}

func (proxy *placeLookupProxy) reverse(
	ctx context.Context,
	longitude, latitude float64,
) (reverseGeoResult, error) {
	var envelope amapReverseEnvelope
	query := url.Values{
		"key": {proxy.key},
		"location": {
			strconv.FormatFloat(longitude, 'f', 6, 64) + "," +
				strconv.FormatFloat(latitude, 'f', 6, 64),
		},
		"extensions": {"base"}, "output": {"JSON"},
	}
	if err := proxy.getJSON(ctx, proxy.reverseEndpoint, query, &envelope); err != nil {
		return reverseGeoResult{}, err
	}
	if envelope.Status != "1" || strings.TrimSpace(envelope.Regeocode.Address) == "" {
		return reverseGeoResult{}, fmt.Errorf("amap reverse geocode rejected request: %s (%s)", envelope.Info, envelope.Infocode)
	}
	component := envelope.Regeocode.Component
	return reverseGeoResult{
		Address: envelope.Regeocode.Address,
		Adcode:  component.Adcode,
		Region: joinRegion(
			string(component.Province),
			string(component.City),
			string(component.District),
		),
	}, nil
}

func (proxy *placeLookupProxy) getJSON(
	ctx context.Context,
	endpoint string,
	query url.Values,
	target any,
) error {
	proxy.requestMu.Lock()
	if delay := time.Until(proxy.nextRequest); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			proxy.requestMu.Unlock()
			return ctx.Err()
		case <-timer.C:
		}
	}
	proxy.nextRequest = time.Now().Add(150 * time.Millisecond)
	proxy.requestMu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	response, err := proxy.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("amap place status %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target)
}

func parseLocation(raw string) (float64, float64, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid location")
	}
	longitude, longitudeErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	latitude, latitudeErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if longitudeErr != nil || latitudeErr != nil ||
		longitude < 73 || longitude > 136 || latitude < 3 || latitude > 54 {
		return 0, 0, fmt.Errorf("invalid location")
	}
	return longitude, latitude, nil
}

func joinRegion(parts ...string) string {
	result := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.Contains(result, part) {
			result += part
		}
	}
	return result
}
