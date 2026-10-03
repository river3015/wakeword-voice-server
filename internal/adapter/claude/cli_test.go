package claude

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func delta(text string) string {
	return `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"` + text + `"}}}`
}

func stop(reason string) string {
	return `{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"` + reason + `"}}}
{"type":"stream_event","event":{"type":"message_stop"}}`
}

// fakeCLI は引数と標準入力を記録し、与えた行を出力した後に body を実行するシェルスクリプト。
func fakeCLI(t *testing.T, lines []string, body string) (*CLI, string) {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + record + ".args'\ncat > '" + record + ".stdin'\ncat <<'END'\n" +
		strings.Join(lines, "\n") + "\nEND\n" + body + "\n"
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &CLI{Executable: path, Workdir: dir, Sandbox: "read-only", Timeout: 5 * time.Second}, record
}

func collect(c *CLI) ([]string, error) {
	var chunks []string
	for chunk, err := range c.Respond(context.Background(), "private prompt") {
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func TestStreamsTextAndReturnsWithoutWaitingForExit(t *testing.T) {
	lines := []string{`{"type":"system","subtype":"init"}`, delta("こんにちは。"), delta("元気です。"), stop("end_turn")}
	c, record := fakeCLI(t, lines, "sleep 10") // 返答後の後処理を模す
	c.Effort = "low"
	start := time.Now()
	chunks, err := collect(c)
	if err != nil || !slices.Equal(chunks, []string{"こんにちは。", "元気です。"}) {
		t.Fatalf("chunks = %q, err = %v", chunks, err)
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("返答の終了後もCLIの終了を待っている: %v", time.Since(start))
	}
	args, _ := os.ReadFile(record + ".args")
	list := strings.Split(strings.TrimSpace(string(args)), "\n")
	for _, want := range []string{"dontAsk", "Read,Glob,Grep", "--no-session-persistence", "--strict-mcp-config", "low"} {
		if !slices.Contains(list, want) {
			t.Errorf("引数に %s がない: %q", want, list)
		}
	}
	if slices.Contains(list, "private prompt") {
		t.Error("依頼文が引数に入っている")
	}
	if stdin, _ := os.ReadFile(record + ".stdin"); string(stdin) != "private prompt" {
		t.Errorf("stdin = %q", stdin)
	}
}

func TestTextAfterToolUseIsIncluded(t *testing.T) {
	lines := []string{delta("確認します。"), stop("tool_use"), delta("ファイルは3つです。"), stop("end_turn")}
	c, _ := fakeCLI(t, lines, "")
	chunks, err := collect(c)
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks = %q, err = %v", chunks, err)
	}
}

func TestErrors(t *testing.T) {
	c, _ := fakeCLI(t, []string{`{"type":"result","subtype":"error_during_execution","is_error":true}`}, "exit 1")
	if _, err := collect(c); err == nil || !strings.Contains(err.Error(), "実行に失敗") {
		t.Errorf("result error: %v", err)
	}
	c, _ = fakeCLI(t, nil, "exit 1")
	if _, err := collect(c); err == nil || !strings.Contains(err.Error(), "実行に失敗") {
		t.Errorf("exit error: %v", err)
	}
	c, _ = fakeCLI(t, []string{`{"type":"result","subtype":"success","is_error":false}`}, "")
	if _, err := collect(c); err == nil || !strings.Contains(err.Error(), "空") {
		t.Errorf("empty reply: %v", err)
	}
	c, _ = fakeCLI(t, []string{delta("途中")}, "sleep 10")
	c.Timeout = 300 * time.Millisecond
	if _, err := collect(c); err == nil || !strings.Contains(err.Error(), "タイムアウト") {
		t.Errorf("timeout: %v", err)
	}
}

func TestConsumerCanStopEarly(t *testing.T) {
	c, _ := fakeCLI(t, []string{delta("一。"), delta("二。"), stop("end_turn")}, "sleep 10")
	for range c.Respond(context.Background(), "q") {
		break // ここで抜けても実行時エラーにならないこと
	}
}

func TestValidate(t *testing.T) {
	c, _ := fakeCLI(t, nil, "")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Sandbox = "workspace-write"
	if c.Validate() == nil {
		t.Error("workspace-writeを受け付けた")
	}
	c.Sandbox, c.Effort = "read-only", "minimal"
	if c.Validate() == nil {
		t.Error("Claudeにない推論設定を受け付けた")
	}
}
