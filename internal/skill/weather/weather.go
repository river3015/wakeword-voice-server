// Package weather は「今日の天気は？」のような決まった言い回しで、設定した1地点の予報を読み上げる。
// 予報の取得は Forecaster に任せる。
package weather

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// Day は1日分の予報。Code は WMO の天気コード。
type Day struct {
	Code     int
	Max, Min float64
	// PrecipitationProbability は降水確率（%）。負なら不明として読み上げない
	PrecipitationProbability int
}

// Forecast は現在の気温と、今日から順に並べた日ごとの予報。
type Forecast struct {
	CurrentTemperature float64
	Days               []Day
}

type Forecaster interface {
	Forecast(ctx context.Context) (Forecast, error)
}

type Skill struct {
	Forecaster Forecaster
}

func (s *Skill) Name() string { return "weather" }

func (s *Skill) Vocabulary() []string { return []string{"天気予報"} }

func (s *Skill) Match(text string) (usecase.Invocation, bool) {
	day, ok := Parse(text)
	if !ok {
		return nil, false
	}
	return func(ctx context.Context) (string, error) {
		forecast, err := s.Forecaster.Forecast(ctx)
		if err != nil {
			return "", err
		}
		return Reply(forecast, day)
	}, true
}

var (
	// 「今日の天気は？」「明日の天気予報を教えて」「天気どう？」
	weatherRequest = regexp.MustCompile(`^(今日|きょう|明日|あした|あす)?(?:の|は)?天気(?:予報)?` +
		`(?:は|を)?(?:教えて(?:ください)?|どう|どんな感じ|何)?(?:ですか|かな|でしょう)?$`)
	// 「今日は傘いる？」「明日傘いるかな」
	umbrellaRequest = regexp.MustCompile(`^(今日|きょう|明日|あした|あす)?(?:は)?傘(?:は|が)?(?:いる|必要)(?:かな|ですか|でしょうか)?$`)
)

// Parse は依頼が今日（0）か明日（1）の天気かを返す。日の指定がなければ今日。
func Parse(text string) (int, bool) {
	text = strings.TrimRight(strings.TrimSpace(text), " 　。.!！?？")
	for _, re := range []*regexp.Regexp{weatherRequest, umbrellaRequest} {
		if m := re.FindStringSubmatch(text); m != nil {
			switch m[1] {
			case "明日", "あした", "あす":
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

// Reply は予報を読み上げる文にする。今日なら現在の気温も添える。
func Reply(f Forecast, day int) (string, error) {
	if day >= len(f.Days) {
		return "", errors.New("予報の日数が足りません")
	}
	d := f.Days[day]
	label := []string{"今日", "明日"}[day]
	reply := fmt.Sprintf("%sは%s。最高気温%s、最低気温%sです。", label, Describe(d.Code), degrees(d.Max), degrees(d.Min))
	if d.PrecipitationProbability >= 0 {
		reply += fmt.Sprintf("降水確率は%dパーセントです。", d.PrecipitationProbability)
	}
	if day == 0 {
		reply += fmt.Sprintf("今の気温は%sです。", degrees(f.CurrentTemperature))
	}
	return reply, nil
}

func degrees(v float64) string {
	n := int(math.Round(v))
	if n < 0 {
		return fmt.Sprintf("マイナス%d度", -n)
	}
	return fmt.Sprintf("%d度", n)
}

// Describe は WMO の天気コードを読み上げ用の言葉にする。
func Describe(code int) string {
	switch code {
	case 0:
		return "晴れ"
	case 1:
		return "おおむね晴れ"
	case 2:
		return "晴れ時々くもり"
	case 3:
		return "くもり"
	case 45, 48:
		return "霧"
	case 51, 53, 55, 56, 57:
		return "霧雨"
	case 61:
		return "小雨"
	case 63, 66:
		return "雨"
	case 65, 67:
		return "強い雨"
	case 71, 77:
		return "小雪"
	case 73:
		return "雪"
	case 75:
		return "大雪"
	case 80, 81:
		return "にわか雨"
	case 82:
		return "激しいにわか雨"
	case 85, 86:
		return "にわか雪"
	case 95:
		return "雷雨"
	case 96, 99:
		return "ひょうを伴う雷雨"
	}
	return "天気の種類が不明"
}
