package weather

import (
	"context"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		text string
		day  int
		ok   bool
	}{
		{"今日の天気は？", 0, true},
		{"天気を教えて", 0, true},
		{"天気予報", 0, true},
		{"明日の天気予報を教えてください。", 1, true},
		{"あしたの天気どう", 1, true},
		{"今日は傘いる？", 0, true},
		{"明日傘必要かな", 1, true},
		{"あすは、かさいる?", 1, true},
		// 地点の指定や天気に関する質問は AI へ渡す
		{"大阪の天気は？", 0, false},
		{"天気予報の仕組みを教えて", 0, false},
		{"来週の天気は", 0, false},
	}
	for _, c := range cases {
		day, ok := Parse(c.text)
		if ok != c.ok || (ok && day != c.day) {
			t.Errorf("Parse(%q) = %d, %v; want %d, %v", c.text, day, ok, c.day, c.ok)
		}
	}
}

type stubForecaster Forecast

func (s stubForecaster) Forecast(context.Context) (Forecast, error) { return Forecast(s), nil }

func TestReply(t *testing.T) {
	s := &Skill{Forecaster: stubForecaster{CurrentTemperature: 19.4,
		Days: []Day{{Code: 3, Max: 22.7, Min: 16.1, PrecipitationProbability: 4}, {Code: 61, Max: 1.2, Min: -3.6, PrecipitationProbability: -1}}}}
	cases := map[string]string{
		"今日の天気は": "今日はくもり。最高気温23度、最低気温16度です。降水確率は4パーセントです。今の気温は19度です。",
		"明日の天気は": "明日は小雨。最高気温1度、最低気温マイナス4度です。",
	}
	for text, want := range cases {
		invoke, ok := s.Match(text)
		if !ok {
			t.Fatalf("Match(%q) が一致しない", text)
		}
		if got, err := invoke(context.Background()); err != nil || got.Reply != want {
			t.Errorf("%q: got %q, err = %v", text, got.Reply, err)
		}
	}
	if _, err := Reply(Forecast{Days: []Day{{}}}, 1); err == nil {
		t.Error("明日の予報がないのにエラーにならない")
	}
}
