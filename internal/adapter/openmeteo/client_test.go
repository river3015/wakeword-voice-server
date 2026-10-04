package openmeteo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sample = `{"current":{"time":"2026-10-04T19:45","temperature_2m":19.4},
"daily":{"time":["2026-10-04","2026-10-05"],"weather_code":[3,61],"temperature_2m_max":[22.7,23.5],
"temperature_2m_min":[16.1,17.2],"precipitation_probability_max":[4,null]}}`

func TestForecastRoundsCoordinatesAndParses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("latitude") != "35.68" || q.Get("longitude") != "139.77" || q.Get("forecast_days") != "2" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		w.Write([]byte(sample))
	}))
	defer server.Close()
	c := &Client{URL: server.URL, Latitude: 35.681236, Longitude: 139.767125}
	f, err := c.Forecast(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.CurrentTemperature != 19.4 || len(f.Days) != 2 || f.Days[1].Code != 61 || f.Days[0].PrecipitationProbability != 4 ||
		f.Days[1].PrecipitationProbability != -1 {
		t.Errorf("forecast = %+v", f)
	}
}

func TestForecastErrors(t *testing.T) {
	for _, body := range []string{`{"daily":{}}`, `not json`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		c := &Client{URL: server.URL, Latitude: 35, Longitude: 139}
		if _, err := c.Forecast(context.Background()); err == nil {
			t.Errorf("%q でエラーにならない", body)
		}
		server.Close()
	}
	if err := (&Client{}).Validate(); err == nil {
		t.Error("座標が未設定なのに通った")
	}
}
