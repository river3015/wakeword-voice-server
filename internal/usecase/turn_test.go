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

func (s stubListener) Listen(context.Context) (Heard, error) { return Heard{PCM: s}, nil }

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

func TestMarkdownIsNotSpoken(t *testing.T) {
	cases := map[string]string{
		"## 日本の四季":          "日本の四季",
		"- **春**は桜です。":      "春は桜です。",
		"1. `go build`します。": "go buildします。",
		"---":               "",
		"普通の文です。":           "普通の文です。",
	}
	for in, want := range cases {
		if got := speakable(in); got != want {
			t.Errorf("speakable(%q) = %q, want %q", in, got, want)
		}
	}
	responder := &stubResponder{chunks: []string{"## 見出し\n", "---\n", "- 一つ目。"}}
	runner, player, _ := newRunner(t, "質問", responder)
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(player.spoken, []string{"見出し", "一つ目。"}) {
		t.Errorf("spoken = %q", player.spoken)
	}
	if result.ReplyCharacter != len([]rune("## 見出し\n---\n- 一つ目。")) {
		t.Errorf("履歴用の返答が加工されている: %d", result.ReplyCharacter)
	}
}

// stubSkill は決まった発話にだけ一致する。
type stubSkill struct {
	phrase, reply string
	err           error
	calls         int
	after         func(context.Context) error
}

func (s *stubSkill) Name() string { return "stub" }

func (s *stubSkill) Match(text string) (Invocation, bool) {
	if text != s.phrase {
		return nil, false
	}
	return func(context.Context) (Outcome, error) {
		s.calls++
		return Outcome{Reply: s.reply, AfterSpeech: s.after}, s.err
	}, true
}

func TestRunSkillHandlesMatchedPhraseWithoutAI(t *testing.T) {
	responder := &stubResponder{chunks: []string{"AIの返答。"}}
	runner, player, _ := newRunner(t, "牛乳を追加", responder)
	skill := &stubSkill{phrase: "牛乳を追加", reply: "牛乳を追加しました。"}
	runner.Skills = []Skill{&stubSkill{phrase: "別の言い回し"}, skill}

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if responder.calls != 0 || skill.calls != 1 || !slices.Equal(player.spoken, []string{"牛乳を追加しました。"}) {
		t.Errorf("ai calls = %d, skill calls = %d, spoken = %q", responder.calls, skill.calls, player.spoken)
	}
	if !result.Completed || result.Skill != "stub" || runner.Conversation.Turns() != 1 {
		t.Errorf("result = %+v, turns = %d", result, runner.Conversation.Turns())
	}
}

func TestRunUnmatchedPhraseGoesToAI(t *testing.T) {
	responder := &stubResponder{chunks: []string{"AIの返答。"}}
	runner, _, _ := newRunner(t, "質問", responder)
	runner.Skills = []Skill{&stubSkill{phrase: "牛乳を追加"}}

	result, err := runner.Run(context.Background())
	if err != nil || responder.calls != 1 || result.Skill != "" {
		t.Errorf("result = %+v, err = %v, ai calls = %d", result, err, responder.calls)
	}
}

func TestRunSkillFailureIsSpokenAndReturned(t *testing.T) {
	responder := &stubResponder{}
	runner, player, states := newRunner(t, "牛乳を追加", responder)
	runner.Skills = []Skill{&stubSkill{phrase: "牛乳を追加", err: errors.New("store")}}

	_, err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "skill stub") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(player.spoken, []string{skillFailedNotice}) || responder.calls != 0 {
		t.Errorf("spoken = %q, ai calls = %d", player.spoken, responder.calls)
	}
	if runner.Conversation.Turns() != 0 || (*states)[len(*states)-1] != StateWaiting {
		t.Errorf("turns = %d, states = %v", runner.Conversation.Turns(), *states)
	}
}

func TestRunSkillActsAfterSpeaking(t *testing.T) {
	runner, player, _ := newRunner(t, "音楽をかけて", &stubResponder{})
	var spokenBefore []string
	skill := &stubSkill{phrase: "音楽をかけて", reply: "再生します。", after: func(context.Context) error {
		spokenBefore = slices.Clone(player.spoken)
		return nil
	}}
	runner.Skills = []Skill{skill}

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spokenBefore, []string{"再生します。"}) {
		t.Errorf("読み上げの前に実行された: %q", spokenBefore)
	}
	skill.after = func(context.Context) error { return errors.New("music") }
	if _, err := runner.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "skill stub") {
		t.Errorf("err = %v", err)
	}
}

// wakeListener は依頼音声なしで、指定したウェイクワードを検出したことにする。
type wakeListener string

func (w wakeListener) Listen(context.Context) (Heard, error) { return Heard{Wake: string(w)}, nil }

func TestRunWakeActionSkipsTranscriptionAndHistory(t *testing.T) {
	responder := &stubResponder{}
	runner, player, _ := newRunner(t, "使われない", responder)
	runner.Listener = wakeListener("hey_jarvis")
	called := 0
	runner.WakeActions = map[string]Invocation{"hey_jarvis": func(context.Context) (Outcome, error) {
		called++
		return Outcome{Reply: "通話に入ります。"}, nil
	}}

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 || !result.Completed || result.Wake != "hey_jarvis" || !slices.Equal(player.spoken, []string{"通話に入ります。"}) {
		t.Errorf("called = %d, result = %+v, spoken = %q", called, result, player.spoken)
	}
	if responder.calls != 0 || runner.Conversation.Turns() != 0 {
		t.Error("AI を呼んだか、履歴に残した")
	}
}

func TestRunWakeActionHoldsUntilDone(t *testing.T) {
	runner, _, states := newRunner(t, "", &stubResponder{})
	runner.Listener = wakeListener("hey_jarvis")
	release := make(chan struct{})
	runner.WakeActions = map[string]Invocation{"hey_jarvis": func(context.Context) (Outcome, error) {
		return Outcome{Until: func(context.Context) error { <-release; return nil }}, nil
	}}
	done := make(chan Result, 1)
	go func() {
		result, err := runner.Run(context.Background())
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	select {
	case <-done:
		t.Fatal("通話が終わる前に待ち受けへ戻った")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if result := <-done; !result.Completed {
		t.Errorf("result = %+v", result)
	}
	if !slices.Contains(*states, StateHandedOff) || (*states)[len(*states)-1] != StateWaiting {
		t.Errorf("states = %v", *states)
	}
}

func TestRunWakeActionStopsHoldingOnCancel(t *testing.T) {
	runner, _, _ := newRunner(t, "", &stubResponder{})
	runner.Listener = wakeListener("hey_jarvis")
	runner.WakeActions = map[string]Invocation{"hey_jarvis": func(context.Context) (Outcome, error) {
		return Outcome{Until: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := runner.Run(ctx); err != nil {
		t.Errorf("停止の指示をエラーにした: %v", err)
	}
}
