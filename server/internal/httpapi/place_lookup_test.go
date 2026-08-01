package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlaceLookupUsesWebServiceKeyAndNormalizesCandidates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "server-web-key" {
			t.Fatalf("key=%q", r.URL.Query().Get("key"))
		}
		if r.URL.Query().Get("keywords") != "示例东站" {
			t.Fatalf("keywords=%q", r.URL.Query().Get("keywords"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"1",
			"pois":[{
				"id":"poi-1",
				"name":"示例东站",
				"address":"示例省示例市示例区示例路333号",
				"location":"116.397428,39.909230",
				"adcode":"110101",
				"pname":"示例省",
				"cityname":"示例市",
				"adname":"示例区"
			}]
		}`))
	}))
	defer upstream.Close()

	proxy := &placeLookupProxy{
		key: "server-web-key", client: &http.Client{Timeout: time.Second},
		searchEndpoint: upstream.URL, reverseEndpoint: upstream.URL,
	}
	candidates, err := proxy.search(context.Background(), "示例东站")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%#v", candidates)
	}
	candidate := candidates[0]
	if candidate.Longitude != 116.397428 || candidate.Latitude != 39.909230 ||
		candidate.Adcode != "110101" || candidate.Region != "示例省示例市示例区" {
		t.Fatalf("candidate=%#v", candidate)
	}
}

func TestReverseGeocodeReturnsAddressAndWeatherRegion(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("location") != "116.397428,39.909230" {
			t.Fatalf("location=%q", r.URL.Query().Get("location"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"1",
			"regeocode":{
				"formatted_address":"示例省示例市示例区示例路333号",
				"addressComponent":{
					"province":"示例省",
					"city":"示例市",
					"district":"示例区",
					"adcode":"110101"
				}
			}
		}`))
	}))
	defer upstream.Close()

	proxy := &placeLookupProxy{
		key: "server-web-key", client: &http.Client{Timeout: time.Second},
		searchEndpoint: upstream.URL, reverseEndpoint: upstream.URL,
	}
	result, err := proxy.reverse(context.Background(), 116.397428, 39.909230)
	if err != nil {
		t.Fatal(err)
	}
	if result.Address == "" || result.Adcode != "110101" ||
		result.Region != "示例省示例市示例区" {
		t.Fatalf("result=%#v", result)
	}
}

func TestParseLocationRejectsInvalidOrOutOfChinaCoordinates(t *testing.T) {
	for _, raw := range []string{"", "104", "wrong,30", "10,30", "104,80"} {
		if _, _, err := parseLocation(raw); err == nil {
			t.Fatalf("location %q was accepted", raw)
		}
	}
}
