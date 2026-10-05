package usecase

import (
	"context"
)

// Service は使うときだけ動かしておく外部の部品（常駐させる文字起こしや音声合成のサーバーなど）。
// 待ち受け中は止めておき、メモリを空ける。
type Service interface {
	Prepare()                          // 待たずに準備を始める
	Acquire(ctx context.Context) error // 準備ができるまで待ち、使用中にする
	Release()                          // 使い終えた。しばらく使われなければ止めてよい
}

// Preparer は使う前に時間のかかる準備が要る部品が、任意で実装する。
// 呼びかけを検出した時点で Prepare を呼び、文字起こしや読み上げまでに準備を終えておく。
type Preparer interface {
	Prepare()
}

// Prepare は parts のうち Preparer を実装するものの準備を始める。
func Prepare(parts ...any) {
	for _, part := range parts {
		if p, ok := part.(Preparer); ok {
			p.Prepare()
		}
	}
}

// OnDemandTranscriber は文字起こしの前に Service の準備を待つ。
type OnDemandTranscriber struct {
	Transcriber
	Service Service
}

func (t OnDemandTranscriber) Prepare() { t.Service.Prepare() }

func (t OnDemandTranscriber) Transcribe(ctx context.Context, pcm []int16) (string, error) {
	if err := t.Service.Acquire(ctx); err != nil {
		return "", err
	}
	defer t.Service.Release()
	return t.Transcriber.Transcribe(ctx, pcm)
}

// OnDemandSynthesizer は音声合成の前に Service の準備を待つ。
type OnDemandSynthesizer struct {
	Synthesizer
	Service Service
}

func (s OnDemandSynthesizer) Prepare() { s.Service.Prepare() }

func (s OnDemandSynthesizer) Synthesize(ctx context.Context, sentence string) ([]byte, error) {
	if err := s.Service.Acquire(ctx); err != nil {
		return nil, err
	}
	defer s.Service.Release()
	return s.Synthesizer.Synthesize(ctx, sentence)
}
