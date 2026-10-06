package audio

import (
	"context"
	"io"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// WAVFile はマイクの代わりに録音済み WAV を流す。検証用。
type WAVFile struct {
	Path string
	// Realtime ならマイクと同じく80msごとに1フレームを返す。呼びかけから発話の終わりまでの間に
	// 進む処理（文字起こしなどの準備）を含めて測るときに使う
	Realtime bool
}

type wavSource struct {
	samples []int16
	next    time.Time // Realtime のとき、次のフレームを返す時刻
}

func (w WAVFile) Open(context.Context) (usecase.FrameSource, error) {
	samples, err := ReadRequestWAV(w.Path)
	if err != nil {
		return nil, err
	}
	source := &wavSource{samples: samples}
	if w.Realtime {
		source.next = time.Now()
	}
	return source, nil
}

func (s *wavSource) Next(ctx context.Context) ([]int16, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.samples) == 0 {
		return nil, io.EOF
	}
	if !s.next.IsZero() {
		s.next = s.next.Add(frameSamples * time.Second / 16000)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Until(s.next)):
		}
	}
	frame := make([]int16, frameSamples) // 最後の端数は無音で埋める
	n := copy(frame, s.samples)
	s.samples = s.samples[n:]
	return frame, nil
}

func (s *wavSource) Close() error { return nil }

// RequestWAV は呼びかけなしの依頼 WAV をそのまま依頼音声として返す。検証用。
type RequestWAV struct {
	Path string
}

func (r RequestWAV) Listen(context.Context) (usecase.Heard, error) {
	pcm, err := ReadRequestWAV(r.Path)
	return usecase.Heard{PCM: pcm}, err
}
