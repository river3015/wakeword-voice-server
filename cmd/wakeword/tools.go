package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
	"github.com/river3015/wakeword-voice-server/internal/adapter/onnx"
	"github.com/river3015/wakeword-voice-server/internal/config"
)

// detectWake は録音済み WAV でウェイクワードのスコアを確かめる。
func detectWake(args []string) error {
	flags := flag.NewFlagSet("detect-wake", flag.ContinueOnError)
	cfg, err := loadConfig(flags, args)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("WAVファイルを1つ指定してください")
	}
	samples, err := audio.ReadRequestWAV(flags.Arg(0))
	if err != nil {
		return err
	}
	if err := onnx.Init(cfg.Path(cfg.ONNXRuntime)); err != nil {
		return err
	}
	models := cfg.WakeModels()
	wake, err := onnx.NewWakeWord(models...)
	if err != nil {
		return err
	}
	defer wake.Close()
	peaks, firsts := make([]float64, len(models)), make([]float64, len(models))
	for i := range firsts {
		firsts[i] = -1
	}
	for i := 0; i < len(samples); i += 1280 {
		frame := make([]int16, 1280)
		copy(frame, samples[i:])
		scores, err := wake.Predict(frame)
		if err != nil {
			return err
		}
		for j, score := range scores {
			peaks[j] = max(peaks[j], score)
			if firsts[j] < 0 && score >= cfg.Capture.WakeThreshold {
				firsts[j] = float64(i) / 16000
			}
		}
	}
	// ウェイクワードごとに1行ずつ出す
	for j, model := range models {
		result := map[string]any{"wake": config.WakeName(model), "detected": firsts[j] >= 0, "peak_score": peaks[j]}
		if firsts[j] >= 0 {
			result["first_frame_seconds"] = firsts[j]
		}
		out, _ := json.Marshal(result)
		fmt.Println(string(out))
	}
	return nil
}

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>local.wakeword-voice-server</string>
	<key>ProgramArguments</key>
	<array><string>{{binary}}</string><string>serve</string><string>--config</string><string>{{config}}</string></array>
	<key>WorkingDirectory</key><string>{{root}}</string>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>ThrottleInterval</key><integer>10</integer>
	<key>StandardOutPath</key><string>{{root}}/.runtime/server.log</string>
	<key>StandardErrorPath</key><string>{{root}}/.runtime/server-error.log</string>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string></dict>
</dict>
</plist>
`

// launchAgent は確認用の plist を生成するだけで、登録はしない。
func launchAgent(args []string) error {
	flags := flag.NewFlagSet("launch-agent", flag.ContinueOnError)
	path := flags.String("config", "config.local.toml", "設定ファイル")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err != nil {
		return fmt.Errorf("設定ファイルがありません: %w", err)
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.Contains(binary, "go-build") {
		return errors.New("go run ではなく、go build で作った実行ファイルから実行してください")
	}
	configPath, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	root := filepath.Dir(configPath)
	escape := func(s string) string {
		var b strings.Builder
		xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	plist := strings.NewReplacer("{{binary}}", escape(binary), "{{config}}", escape(configPath),
		"{{root}}", escape(root)).Replace(plistTemplate)
	output := filepath.Join(root, ".runtime/local.wakeword-voice-server.plist")
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(output, []byte(plist), 0o644); err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}
