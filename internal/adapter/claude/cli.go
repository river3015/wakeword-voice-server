// Package claude は公式 Claude Code CLI の claude -p で返答を逐次受け取る。
// 依頼文はコマンド引数に入れず標準入力で渡す。
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/river3015/wakeword-voice-server/internal/adapter/process"
)

type CLI struct {
	Executable string
	Workdir    string
	Sandbox    string // read-only のみ対応
	Timeout    time.Duration
	// Model、Effort は空なら Claude Code の既定
	Model  string
	Effort string
}

func (c *CLI) Validate() error {
	if c.Sandbox != "read-only" {
		// 編集を許すには編集の自動承認（acceptEdits）が要る。承認を弱めるため対応しない
		return errors.New("Claudeはsandbox = \"read-only\"のみ対応しています")
	}
	if info, err := os.Stat(c.Workdir); err != nil || !info.IsDir() {
		return errors.New("AI作業ディレクトリがありません")
	}
	if c.Timeout <= 0 || c.Timeout > 30*time.Minute {
		return errors.New("ai_timeoutは0より大きく1800秒以下にしてください")
	}
	switch c.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return errors.New("Claudeのreasoning_effortはlow、medium、high、xhigh、maxのいずれかにしてください")
	}
	return nil
}

func (c *CLI) args() []string {
	// 読み取り用のツールだけを使え、それ以外は確認を求めずに拒否する。MCP サーバーは読み込まない
	args := []string{"-p", "--output-format", "stream-json", "--include-partial-messages", "--verbose",
		"--no-session-persistence", "--strict-mcp-config", "--permission-mode", "dontAsk",
		"--tools", "Read,Glob,Grep"}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if c.Effort != "" {
		args = append(args, "--effort", c.Effort)
	}
	return args
}

// event は stream-json の1行のうち、使う項目だけを読む。
type event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Event   *struct {
		Type  string `json:"type"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	} `json:"event"`
}

func (c *CLI) Respond(parent context.Context, prompt string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		if strings.TrimSpace(prompt) == "" || utf8.RuneCountInString(prompt) > 30000 {
			yield("", errors.New("AIに渡す依頼が空か長すぎます"))
			return
		}
		ctx, cancel := context.WithTimeout(parent, c.Timeout)
		defer cancel()
		command := process.Command(ctx, c.Executable, c.args(), c.Workdir)
		command.Stdin = strings.NewReader(prompt)
		stdout, err := command.StdoutPipe()
		if err != nil {
			yield("", err)
			return
		}
		// 標準エラーには依頼内容が含まれ得るので保存しない
		if err := command.Start(); err != nil {
			yield("", fmt.Errorf("Claude CLIを起動できません: %w", err))
			return
		}
		// 返答が終わった時点で戻り、CLI の後処理は待たずに止める
		defer func() {
			cancel()
			_ = command.Wait()
			process.KillGroup(command)
		}()

		stopped := false
		guarded := func(text string, err error) bool {
			if !yield(text, err) {
				stopped = true
			}
			return !stopped
		}
		finished, received, err := c.read(stdout, guarded)
		if stopped {
			return // 呼び出し側がループを抜けた後に yield を呼ぶと実行時エラーになる
		}
		if err == nil && !finished {
			if waitErr := command.Wait(); ctx.Err() == nil && waitErr != nil {
				err = errors.New("AIの実行に失敗しました。認証・利用上限・承認条件を確認してください")
			}
		}
		switch {
		case parent.Err() != nil:
			yield("", parent.Err())
		case ctx.Err() != nil && !finished:
			yield("", errors.New("AI処理がタイムアウトしました"))
		case err != nil:
			yield("", err)
		case !received:
			yield("", errors.New("AIの返答が空です"))
		}
	}
}

// read は返答の断片を yield へ渡す。最終の返答が終わったら finished を true にする。
// yield が false を返した（呼び出し側が受け取りをやめた）場合も finished として扱う。
func (c *CLI) read(stdout io.Reader, yield func(string, error) bool) (finished, received bool, err error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	ended := false
	for scanner.Scan() {
		var e event
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue // 将来の形式変更で読めない行は無視する
		}
		switch {
		case e.Type == "stream_event" && e.Event != nil:
			switch e.Event.Type {
			case "content_block_delta":
				if e.Event.Delta.Type == "text_delta" && e.Event.Delta.Text != "" {
					received = true
					if !yield(e.Event.Delta.Text, nil) {
						return true, received, nil
					}
				}
			case "message_delta":
				// tool_use はツール実行後に続きがある。それ以外は最終の返答の終わり
				ended = e.Event.Delta.StopReason != "" && e.Event.Delta.StopReason != "tool_use"
			case "message_stop":
				if ended {
					return true, received, nil
				}
			}
		case e.Type == "result":
			if e.IsError || e.Subtype != "success" {
				return true, received, errors.New("AIの実行に失敗しました。認証・利用上限・承認条件を確認してください")
			}
			return true, received, nil
		}
	}
	return false, received, scanner.Err()
}
