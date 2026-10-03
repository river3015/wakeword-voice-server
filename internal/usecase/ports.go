// Package usecase は一往復の進め方と、外部部品に求めるポートを定義する。
// 具体的な技術（whisper、Codex、VOICEVOXなど）はここでは import しない。
package usecase

import (
	"context"
	"iter"
)

// Listener は呼びかけを待ち、続く依頼の音声（16kHz・モノラル・16bit PCM）を返す。
// 発話がなければ空のスライスを返す。
type Listener interface {
	Listen(ctx context.Context) ([]int16, error)
}

type Transcriber interface {
	Transcribe(ctx context.Context, pcm []int16) (string, error)
}

// Responder は返答を断片ごとに返す。iter.Seq2 は for range で回せる関数型（Go 1.23以降）。
// 途中で失敗した場合は (空文字, エラー) を渡して終える。
type Responder interface {
	Respond(ctx context.Context, prompt string) iter.Seq2[string, error]
}

// Synthesizer は一文を音声データにする。再生と分けることで、再生中に次の文を合成できる。
type Synthesizer interface {
	Synthesize(ctx context.Context, sentence string) ([]byte, error)
}

// Player は音声データを再生し、再生が終わるまで戻らない。
type Player interface {
	Play(ctx context.Context, audio []byte) error
}

// FrameSource は80ms（1280標本）ずつ音声を返す。入力が終わったら io.EOF を返す。
type FrameSource interface {
	Next(ctx context.Context) ([]int16, error)
	Close() error
}

// FrameOpener は待ち受けのたびに入力を開く。AI処理や読み上げの間はマイクを閉じておくため。
type FrameOpener interface {
	Open(ctx context.Context) (FrameSource, error)
}

// Detector は待ち受け中はウェイクワード、録音中は発話らしさのスコアを返す。
type Detector interface {
	Scores(frame []int16, waiting bool) (wake, speech float64, err error)
	Reset() error
}

type State string

const (
	StateWaiting      State = "waiting"
	StateRecording    State = "recording"
	StateTranscribing State = "transcribing"
	StateProcessing   State = "processing"
	StateSpeaking     State = "speaking"
	StateError        State = "error"
	StateStopped      State = "stopped"
)
