package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"my-travel/server/internal/app"
)

func TestRouteEndpointOnlyAllowsDrawableModes(t *testing.T) {
	for mode, expectedPath := range map[string]string{
		"drive":     "/v3/direction/driving",
		"taxi":      "/v3/direction/driving",
		"rideshare": "/v3/direction/driving",
		"walk":      "/v3/direction/walking",
		"bicycle":   "/v4/direction/bicycling",
	} {
		if _, path, ok := routeEndpoint(mode); !ok || path != expectedPath {
			t.Errorf("routeEndpoint(%q)=(%q,%v), want %q,true", mode, path, ok, expectedPath)
		}
	}
	if _, _, ok := routeEndpoint("train"); ok {
		t.Fatal("train must remain a schematic route")
	}
}

func TestRouteCoordinatesParsesAndDeduplicatesPolyline(t *testing.T) {
	path := amapRoutePath{}
	path.Steps = append(path.Steps, struct {
		Polyline string `json:"polyline"`
	}{Polyline: "104.100000,30.100000;104.200000,30.200000"})
	path.Steps = append(path.Steps, struct {
		Polyline string `json:"polyline"`
	}{Polyline: "104.200000,30.200000;104.300000,30.300000"})
	got := routeCoordinates(path)
	if len(got) != 3 || got[2] != [2]float64{104.3, 30.3} {
		t.Fatalf("routeCoordinates=%v", got)
	}
}

func TestFlexibleNumberAcceptsStringAndNumber(t *testing.T) {
	for _, raw := range []string{`120`, `"120"`} {
		var value flexibleNumber
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if value != 120 {
			t.Fatalf("value=%v", value)
		}
	}
}

func TestFetchRoutePlanNormalizesDrivingAndBicycleResponses(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     string
		path     string
		response string
	}{
		{
			name: "driving", mode: "drive", path: "/v3/direction/driving",
			response: `{"status":"1","route":{"paths":[{"distance":"1200","duration":"300","steps":[{"polyline":"104.1,30.1;104.2,30.2"}]}]}}`,
		},
		{
			name: "bicycle", mode: "bicycle", path: "/v4/direction/bicycling",
			response: `{"errcode":0,"data":{"paths":[{"distance":900,"duration":240,"steps":[{"polyline":"104.1,30.1;104.2,30.2"}]}]}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path || r.URL.Query().Get("key") != "secret" {
					t.Fatalf("request=%s?%s", r.URL.Path, r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer upstream.Close()

			server := &Server{
				config: app.Config{AmapWebKey: "secret"},
				amap: &amapServiceProxy{
					client: upstream.Client(), endpoint: upstream.URL,
				},
			}
			plan, err := server.fetchRoutePlan(
				context.Background(), test.path, test.mode, "104.1,30.1", "104.2,30.2",
			)
			if err != nil {
				t.Fatal(err)
			}
			if plan.DistanceMeters <= 0 || plan.DurationSeconds <= 0 || len(plan.Path) != 2 {
				t.Fatalf("plan=%+v", plan)
			}
		})
	}
}
