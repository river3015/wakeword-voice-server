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
	// MaxWait は呼びかけを待つ上限。0なら無期限。録音中に達した場合は録音を終えるまで続ける
	MaxWait     time.Duration
	OnState     func(State)
	OnRecording func() // 受付音を鳴らすなど。録音を止めないよう、すぐ戻ること
}

func (l *CaptureListener) Listen(ctx context.Context) ([]int16, error) {
	if err := l.Detector.Reset(); err != nil {
		return nil, fmt.Errorf("detector: %w", err)
	}
	source, err := l.Opener.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer source.Close()

	recorder := domain.NewRecorder(l.Settings)
	deadline := time.Time{}
	if l.MaxWait > 0 {
		deadline = time.Now().Add(l.MaxWait)
	}
	for {
		waiting := recorder.State() == domain.RecorderWaiting
		if waiting && !deadline.IsZero() && time.Now().After(deadline) {
			return nil, nil
		}
		frame, err := source.Next(ctx)
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		wake, speech, err := l.Detector.Scores(frame, waiting)
		if err != nil {
			return nil, fmt.Errorf("detector: %w", err)
		}
		utterance, err := recorder.Feed(frame, wake, speech)
		if err != nil {
			return nil, err
		}
		if waiting && recorder.State() == domain.RecorderRecording {
			if l.OnState != nil {
				l.OnState(StateRecording)
			}
			if l.OnRecording != nil {
				l.OnRecording()
			}
		}
		if utterance != nil {
			return utterance.PCM, nil
		}
	}
}
