// Package reminders は macOS のリマインダー.app を osascript で操作する。
// リスト名と項目はスクリプトに埋め込まず、引数（on run argv）で渡す。
package reminders

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/applescript"
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

func (r *Reminders) run(ctx context.Context, script string, args ...string) (string, error) {
	runner := applescript.Runner{App: "リマインダー", Executable: r.Executable, Timeout: r.Timeout}
	return runner.Run(ctx, script, args...)
}

// Check はリマインダーを操作できるか、必要なリストがあるかを確かめる。
// 初回はmacOSが自動化の許可を求める。
func (r *Reminders) Check(ctx context.Context, required []string) error {
	out, err := r.run(ctx, listsScript)
	if err != nil {
		return err
	}
	existing := applescript.Lines(out)
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
	return applescript.Lines(out), nil
}
