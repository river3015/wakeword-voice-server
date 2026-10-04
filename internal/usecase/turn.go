package usecase

import (
	"context"
	"fmt"
	"iter"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/domain"
)

// VoiceInstruction は連携先に関係なく依頼の前に付ける、音声で返すための指示。
const VoiceInstruction = "日本語で答えてください。音声で読み上げるため結論を先に短く述べ、見出し・箇条書き・表などの記号を使わず話し言葉の文章で答えてください。コード全文や秘密値は返答に含めないでください。\n"

const (
	// MaxSpokenCharacters を超える返答は読み上げを打ち切る。履歴には全文を残す
	MaxSpokenCharacters = 1100
	truncatedNotice     = "返答が長いため読み上げを省略しました。"
	// maxSentenceCharacters 文末記号がないまま長く続く場合は読点などで区切って合成する
	maxSentenceCharacters = 120
	skillFailedNotice     = "依頼を処理できませんでした。"
)

// TurnRunner はポートの中身を知らずに一往復を進める。組み立ては main が行う。
type TurnRunner struct {
	Listener     Listener
	Transcriber  Transcriber
	Responder    Responder
	Synthesizer  Synthesizer
	Player       Player
	Conversation *domain.Conversation
	Skills       []Skill          // AI より先に試す。nil なら全て AI へ渡す
	OnState      func(State)      // nil なら通知しない
	Now          func() time.Time // nil なら time.Now
}

// Timings は発話の終わり（録音完了）からの経過時間。返答の速さを測るために使う。
type Timings struct {
	Transcribe time.Duration // 文字起こし完了まで
	FirstChunk time.Duration // AIの最初の返答断片まで
	FirstAudio time.Duration // 最初の文の再生開始まで。体感の待ち時間
	Total      time.Duration // 最後の再生終了まで
}

type Result struct {
	Completed      bool
	Reason         string // 完了しなかった理由。no_speech、end_conversation、stop_server
	Skill          string // スキルが処理した場合はその名前。AI が返答した場合は空
	Stop           bool   // サーバーを止める指示だったか
	ReplyCharacter int
	Timings        Timings
}

func (r *TurnRunner) notify(s State) {
	if r.OnState != nil {
		r.OnState(s)
	}
}

func (r *TurnRunner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *TurnRunner) Run(ctx context.Context) (Result, error) {
	defer r.notify(StateWaiting) // 成功・失敗のどちらでも待ち受けに戻ったことを通知する
	r.notify(StateWaiting)

	pcm, err := r.Listener.Listen(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("listen: %w", err)
	}
	if len(pcm) == 0 {
		return Result{Reason: "no_speech"}, nil
	}
	start := r.now()
	var timings Timings

	r.notify(StateTranscribing)
	text, err := r.Transcriber.Transcribe(ctx, pcm)
	if err != nil {
		return Result{}, fmt.Errorf("transcribe: %w", err)
	}
	timings.Transcribe = r.now().Sub(start)
	if strings.TrimSpace(text) == "" {
		return Result{Reason: "no_speech", Timings: timings}, nil
	}
	if action := domain.Control(text); action != domain.ActionNone {
		r.Conversation.Clear()
		return Result{Reason: action.String(), Stop: action == domain.ActionStopServer, Timings: timings}, nil
	}

	r.notify(StateProcessing)
	name, replies := "", iter.Seq2[string, error](nil)
	if skill, invoke := r.matchSkill(text); invoke != nil {
		name = skill.Name()
		reply, err := invoke(ctx)
		if err != nil {
			// 失敗も声で知らせる。読み上げの失敗より、スキルの失敗を返す
			_, _ = r.respondAndSpeak(ctx, fixed(skillFailedNotice), start, &timings)
			return Result{}, fmt.Errorf("skill %s: %w", name, err)
		}
		replies = fixed(reply)
	} else {
		replies = r.Responder.Respond(ctx, VoiceInstruction+r.Conversation.Prompt(text))
	}
	reply, err := r.respondAndSpeak(ctx, replies, start, &timings)
	if err != nil {
		return Result{}, err
	}
	timings.Total = r.now().Sub(start)
	// スキルの結果も履歴に残し、続けて AI に「さっき何を追加した？」と聞けるようにする
	r.Conversation.Remember(text, reply)
	return Result{Completed: true, Skill: name, ReplyCharacter: len([]rune(reply)), Timings: timings}, nil
}

func (r *TurnRunner) matchSkill(text string) (Skill, Invocation) {
	for _, skill := range r.Skills {
		if invoke, ok := skill.Match(text); ok {
			return skill, invoke
		}
	}
	return nil, nil
}

// fixed は決まった返答を AI の返答と同じ形で流す。
func fixed(reply string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) { yield(reply, nil) }
}

// respondAndSpeak は 3 つの段を goroutine で並行に動かす。
//
//	受信: 返答 → 文に分割 ──sentences──▶ 合成: Synthesizer ──audios──▶ 再生: Player
//
// 再生中も受信と次の文の合成が進むので、文と文の間に合成待ちが入りにくい。
// どこかの段が失敗したら ctx を取り消し、他の段も止める。
func (r *TurnRunner) respondAndSpeak(parent context.Context, replies iter.Seq2[string, error], start time.Time, timings *Timings) (string, error) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	sentences := make(chan string, 4)
	audios := make(chan []byte, 1) // 先読みは1文まで。不要になる合成とメモリを抑える
	var reply strings.Builder
	var firstChunk time.Duration
	var wg sync.WaitGroup

	wg.Go(func() { // Go 1.25 の WaitGroup.Go は Add/Done を自動で行う
		defer close(sentences) // 送り手が close し、受け手の for range を終わらせる
		received := func() { firstChunk = r.now().Sub(start) }
		if err := r.receive(ctx, replies, &reply, sentences, received); err != nil {
			cancel(err)
		}
	})
	wg.Go(func() {
		defer close(audios)
		for sentence := range sentences {
			audio, err := r.Synthesizer.Synthesize(ctx, sentence)
			if err != nil {
				cancel(fmt.Errorf("synthesize: %w", err))
				return
			}
			if !send(ctx, audios, audio) {
				return
			}
		}
	})
	// 再生は呼び出し元の goroutine で行う。OnState も常にこの goroutine から呼ばれる
	if err := r.play(ctx, audios, func() { timings.FirstAudio = r.now().Sub(start) }); err != nil {
		cancel(err)
	}
	wg.Wait() // 全ての goroutine の終了を待ってから reply と firstChunk を読む
	timings.FirstChunk = firstChunk

	// 最初に cancel に渡された原因、または親 ctx の取り消し理由を返す
	if err := context.Cause(ctx); err != nil {
		return "", err
	}
	if strings.TrimSpace(reply.String()) == "" {
		return "", fmt.Errorf("respond: 返答が空です")
	}
	return reply.String(), nil
}

// receive は返答を受け取りながら文に区切って送る。読み上げの上限を超えたら、
// 省略の案内を送って読み上げだけを打ち切る。履歴用に返答は最後まで受け取る。
func (r *TurnRunner) receive(ctx context.Context, replies iter.Seq2[string, error], reply *strings.Builder, out chan<- string,
	onFirst func()) error {
	pending, spoken, truncated := "", 0, false
	emit := func(sentences []string) bool {
		for _, s := range sentences {
			if s = speakable(s); s == "" {
				continue
			}
			if truncated {
				return true
			}
			if n := len([]rune(s)); spoken+n > MaxSpokenCharacters {
				truncated = true
				s = truncatedNotice
			} else {
				spoken += n
			}
			if !send(ctx, out, s) {
				return false
			}
		}
		return true
	}
	for chunk, err := range replies {
		if err != nil {
			return fmt.Errorf("respond: %w", err)
		}
		if reply.Len() == 0 && chunk != "" {
			onFirst()
		}
		reply.WriteString(chunk)
		var sentences []string
		sentences, pending = splitSentences(pending + chunk)
		if !emit(sentences) {
			return nil // 取り消し済み。原因は取り消した側が記録している
		}
	}
	if rest := strings.TrimSpace(pending); rest != "" {
		emit([]string{rest})
	}
	return nil
}

func (r *TurnRunner) play(ctx context.Context, audios <-chan []byte, onFirst func()) error {
	first := true
	for audio := range audios {
		if first {
			first = false
			onFirst()
			r.notify(StateSpeaking)
		}
		if err := r.Player.Play(ctx, audio); err != nil {
			return fmt.Errorf("play: %w", err)
		}
	}
	return nil
}

// send は受け手が詰まっていても、取り消されたら諦めて false を返す。
// これがないと、相手の段が先に終わったときに goroutine が永遠に待ち続ける（goroutine リーク）。
// [T any] は型パラメーター（ジェネリクス）。string と []byte の両方の channel に使える。
func send[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	}
}

var (
	markdownLine   = regexp.MustCompile(`^\s*(#{1,6}\s+|[-*+]\s+|\d+[.)]\s+|>\s*)`)
	markdownInline = strings.NewReplacer("**", "", "__", "", "`", "", "|", " ")
)

// speakable は読み上げで記号が読まれないよう、Markdown の見出し・箇条書き・強調などを外す。
// 履歴には元の返答を残す。
func speakable(sentence string) string {
	sentence = markdownLine.ReplaceAllString(sentence, "")
	sentence = markdownInline.Replace(sentence)
	if strings.Trim(sentence, " \t-=*_#。、") == "" {
		return "" // 区切り線や記号だけの行
	}
	return strings.TrimSpace(sentence)
}

// splitSentences は文末記号までを完成した文として切り出し、残りを返す。
// 文末記号がないまま長くなった場合は、最後の読点（なければ上限の位置）で区切る。
func splitSentences(text string) (sentences []string, rest string) {
	start := 0
	for i, r := range text { // string を range すると i はバイト位置、r は文字(rune)
		if strings.ContainsRune("。！？!?\n", r) {
			end := i + len(string(r))
			if s := strings.TrimSpace(text[start:end]); s != "" {
				sentences = append(sentences, s)
			}
			start = end
		}
	}
	rest = text[start:]
	for len([]rune(rest)) > maxSentenceCharacters {
		runes := []rune(rest)
		cut := maxSentenceCharacters
		for i := maxSentenceCharacters - 1; i > 0; i-- {
			if strings.ContainsRune("、，,", runes[i]) {
				cut = i + 1
				break
			}
		}
		if s := strings.TrimSpace(string(runes[:cut])); s != "" {
			sentences = append(sentences, s)
		}
		rest = string(runes[cut:])
	}
	return sentences, rest
}
