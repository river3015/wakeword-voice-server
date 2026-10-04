// Package openmeteo は Open-Meteo の予報 API から、設定した1地点の予報を取得する。
// 送るのは座標だけ。座標は小数2桁（約1km）に丸める。
package openmeteo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/skill/weather"
)

const DefaultURL = "https://api.open-meteo.com/v1/forecast"

type Client struct {
	URL                 string // 空なら DefaultURL。テストで差し替える
	Latitude, Longitude float64
	HTTP                *http.Client // nil なら10秒でタイムアウトするクライアント
}

func (c *Client) Validate() error {
	if math.IsNaN(c.Latitude) || math.Abs(c.Latitude) > 90 || math.IsNaN(c.Longitude) || math.Abs(c.Longitude) > 180 ||
		(c.Latitude == 0 && c.Longitude == 0) {
		return errors.New("天気の緯度・経度を設定してください")
	}
	return nil
}

type response struct {
	Current struct {
		Temperature *float64 `json:"temperature_2m"`
	} `json:"current"`
	Daily struct {
		Code          []int     `json:"weather_code"`
		Max           []float64 `json:"temperature_2m_max"`
		Min           []float64 `json:"temperature_2m_min"`
		Precipitation []*int    `json:"precipitation_probability_max"`
	} `json:"daily"`
}

func (c *Client) Forecast(ctx context.Context) (weather.Forecast, error) {
	base, client := c.URL, c.HTTP
	if base == "" {
		base = DefaultURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	query := url.Values{
		"latitude":      {coordinate(c.Latitude)},
		"longitude":     {coordinate(c.Longitude)},
		"current":       {"temperature_2m"},
		"daily":         {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"timezone":      {"auto"}, // 地点の時間帯で日を区切る
		"forecast_days": {"2"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+query.Encode(), nil)
	if err != nil {
		return weather.Forecast{}, err
	}
	res, err := client.Do(request)
	if err != nil {
		return weather.Forecast{}, fmt.Errorf("天気予報を取得できません: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return weather.Forecast{}, fmt.Errorf("天気予報を取得できません: HTTP %d", res.StatusCode)
	}
	var body response
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return weather.Forecast{}, fmt.Errorf("天気予報を読めません: %w", err)
	}
	d := body.Daily
	n := min(len(d.Code), len(d.Max), len(d.Min), len(d.Precipitation))
	if n == 0 || body.Current.Temperature == nil {
		return weather.Forecast{}, errors.New("天気予報が空です")
	}
	forecast := weather.Forecast{CurrentTemperature: *body.Current.Temperature}
	for i := range n {
		day := weather.Day{Code: d.Code[i], Max: d.Max[i], Min: d.Min[i], PrecipitationProbability: -1}
		if p := d.Precipitation[i]; p != nil {
			day.PrecipitationProbability = *p
		}
		forecast.Days = append(forecast.Days, day)
	}
	return forecast, nil
}

func coordinate(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
