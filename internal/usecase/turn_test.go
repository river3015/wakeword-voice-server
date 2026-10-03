package usecase

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/domain"
)

// テスト用の小さな偽物。ポートのメソッドを満たせば何でも差し込める。
type stubListener []int16

func (s stubListener) Listen(context.Context) ([]int16, error) { return s, nil }

type stubTranscriber string

func (s stubTranscriber) Transcribe(context.Context, []int16) (string, error) { return string(s), nil }

type stubResponder struct {
	chunks []string
	err    error
	calls  int
}

func (s *stubResponder) Respond(context.Context, string) iter.Seq2[string, error] {
	s.calls++
	return func(yield func(string, error) bool) {
		for _, c := range s.chunks {
			if !yield(c, nil) {
				return
			}
		}
		if s.err != nil {
			yield("", s.err)
		}
	}
}

// echoSynthesizer は文をそのまま音声データとして返す。started には合成を始めた文を送る。
type echoSynthesizer struct {
	started chan string // nil なら送らない
	fail    string      // この文の合成で失敗する
}

func (e echoSynthesizer) Synthesize(_ context.Context, s string) ([]byte, error) {
	if e.started != nil {
		e.started <- s
	}
	if s == e.fail {
		return nil, errors.New("synthesis failed")
	}
	return []byte(s), nil
}

type recordingPlayer struct {
	spoken  []string
	onFirst func() error // 最初の再生中に実行する処理
}

func (r *recordingPlayer) Play(_ context.Context, audio []byte) error {
	r.spoken = append(r.spoken, string(audio))
	if len(r.spoken) == 1 && r.onFirst != nil {
		return r.onFirst()
	}
	return nil
}

func newRunner(t *testing.T, text string, responder *stubResponder) (*TurnRunner, *recordingPlayer, *[]State) {
	t.Helper()
	conversation, err := domain.NewConversation(time.Minute, 6, 16000, nil)
	if err != nil {
		t.Fatal(err)
	}
	player := &recordingPlayer{}
	states := &[]State{}
	return &TurnRunner{
		Listener: stubListener{1, 2}, Transcriber: stubTranscriber(text), Responder: responder,
		Synthesizer: echoSynthesizer{}, Player: player, Conversation: conversation,
		OnState: func(s State) { *states = append(*states, s) },
	}, player, states
}

func TestRunSpeaksEachSentenceAsItArrives(t *testing.T) {
	responder := &stubResponder{chunks: []string{"こんに", "ちは。元気", "です！最後"}}
	runner, player, states := newRunner(t, "挨拶して", responder)

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"こんにちは。", "元気です！", "最後"}; !slices.Equal(player.spoken, want) {
		t.Errorf("spoken = %q, want %q", player.spoken, want)
	}
	want := []State{StateWaiting, StateTranscribing, StateProcessing, StateSpeaking, StateWaiting}
	if !slices.Equal(*states, want) {
		t.Errorf("states = %v, want %v", *states, want)
	}
	if !result.Completed || runner.Conversation.Turns() != 1 {
		t.Errorf("result = %+v, turns = %d", result, runner.Conversation.Turns())
	}
}

// 1文目の再生が終わる前に、2文目の合成が始まることを確かめる。
// 直列の実装だと 2文目の合成は 1文目の再生後なので、ここで待ち続けてタイムアウトする。
func TestRunSynthesizesNextSentenceWhilePlaying(t *testing.T) {
	responder := &stubResponder{chunks: []string{"一文目。二文目。"}}
	runner, player, _ := newRunner(t, "質問", responder)
	started := make(chan string, 2)
	runner.Synthesizer = echoSynthesizer{started: started}
	player.onFirst = func() error {
		<-started // 再生中なので 1文目の合成は済んでいる。読み捨てる
		select {
		case s := <-started:
			if s != "二文目。" {
				return fmt.Errorf("unexpected sentence %q", s)
			}
			return nil
		case <-time.After(time.Second):
			return errors.New("再生中に次の文の合成が始まらない")
		}
	}

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunSynthesisFailureStopsPipeline(t *testing.T) {
	responder := &stubResponder{chunks: []string{"一文目。二文目。三文目。"}}
	runner, player, states := newRunner(t, "質問", responder)
	runner.Synthesizer = echoSynthesizer{fail: "二文目。"}

	_, err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "synthesize") {
		t.Fatalf("err = %v", err)
	}
	if slices.Contains(player.spoken, "三文目。") {
		t.Error("失敗後も再生が続いている")
	}
	if last := (*states)[len(*states)-1]; last != StateWaiting || runner.Conversation.Turns() != 0 {
		t.Errorf("最後の状態 = %s, turns = %d", last, runner.Conversation.Turns())
	}
}

func TestRunControlPhraseSkipsAI(t *testing.T) {
	responder := &stubResponder{chunks: []string{"呼ばれない"}}
	runner, _, _ := newRunner(t, "サーバー停止", responder)

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stop || responder.calls != 0 {
		t.Errorf("result = %+v, AI calls = %d", result, responder.calls)
	}
}

func TestRunErrorReturnsToWaitingWithoutHistory(t *testing.T) {
	responder := &stubResponder{chunks: []string{"途中まで。"}, err: errors.New("timeout")}
	runner, _, states := newRunner(t, "質問", responder)

	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("エラーが返らない")
	}
	if last := (*states)[len(*states)-1]; last != StateWaiting {
		t.Errorf("最後の状態 = %s", last)
	}
	if runner.Conversation.Turns() != 0 {
		t.Error("失敗した往復が履歴に残っている")
	}
}

func TestSplitSentences(t *testing.T) {
	sentences, rest := splitSentences("一文目。二文目？途中")
	if !slices.Equal(sentences, []string{"一文目。", "二文目？"}) || rest != "途中" {
		t.Errorf("got %q, %q", sentences, rest)
	}
}

func TestSplitLongSentenceAtComma(t *testing.T) {
	long := strings.Repeat("あ", 100) + "、" + strings.Repeat("い", 50)
	sentences, rest := splitSentences(long)
	if len(sentences) != 1 || sentences[0] != strings.Repeat("あ", 100)+"、" || rest != strings.Repeat("い", 50) {
		t.Errorf("got %q, %q", sentences, rest)
	}
	sentences, rest = splitSentences(strings.Repeat("う", 250))
	if len(sentences) != 2 || len([]rune(rest)) != 10 {
		t.Errorf("読点のない長文を区切れない: %d, %d", len(sentences), len([]rune(rest)))
	}
}

func TestLongReplyIsTruncatedButRemembered(t *testing.T) {
	sentence := strings.Repeat("長", 99) + "。"
	responder := &stubResponder{chunks: []string{strings.Repeat(sentence, 20)}}
	runner, player, _ := newRunner(t, "長い説明", responder)

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(player.spoken) != 12 || player.spoken[11] != truncatedNotice {
		t.Errorf("spoken = %d 文, 最後 = %q", len(player.spoken), player.spoken[len(player.spoken)-1])
	}
	if result.ReplyCharacter != 2000 {
		t.Errorf("reply characters = %d", result.ReplyCharacter)
	}
}
