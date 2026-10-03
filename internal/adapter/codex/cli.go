// Package codex は公式 Codex CLI の codex exec で返答を得る。
// 依頼文はコマンド引数に入れず標準入力で渡し、返答は一時ファイルから読んで削除する。
package codex

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/river3015/wakeword-voice-server/internal/adapter/process"
)

type CLI struct {
	Executable string
	Workdir    string
	Sandbox    string // read-only または workspace-write
	Timeout    time.Duration
	// Model、ReasoningEffort は空なら Codex の既定。推論の深さを下げると返答が速くなる
	Model           string
	ReasoningEffort string
}

func (c *CLI) Validate() error {
	if c.Sandbox != "read-only" && c.Sandbox != "workspace-write" {
		return errors.New("sandboxはread-onlyかworkspace-writeにしてください")
	}
	if info, err := os.Stat(c.Workdir); err != nil || !info.IsDir() {
		return errors.New("AI作業ディレクトリがありません")
	}
	if c.Timeout <= 0 || c.Timeout > 30*time.Minute {
		return errors.New("ai_timeoutは0より大きく1800秒以下にしてください")
	}
	switch c.ReasoningEffort {
	case "", "minimal", "low", "medium", "high":
	default:
		return errors.New("reasoning_effortはminimal、low、medium、highのいずれかにしてください")
	}
	return nil
}

func (c *CLI) args(output string) []string {
	args := []string{"exec", "--ephemeral", "--ignore-user-config", "--sandbox", c.Sandbox,
		"-c", `approval_policy="never"`, "--color", "never", "-o", output}
	if c.Model != "" {
		args = append(args, "-m", c.Model)
	}
	if c.ReasoningEffort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", c.ReasoningEffort))
	}
	return append(args, "-")
}

// Respond は Codex CLI が逐次の返答を出さないため、完成した返答を1つの断片として返す。
func (c *CLI) Respond(ctx context.Context, prompt string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		reply, err := c.ask(ctx, prompt)
		yield(reply, err)
	}
}

func (c *CLI) ask(parent context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" || utf8.RuneCountInString(prompt) > 30000 {
		return "", errors.New("AIに渡す依頼が空か長すぎます")
	}
	folder, err := os.MkdirTemp("", "wakeword-ai-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(folder)
	output := filepath.Join(folder, "response.txt")

	ctx, cancel := context.WithTimeout(parent, c.Timeout)
	defer cancel()
	command := process.Command(ctx, c.Executable, c.args(output), c.Workdir)
	command.Stdin = strings.NewReader(prompt)
	// 標準出力・エラーには依頼内容が含まれ得るので保存しない（nil は /dev/null）
	err = command.Run()
	process.KillGroup(command)
	if ctx.Err() != nil {
		if parent.Err() != nil {
			return "", parent.Err()
		}
		return "", errors.New("AI処理がタイムアウトしました")
	}
	if err != nil {
		return "", errors.New("AIの実行に失敗しました。認証・利用上限・承認条件を確認してください")
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() > 65536 {
		return "", errors.New("AIの返答がないか長すぎます")
	}
	data, err := os.ReadFile(output)
	if err != nil || !utf8.Valid(data) {
		return "", errors.New("AIの返答を読み込めません")
	}
	reply := strings.TrimSpace(string(data))
	if reply == "" {
		return "", errors.New("AIの返答が空です")
	}
	return reply, nil
}
