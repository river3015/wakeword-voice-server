package usecase

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/domain"
)

// sequenceListener は呼ばれるたびに次の結果を返す。
type sequenceListener struct {
	results []error
	calls   int
}

func (s *sequenceListener) Listen(context.Context) (Heard, error) {
	err := s.results[s.calls]
	s.calls++
	if err != nil {
		return Heard{}, err
	}
	return Heard{PCM: []int16{1}}, nil
}

func TestServeRecoversAfterFailureAndStops(t *testing.T) {
	responder := &stubResponder{chunks: []string{"はい。"}}
	runner, _, states := newRunner(t, "こんにちは", responder)
	listener := &sequenceListener{results: []error{errors.New("mic"), nil, nil}}
	runner.Listener = listener
	var slept []time.Duration
	var results []Result
	server := &Server{Runner: runner,
		OnResult: func(r Result) { results = append(results, r) },
		Sleep:    func(_ context.Context, d time.Duration) bool { slept = append(slept, d); return true }}

	server.Serve(context.Background(), 2)

	if listener.calls != 2 || len(results) != 1 || !results[0].Completed {
		t.Fatalf("calls = %d, results = %+v", listener.calls, results)
	}
	if !slices.Equal(slept, []time.Duration{2 * time.Second}) {
		t.Errorf("slept = %v", slept)
	}
	if !slices.Contains(*states, StateError) || (*states)[len(*states)-1] != StateStopped {
		t.Errorf("states = %v", *states)
	}
	if runner.Conversation.Turns() != 0 {
		t.Error("停止時に履歴が破棄されない")
	}
}

func TestServeStopsOnStopCommand(t *testing.T) {
	runner, _, _ := newRunner(t, "サーバー停止", &stubResponder{})
	server := &Server{Runner: runner}
	done := make(chan struct{})
	go func() { server.Serve(context.Background(), 0); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("停止の指示で終わらない")
	}
}

// scriptedSource は与えたフレームを順に返し、尽きたら io.EOF を返す。
type scriptedSource struct{ frames int }

func (s *scriptedSource) Next(context.Context) ([]int16, error) {
	if s.frames == 0 {
		return nil, io.EOF
	}
	s.frames--
	return make([]int16, domain.FrameSamples), nil
}
func (s *scriptedSource) Close() error { return nil }

type opener struct{ source *scriptedSource }

func (o opener) Open(context.Context) (FrameSource, error) { return o.source, nil }

// scriptedDetector は1フレーム目で呼びかけを検出し、その後 speech フレームだけ発話とする。
type scriptedDetector struct {
	calls, speech, resets int
	wake                  []float64 // 待ち受け中に返すスコア。nil なら1つ目が1
}

func (d *scriptedDetector) Scores(_ []int16, waiting bool) ([]float64, float64, error) {
	d.calls++
	if waiting {
		if d.wake != nil {
			return d.wake, 0, nil
		}
		return []float64{1}, 0, nil
	}
	if d.calls <= 1+d.speech {
		return nil, 1, nil
	}
	return nil, 0, nil
}
func (d *scriptedDetector) Reset() error { d.resets++; return nil }

func TestCaptureListenerCollectsUtterance(t *testing.T) {
	settings := domain.DefaultCaptureSettings()
	settings.SettleSeconds = 0
	detector := &scriptedDetector{speech: 5}
	var states []State
	cues := 0
	listener := &CaptureListener{Opener: opener{&scriptedSource{frames: 100}}, Detector: detector,
		Settings: settings, OnState: func(s State) { states = append(states, s) }, OnRecording: func() { cues++ }}

	heard, err := listener.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pcm := heard.PCM
	// 発話5フレーム＋無音1.2秒（15フレーム）
	if len(pcm) != 20*domain.FrameSamples {
		t.Errorf("samples = %d", len(pcm))
	}
	if cues != 1 || !slices.Equal(states, []State{StateRecording}) {
		t.Errorf("cues = %d, states = %v", cues, states)
	}
}

func TestCaptureListenerInputEnded(t *testing.T) {
	listener := &CaptureListener{Opener: opener{&scriptedSource{frames: 3}}, Detector: &scriptedDetector{},
		Settings: domain.DefaultCaptureSettings()}
	heard, err := listener.Listen(context.Background())
	if err != nil || heard.PCM != nil {
		t.Errorf("pcm = %d, err = %v", len(heard.PCM), err)
	}
}

func TestCaptureListenerReportsStrongestWakeWord(t *testing.T) {
	settings := domain.DefaultCaptureSettings()
	settings.SettleSeconds = 0
	listener := &CaptureListener{Opener: opener{&scriptedSource{frames: 100}},
		Detector: &scriptedDetector{speech: 5, wake: []float64{0.6, 0.9}}, Settings: settings,
		WakeWords: []string{"hey_mycroft", "hey_jarvis"}}

	heard, err := listener.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if heard.Wake != "hey_jarvis" || len(heard.PCM) == 0 {
		t.Errorf("wake = %q, pcm = %d", heard.Wake, len(heard.PCM))
	}
}

func TestCaptureListenerReturnsImmediateWakeWithoutRecording(t *testing.T) {
	source := &scriptedSource{frames: 100}
	cues := 0
	listener := &CaptureListener{Opener: opener{source}, Detector: &scriptedDetector{wake: []float64{0, 0.9}},
		Settings: domain.DefaultCaptureSettings(), WakeWords: []string{"hey_mycroft", "hey_jarvis"},
		Immediate: map[string]bool{"hey_jarvis": true}, OnRecording: func() { cues++ }}

	heard, err := listener.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if heard.Wake != "hey_jarvis" || heard.PCM != nil || cues != 0 || source.frames != 99 {
		t.Errorf("heard = %+v, cues = %d, frames left = %d", heard, cues, source.frames)
	}
}
