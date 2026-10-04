// wakeword はウェイクワードで起動する音声会話サーバー。
//
//	wakeword serve --config config.local.toml     継続して待ち受ける
//	wakeword once  --config config.local.toml     一往復だけ実行する（検証用）
//	wakeword devices                              音声デバイスを一覧する
//	wakeword detect-wake --config ... file.wav    WAVでウェイクワード検出を確かめる
//	wakeword launch-agent --config ...            LaunchAgent の plist を生成する（登録はしない）
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
	"github.com/river3015/wakeword-voice-server/internal/config"
	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

const usage = `使い方: wakeword <command> [options]

commands:
  serve         継続して待ち受ける
  once          一往復だけ実行する（--source-wav / --request-wav で録音済み音声も使える）
  devices       音声デバイスを一覧する
  detect-wake   WAVでウェイクワード検出を確かめる
  launch-agent  LaunchAgent の plist を .runtime に生成する（登録はしない）
`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(ctx, os.Args[2:])
	case "once":
		err = once(ctx, os.Args[2:])
	case "devices":
		err = devices()
	case "detect-wake":
		err = detectWake(os.Args[2:])
	case "launch-agent":
		err = launchAgent(os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, errStopped) {
		slog.Info("停止しました")
		return
	}
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

var errStopped = errors.New("stopped")

func loadConfig(flags *flag.FlagSet, args []string) (*config.Config, error) {
	path := flags.String("config", "config.local.toml", "設定ファイル")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	return config.Load(*path)
}

func serve(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	maxCycles := flags.Int("max-cycles", 0, "検証用の待ち受け回数上限。0なら無制限")
	listen := flags.Duration("listen", 0, "1回の待ち受けの上限（例: 30s）。0なら無制限")
	cfg, err := loadConfig(flags, args)
	if err != nil {
		return err
	}
	a, ctx, err := start(ctx, cfg, true)
	if err != nil {
		return err
	}
	defer a.close()
	runner := a.runner(a.microphoneListener(*listen))
	server := &usecase.Server{Runner: runner,
		OnResult: printResult,
		OnError:  func(err error) { slog.Error("turn failed", "error", err.Error()) }}
	server.Serve(ctx, *maxCycles)
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause // 子プロセスが落ちた場合など。launchd に再起動させるため異常終了する
	}
	return nil
}

func once(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("once", flag.ContinueOnError)
	sourceWAV := flags.String("source-wav", "", "マイクの代わりに使う、呼びかけを含むWAV")
	var requestWAVs stringList
	flags.Var(&requestWAVs, "request-wav", "呼びかけなしの日本語依頼WAV。複数指定すると同じ会話で順に依頼する")
	listen := flags.Duration("listen", 30*time.Second, "呼びかけを待つ上限")
	cfg, err := loadConfig(flags, args)
	if err != nil {
		return err
	}
	if *sourceWAV != "" && len(requestWAVs) > 0 {
		return errors.New("--source-wav と --request-wav は同時に指定できません")
	}
	a, ctx, err := start(ctx, cfg, *sourceWAV == "" && len(requestWAVs) == 0)
	if err != nil {
		return err
	}
	defer a.close()
	var listeners []usecase.Listener
	switch {
	case len(requestWAVs) > 0:
		for _, path := range requestWAVs {
			listeners = append(listeners, audio.RequestWAV{Path: path})
		}
	case *sourceWAV != "":
		listeners = append(listeners, a.captureListener(audio.WAVFile{Path: *sourceWAV}, 0))
	default:
		listeners = append(listeners, a.microphoneListener(*listen))
	}
	// 同じ app.conversation を使うので、2件目以降は前の依頼と返答を履歴として渡す
	for _, listener := range listeners {
		result, err := a.runner(listener).Run(ctx)
		if err != nil {
			return err
		}
		printResult(result)
	}
	return nil
}

// stringList は繰り返し指定できるフラグ。
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

func printResult(r usecase.Result) {
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	out, _ := json.Marshal(map[string]any{
		"completed": r.Completed, "reason": r.Reason, "skill": r.Skill, "reply_characters": r.ReplyCharacter,
		"timings_ms": map[string]int64{"transcribe": ms(r.Timings.Transcribe), "first_chunk": ms(r.Timings.FirstChunk),
			"first_audio": ms(r.Timings.FirstAudio), "total": ms(r.Timings.Total)},
	})
	fmt.Println(string(out))
}

func devices() error {
	d, err := audio.NewDevices()
	if err != nil {
		return err
	}
	defer d.Close()
	for _, capture := range []bool{true, false} {
		names, err := d.List(capture)
		if err != nil {
			return err
		}
		label := "出力"
		if capture {
			label = "入力"
		}
		for _, name := range names {
			fmt.Printf("%s: %s\n", label, name)
		}
	}
	return nil
}
