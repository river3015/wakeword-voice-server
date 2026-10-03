package domain

import (
	"errors"
	"fmt"
)

// 音声フレームの共通形式：16kHz・モノラル・16bit PCM・80ms。
const (
	SampleRate   = 16000
	FrameSamples = 1280
	FrameSeconds = float64(FrameSamples) / SampleRate
)

// CaptureSettings は呼びかけ後の録音を終える条件。秒単位。
type CaptureSettings struct {
	WakeThreshold    float64 `toml:"wake_threshold"`
	SpeechThreshold  float64 `toml:"speech_threshold"`
	SilenceSeconds   float64 `toml:"silence_seconds"`
	StartTimeout     float64 `toml:"start_timeout"`
	MaxSeconds       float64 `toml:"max_seconds"`
	MinSpeechSeconds float64 `toml:"min_speech_seconds"`
	SettleSeconds    float64 `toml:"settle_seconds"`
}

func DefaultCaptureSettings() CaptureSettings {
	return CaptureSettings{WakeThreshold: 0.5, SpeechThreshold: 0.5, SilenceSeconds: 1.2, StartTimeout: 5,
		MaxSeconds: 30, MinSpeechSeconds: 0.24, SettleSeconds: 0.32}
}

func (s CaptureSettings) Validate() error {
	if !(0 < s.WakeThreshold && s.WakeThreshold <= 1 && 0 < s.SpeechThreshold && s.SpeechThreshold <= 1) {
		return errors.New("検出閾値は0より大きく1以下にしてください")
	}
	for _, v := range []float64{s.SilenceSeconds, s.StartTimeout, s.MaxSeconds, s.MinSpeechSeconds} {
		if !(0 < v && v <= 300) {
			return errors.New("録音時間の設定は0より大きく300秒以下にしてください")
		}
	}
	if s.MinSpeechSeconds > s.MaxSeconds {
		return errors.New("最小発話時間は録音上限以下にしてください")
	}
	if !(0 <= s.SettleSeconds && s.SettleSeconds <= 2) {
		return errors.New("受付後の抑制時間は0〜2秒にしてください")
	}
	return nil
}

// Utterance は収集した依頼音声。PCM が空なら依頼なし。
type Utterance struct {
	PCM    []int16
	Reason string // silence、max_duration、no_speech
}

type RecorderState string

const (
	RecorderWaiting   RecorderState = "waiting"
	RecorderRecording RecorderState = "recording"
)

const epsilon = 1e-9

// Recorder はウェイクワード検出と発話区間の判定結果から依頼音声を切り出す。
// 推論やデバイスには依存せず、スコアを受け取るだけなので単体でテストできる。
type Recorder struct {
	settings CaptureSettings
	state    RecorderState
	frames   []int16
	preroll  [][]int16 // 発話開始直前の音を最大4フレーム残す
	elapsed  float64
	speech   float64
	silence  float64
	started  bool
	settle   float64
}

func NewRecorder(settings CaptureSettings) *Recorder {
	r := &Recorder{settings: settings}
	r.Reset()
	return r
}

func (r *Recorder) State() RecorderState { return r.state }

func (r *Recorder) Reset() {
	r.state = RecorderWaiting
	r.frames = nil
	r.preroll = nil
	r.elapsed, r.speech, r.silence = 0, 0, 0
	r.started = false
	r.settle = r.settings.SettleSeconds
}

// Feed は1フレームを処理し、録音が終わったときだけ Utterance を返す。
func (r *Recorder) Feed(pcm []int16, wakeScore, speechScore float64) (*Utterance, error) {
	if len(pcm) != FrameSamples {
		return nil, fmt.Errorf("入力音声フレームのサイズが不正です: %d", len(pcm))
	}
	if r.state == RecorderWaiting {
		if wakeScore >= r.settings.WakeThreshold {
			r.state = RecorderRecording
		}
		return nil, nil
	}
	// 受付直後は受付音の反響を拾うので捨てる
	if r.settle > epsilon {
		r.settle -= FrameSeconds
		return nil, nil
	}
	r.elapsed += FrameSeconds
	if speechScore >= r.settings.SpeechThreshold {
		if !r.started {
			for _, f := range r.preroll {
				r.frames = append(r.frames, f...)
			}
			r.preroll = nil
		}
		r.started = true
		r.speech += FrameSeconds
		r.silence = 0
	} else if r.started {
		r.silence += FrameSeconds
	}
	if r.started {
		r.frames = append(r.frames, pcm...)
	} else {
		r.preroll = append(r.preroll, pcm)
		if len(r.preroll) > 4 {
			r.preroll = r.preroll[1:]
		}
	}

	reason := ""
	switch {
	case !r.started && r.elapsed+epsilon >= r.settings.StartTimeout:
		reason = "no_speech"
	case r.elapsed+epsilon >= r.settings.MaxSeconds:
		reason = "max_duration"
	case r.started && r.silence+epsilon >= r.settings.SilenceSeconds:
		reason = "silence"
	}
	if reason == "" {
		return nil, nil
	}
	result := &Utterance{Reason: reason}
	if r.speech+epsilon >= r.settings.MinSpeechSeconds {
		result.PCM = r.frames
	} else {
		result.Reason = "no_speech"
	}
	r.Reset()
	return result, nil
}
