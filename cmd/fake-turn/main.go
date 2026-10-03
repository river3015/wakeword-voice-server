// fake-turn は偽物の部品で一往復を実行し、状態の変化を経過時間つきで表示する。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/fake"
	"github.com/river3015/wakeword-voice-server/internal/domain"
	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

func main() {
	text := flag.String("text", "今日の予定を教えて", "文字起こし結果として使う依頼")
	reply := flag.String("reply", "今日は予定が二件あります。十時に定例、十五時に面談です。準備するものは特にありません。",
		"AIの返答として使う文")
	flag.Parse()

	// Ctrl-C で ctx が取り消され、待機中の処理が中断される
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	start := time.Now()
	clock := func() string { return fmt.Sprintf("[%5dms]", time.Since(start).Milliseconds()) }

	conversation, err := domain.NewConversation(5*time.Minute, 6, 16000, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	// 組み立て：ここで偽物を本物の adapter に差し替えれば、usecase は変えずに動く
	runner := &usecase.TurnRunner{
		Listener:    fake.Listener{Delay: 300 * time.Millisecond},
		Transcriber: fake.Transcriber{Text: *text, Delay: 450 * time.Millisecond},
		Responder: fake.Responder{Reply: *reply, FirstDelay: 1500 * time.Millisecond,
			ChunkDelay: 60 * time.Millisecond, ChunkCharacters: 3},
		Synthesizer: fake.Synthesizer{Out: os.Stdout, Base: 200 * time.Millisecond,
			PerCharacter: 15 * time.Millisecond, Clock: clock},
		Player:       fake.Player{Out: os.Stdout, PerCharacter: 80 * time.Millisecond, Clock: clock},
		Conversation: conversation,
		OnState:      func(s usecase.State) { fmt.Printf("%s state=%s\n", clock(), s) },
	}

	result, err := runner.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	fmt.Printf("%s result=%+v\n", clock(), result) // %+v はフィールド名つきで表示する
}
