package domain

import (
	"slices"
	"testing"
)

func frame(value int16) []int16 {
	f := make([]int16, FrameSamples)
	for i := range f {
		f[i] = value
	}
	return f
}

func testRecorder() *Recorder {
	return NewRecorder(CaptureSettings{WakeThreshold: 0.5, SpeechThreshold: 0.5, SilenceSeconds: 0.24,
		StartTimeout: 0.4, MaxSeconds: 0.8, MinSpeechSeconds: 0.16, SettleSeconds: 0})
}

// feed はテストを短くするための補助。エラーならテストを止める。
func feed(t *testing.T, r *Recorder, pcm []int16, wake, speech float64) *Utterance {
	t.Helper()
	u, err := r.Feed(pcm, wake, speech)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestWakeIsNotPartOfRecordedRequest(t *testing.T) {
	r := testRecorder()
	feed(t, r, frame(0), 1, 0)
	if r.State() != RecorderRecording {
		t.Fatal("呼びかけで録音に移らない")
	}
	feed(t, r, frame(1), 0, 1)
	feed(t, r, frame(1), 0, 1)
	var u *Utterance
	for range 3 {
		u = feed(t, r, frame(1), 0, 0)
	}
	if u == nil || u.Reason != "silence" || !slices.Equal(u.PCM, slices.Repeat(frame(1), 5)) {
		t.Fatalf("utterance = %+v", u)
	}
	if r.State() != RecorderWaiting {
		t.Error("録音後に待ち受けへ戻らない")
	}
}

func TestNoSpeechIsDiscarded(t *testing.T) {
	r := testRecorder()
	feed(t, r, frame(1), 1, 0)
	var u *Utterance
	for range 5 {
		u = feed(t, r, frame(1), 0, 0)
	}
	if u == nil || u.Reason != "no_speech" || len(u.PCM) != 0 {
		t.Fatalf("utterance = %+v", u)
	}
}

func TestContinuousSpeechIsBounded(t *testing.T) {
	r := testRecorder()
	feed(t, r, frame(1), 1, 0)
	var u *Utterance
	for range 10 {
		u = feed(t, r, frame(1), 0, 1)
	}
	if u == nil || u.Reason != "max_duration" || len(u.PCM) != FrameSamples*10 {
		t.Fatalf("reason = %v, samples = %d", u.Reason, len(u.PCM))
	}
}

func TestShortNoiseIsNotARequest(t *testing.T) {
	r := testRecorder()
	feed(t, r, frame(1), 1, 0)
	feed(t, r, frame(1), 0, 1)
	var u *Utterance
	for range 3 {
		u = feed(t, r, frame(1), 0, 0)
	}
	if u == nil || len(u.PCM) != 0 {
		t.Fatalf("utterance = %+v", u)
	}
}

func TestBadSettingsAndFrames(t *testing.T) {
	s := DefaultCaptureSettings()
	s.MaxSeconds = 0
	if s.Validate() == nil {
		t.Error("録音上限0秒を受け付けた")
	}
	if err := DefaultCaptureSettings().Validate(); err != nil {
		t.Error(err)
	}
	if _, err := testRecorder().Feed(nil, 0, 0); err == nil {
		t.Error("空フレームを受け付けた")
	}
}

func TestReceiptEchoIsDiscarded(t *testing.T) {
	s := DefaultCaptureSettings()
	s.SettleSeconds, s.StartTimeout, s.MinSpeechSeconds = 0.16, 0.24, 0.08
	r := NewRecorder(s)
	feed(t, r, frame(1), 1, 0)
	feed(t, r, frame(1), 0, 1)
	feed(t, r, frame(1), 0, 1)
	var u *Utterance
	for range 3 {
		u = feed(t, r, frame(1), 0, 0)
	}
	if u == nil || len(u.PCM) != 0 {
		t.Fatalf("utterance = %+v", u)
	}
}
