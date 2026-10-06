package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/domain"
)

// CaptureListener は入力を開き、呼びかけの検出から発話の終わりまでを集める。
type CaptureListener struct {
	Opener   FrameOpener
	Detector Detector
	Settings domain.CaptureSettings
	// WakeWords は Detector が返すスコアの並びに対応するウェイクワードの名前
	WakeWords []string
	// Immediate は依頼を録音せず、検出した時点で返すウェイクワード。WakeAction を持つものに使う
	Immediate map[string]bool
	// MaxWait は呼びかけを待つ上限。0なら無期限。録音中に達した場合は録音を終えるまで続ける
	MaxWait     time.Duration
	OnState     func(State)
	OnRecording func() // 受付音を鳴らすなど。録音を止めないよう、すぐ戻ること
}

func (l *CaptureListener) Listen(ctx context.Context) (Heard, error) {
	if err := l.Detector.Reset(); err != nil {
		return Heard{}, fmt.Errorf("detector: %w", err)
	}
	source, err := l.Opener.Open(ctx)
	if err != nil {
		return Heard{}, err
	}
	defer source.Close()

	recorder := domain.NewRecorder(l.Settings)
	wake := ""
	deadline := time.Time{}
	if l.MaxWait > 0 {
		deadline = time.Now().Add(l.MaxWait)
	}
	for {
		waiting := recorder.State() == domain.RecorderWaiting
		if waiting && !deadline.IsZero() && time.Now().After(deadline) {
			return Heard{}, nil
		}
		frame, err := source.Next(ctx)
		if errors.Is(err, io.EOF) {
			return Heard{}, nil
		}
		if err != nil {
			return Heard{}, err
		}
		scores, speech, err := l.Detector.Scores(frame, waiting)
		if err != nil {
			return Heard{}, fmt.Errorf("detector: %w", err)
		}
		best := strongest(scores)
		score := 0.0
		if best >= 0 {
			score = scores[best]
		}
		utterance, err := recorder.Feed(frame, score, speech)
		if err != nil {
			return Heard{}, err
		}
		if waiting && recorder.State() == domain.RecorderRecording {
			if best < len(l.WakeWords) {
				wake = l.WakeWords[best]
			}
			if l.Immediate[wake] {
				return Heard{Wake: wake}, nil
			}
			if l.OnState != nil {
				l.OnState(StateRecording)
			}
			if l.OnRecording != nil {
				l.OnRecording()
			}
		}
		if utterance != nil {
			return Heard{Wake: wake, PCM: utterance.PCM}, nil
		}
	}
}

// strongest は最もスコアの高いウェイクワードの位置を返す。空なら -1。
func strongest(scores []float64) int {
	best := -1
	for i, s := range scores {
		if best < 0 || s > scores[best] {
			best = i
		}
	}
	return best
}
