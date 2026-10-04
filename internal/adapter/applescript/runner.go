// Package applescript は osascript で macOS のアプリを操作する。
// 値はスクリプトに埋め込まず、引数（on run argv）で渡す。
package applescript

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/process"
)

type Runner struct {
	App        string // エラーに使うアプリ名。例: リマインダー
	Executable string // 空なら /usr/bin/osascript
	Timeout    time.Duration
}

// errorNumber は osascript のエラー出力にある番号。-1743 は自動化が許可されていない。
var errorNumber = regexp.MustCompile(`\((-?\d+)\)\s*$`)

func (r *Runner) Run(parent context.Context, script string, args ...string) (string, error) {
	executable, timeout := r.Executable, r.Timeout
	if executable == "" {
		executable = "/usr/bin/osascript"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// -- の後ろは、- で始まる値でも osascript のオプションとして扱われない
	command := process.Command(ctx, executable, append([]string{"-e", script, "--"}, args...), "")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil && parent.Err() == nil {
			return "", fmt.Errorf("%sの操作がタイムアウトしました。自動化の許可を確認してください", r.App)
		}
		// エラー出力には項目名などが含まれ得るので、番号だけを返す
		if m := errorNumber.FindStringSubmatch(strings.TrimSpace(stderr.String())); m != nil {
			if m[1] == "-1743" {
				return "", fmt.Errorf("%sの操作が許可されていません。システム設定のオートメーションで許可してください", r.App)
			}
			return "", fmt.Errorf("%sを操作できません（osascript %s）", r.App, m[1])
		}
		return "", fmt.Errorf("%sを操作できません: %w", r.App, err)
	}
	return stdout.String(), nil
}

// Lines は改行区切りの出力を、空行を除いて分ける。
func Lines(out string) []string {
	var result []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}
