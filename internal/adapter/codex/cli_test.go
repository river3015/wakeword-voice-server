package codex

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeCLI は引数と標準入力を記録し、-o の次の引数へ返答を書くシェルスクリプト。
func fakeCLI(t *testing.T, body string) (*CLI, string) {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "codex")
	os.WriteFile(script, []byte(`#!/bin/sh
printf '%s\n' "$@" > "`+record+`.args"
cat > "`+record+`.stdin"
while [ $# -gt 0 ]; do if [ "$1" = "-o" ]; then out="$2"; fi; shift; done
`+body+"\n"), 0o755)
	return &CLI{Executable: script, Workdir: dir, Sandbox: "read-only", Timeout: 5 * time.Second}, record
}

func collect(c *CLI, prompt string) (string, error) {
	var reply string
	var err error
	for chunk, e := range c.Respond(context.Background(), prompt) {
		reply, err = chunk, e
	}
	return reply, err
}

func TestPromptIsSentOnStdinAndOutputRemoved(t *testing.T) {
	c, record := fakeCLI(t, `printf '返答です' > "$out"; echo "$out" > "`+filepath.Join(t.TempDir(), "x")+`"`)
	c.ReasoningEffort = "low"
	reply, err := collect(c, "private prompt")
	if err != nil || reply != "返答です" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	args, _ := os.ReadFile(record + ".args")
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if slices.Contains(lines, "private prompt") || !slices.Contains(lines, "--ephemeral") ||
		!slices.Contains(lines, "read-only") || !slices.Contains(lines, `model_reasoning_effort="low"`) {
		t.Errorf("args = %q", lines)
	}
	stdin, _ := os.ReadFile(record + ".stdin")
	if !strings.HasSuffix(string(stdin), "private prompt") {
		t.Errorf("stdin = %q", stdin)
	}
	output := lines[slices.Index(lines, "-o")+1]
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Error("返答の一時ファイルが残っている")
	}
}

func TestFailureAndTimeout(t *testing.T) {
	c, _ := fakeCLI(t, "exit 1")
	if _, err := collect(c, "q"); err == nil || !strings.Contains(err.Error(), "実行に失敗") {
		t.Errorf("err = %v", err)
	}
	c, _ = fakeCLI(t, "sleep 10")
	c.Timeout = 200 * time.Millisecond
	start := time.Now()
	if _, err := collect(c, "q"); err == nil || !strings.Contains(err.Error(), "タイムアウト") {
		t.Errorf("err = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("タイムアウト後すぐに止まらない")
	}
}

func TestValidate(t *testing.T) {
	c, _ := fakeCLI(t, "")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Sandbox = "danger-full-access"
	if c.Validate() == nil {
		t.Error("危険なsandboxを受け付けた")
	}
	if _, err := collect(&CLI{Timeout: time.Second}, " "); err == nil {
		t.Error("空の依頼を受け付けた")
	}
}
