package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadResolvesPathsFromConfigLocation(t *testing.T) {
	path := write(t, `workdir = "work"
ai_executable = "bin/codex"
reasoning_effort = "low"
[capture]
silence_seconds = 0.8
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if c.Path(c.Workdir) != filepath.Join(dir, "work") || c.Executable(c.AIExecutable) != filepath.Join(dir, "bin/codex") {
		t.Errorf("workdir = %s, executable = %s", c.Path(c.Workdir), c.Executable(c.AIExecutable))
	}
	if c.Executable("codex") != "codex" {
		t.Error("PATH から探す名前まで解決した")
	}
	// 指定した項目だけ上書きされ、他は既定値のまま
	if c.Capture.SilenceSeconds != 0.8 || c.Capture.WakeThreshold != 0.5 || c.VoicevoxCredit != "VOICEVOX:四国めたん" {
		t.Errorf("capture = %+v, credit = %q", c.Capture, c.VoicevoxCredit)
	}
}

func TestLegacyAndUnknownKeys(t *testing.T) {
	if _, err := Load(write(t, `whisper_cli = "x"`)); err == nil || !strings.Contains(err.Error(), "whisper_server") {
		t.Errorf("err = %v", err)
	}
	if _, err := Load(write(t, `typo = 1`)); err == nil || !strings.Contains(err.Error(), "typo") {
		t.Errorf("err = %v", err)
	}
}

func TestOtherSpeakerRequiresCredit(t *testing.T) {
	if _, err := Load(write(t, `speaker = 3`)); err == nil {
		t.Error("クレジットなしで別の音声を受け付けた")
	}
	if _, err := Load(write(t, "speaker = 3\nvoicevox_credit = \"VOICEVOX:ずんだもん\"")); err != nil {
		t.Error(err)
	}
	if _, err := Load(write(t, `sandbox = "danger-full-access"`)); err == nil {
		t.Error("危険なsandboxを受け付けた")
	}
}

func TestProvider(t *testing.T) {
	c, err := Load(write(t, `provider = "claude"`))
	if err != nil || c.Provider != "claude" || c.AIExecutable != "claude" {
		t.Fatalf("config = %+v, err = %v", c, err)
	}
	c, err = Load(write(t, ""))
	if err != nil || c.Provider != "codex" || c.AIExecutable != "codex" {
		t.Errorf("既定の連携先 = %q, %q", c.Provider, c.AIExecutable)
	}
	if _, err := Load(write(t, `provider = "gemini"`)); err == nil {
		t.Error("未対応の連携先を受け付けた")
	}
}
