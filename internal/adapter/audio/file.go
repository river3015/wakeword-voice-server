package audio

import (
	"context"
	"io"

	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// WAVFile はマイクの代わりに録音済み WAV を流す。検証用。
type WAVFile struct {
	Path string
}

type wavSource struct {
	samples []int16
}

func (w WAVFile) Open(context.Context) (usecase.FrameSource, error) {
	samples, err := ReadRequestWAV(w.Path)
	if err != nil {
		return nil, err
	}
	return &wavSource{samples: samples}, nil
}

func (s *wavSource) Next(ctx context.Context) ([]int16, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.samples) == 0 {
		return nil, io.EOF
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

func (r RequestWAV) Listen(context.Context) ([]int16, error) {
	return ReadRequestWAV(r.Path)
}
