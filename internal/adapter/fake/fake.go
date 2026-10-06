// Package fake はマイク・AI・音声合成なしで一往復を試すための偽物の実装。
// メソッドの形がポートと一致するので、そのままポートとして使える。
package fake

import (
	"context"
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// wait は ctx が取り消されたら待ちを中断する。select は複数の channel のうち先に届いた方を選ぶ。
func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

type Listener struct {
	Delay time.Duration
}

func (l Listener) Listen(ctx context.Context) (usecase.Heard, error) {
	if err := wait(ctx, l.Delay); err != nil {
		return usecase.Heard{}, err
	}
	return usecase.Heard{PCM: make([]int16, 16000)}, nil // 1秒分の無音 PCM。中身は使わない
}

type Transcriber struct {
	Text  string
	Delay time.Duration
}

func (t Transcriber) Transcribe(ctx context.Context, pcm []int16) (string, error) {
	if err := wait(ctx, t.Delay); err != nil {
		return "", err
	}
	return t.Text, nil
}

// Responder は Reply を数文字ずつ、LLM のトークンのように少しずつ返す。
type Responder struct {
	Reply           string
	FirstDelay      time.Duration // 最初の断片が届くまで
	ChunkDelay      time.Duration // 以降の断片の間隔
	ChunkCharacters int
}

func (r Responder) Respond(ctx context.Context, prompt string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		runes := []rune(r.Reply)
		size := max(r.ChunkCharacters, 1)
		for i := 0; i < len(runes); i += size {
			delay := r.ChunkDelay
			if i == 0 {
				delay = r.FirstDelay
			}
			if err := wait(ctx, delay); err != nil {
				yield("", err)
				return
			}
			// yield が false を返したら、呼び出し側がループを抜けたので終える
			if !yield(string(runes[i:min(i+size, len(runes))]), nil) {
				return
			}
		}
	}
}

// Synthesizer は音声の代わりに文字列のバイト列を返し、固定の時間に文字数比例の時間を足して待つ。
type Synthesizer struct {
	Out          io.Writer
	Base         time.Duration
	PerCharacter time.Duration
	Clock        func() string // 経過時間の表示用
}

func (s Synthesizer) Synthesize(ctx context.Context, sentence string) ([]byte, error) {
	fmt.Fprintf(s.Out, "%s   合成開始: %s\n", s.Clock(), sentence)
	if err := wait(ctx, s.Base+time.Duration(len([]rune(sentence)))*s.PerCharacter); err != nil {
		return nil, err
	}
	return []byte(sentence), nil
}

// Player は再生の代わりに内容を書き出し、文字数に比例して待つ。
type Player struct {
	Out          io.Writer
	PerCharacter time.Duration
	Clock        func() string
}

func (p Player) Play(ctx context.Context, audio []byte) error {
	sentence := string(audio)
	fmt.Fprintf(p.Out, "%s 再生開始: %s\n", p.Clock(), sentence)
	return wait(ctx, time.Duration(len([]rune(sentence)))*p.PerCharacter)
}
