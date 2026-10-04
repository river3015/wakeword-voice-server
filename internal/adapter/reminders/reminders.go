// Package reminders は macOS のリマインダー.app を osascript で操作する。
// リスト名と項目はスクリプトに埋め込まず、引数（on run argv）で渡す。
package reminders

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/process"
)

const (
	listsScript = `on run argv
	tell application "Reminders" to set names to name of every list
	set AppleScript's text item delimiters to linefeed
	return names as text
end run`
	addScript = `on run argv
	tell application "Reminders"
		tell list (item 1 of argv) to set created to make new reminder with properties {name:(item 2 of argv)}
		return name of created
	end tell
end run`
	itemsScript = `on run argv
	tell application "Reminders" to set names to name of (reminders of list (item 1 of argv) whose completed is false)
	set AppleScript's text item delimiters to linefeed
	return names as text
end run`
)

type Reminders struct {
	Executable string // 空なら /usr/bin/osascript
	Timeout    time.Duration
}

// Check はリマインダーを操作できるか、必要なリストがあるかを確かめる。
// 初回はmacOSが自動化の許可を求める。
func (r *Reminders) Check(ctx context.Context, required []string) error {
	out, err := r.run(ctx, listsScript)
	if err != nil {
		return err
	}
	existing := lines(out)
	var missing []string
	for _, name := range required {
		if !slices.Contains(existing, name) && !slices.Contains(missing, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("リマインダーにリストがありません: %s。リマインダー.appで作成するか、設定のリスト名を変えてください",
			strings.Join(missing, "、"))
	}
	return nil
}

func (r *Reminders) Add(ctx context.Context, list, item string) (string, error) {
	out, err := r.run(ctx, addScript, list, item)
	if err != nil {
		return "", err
	}
	stored := strings.TrimSpace(out)
	if stored == "" {
		return "", errors.New("リマインダーに追加した項目を確認できません")
	}
	return stored, nil
}

func (r *Reminders) Items(ctx context.Context, list string) ([]string, error) {
	out, err := r.run(ctx, itemsScript, list)
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// errorNumber は osascript のエラー出力にある番号。-1743 は自動化が許可されていない。
var errorNumber = regexp.MustCompile(`\((-?\d+)\)\s*$`)

func (r *Reminders) run(parent context.Context, script string, args ...string) (string, error) {
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
			return "", errors.New("リマインダーの操作がタイムアウトしました。自動化の許可を確認してください")
		}
		// エラー出力には項目名が含まれ得るので、番号だけを返す
		if m := errorNumber.FindStringSubmatch(strings.TrimSpace(stderr.String())); m != nil {
			if m[1] == "-1743" {
				return "", errors.New("リマインダーの操作が許可されていません。システム設定のオートメーションで許可してください")
			}
			return "", fmt.Errorf("リマインダーを操作できません（osascript %s）", m[1])
		}
		return "", fmt.Errorf("リマインダーを操作できません: %w", err)
	}
	return stdout.String(), nil
}

func lines(out string) []string {
	var result []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}
