// Package config は TOML の設定ファイルを読み、パスを設定ファイルの場所から解決する。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/river3015/wakeword-voice-server/internal/domain"
)

type Conversation struct {
	TTL           float64 `toml:"ttl"`
	MaxTurns      int     `toml:"max_turns"`
	MaxCharacters int     `toml:"max_characters"`
}

// Lists はリマインダーのリストへ追加・読み上げるスキル。各項目は保存先のリスト名
type Lists struct {
	Enabled  bool   `toml:"enabled"`
	Shopping string `toml:"shopping"`
	ToDo     string `toml:"todo"`
	Memo     string `toml:"memo"`
}

// Weather は設定した1地点の天気予報を読み上げるスキル
type Weather struct {
	Enabled   bool    `toml:"enabled"`
	Latitude  float64 `toml:"latitude"`
	Longitude float64 `toml:"longitude"`
}

// Music はミュージック.appを操作するスキル
type Music struct {
	Enabled bool `toml:"enabled"`
}

// WakeWord は wake_model に加えて待ち受けるウェイクワード
type WakeWord struct {
	Model string `toml:"model"`
	// Action は検出したときの動作。turn（既定）は wake_model と同じく依頼を聞いて一往復する。
	// discord は依頼を聞かずに Discord のボイスチャンネルに入る
	Action string `toml:"action"`
}

// ウェイクワードを検出したときの動作。
const (
	WakeActionTurn    = "turn"    // 依頼を聞いて一往復する
	WakeActionDiscord = "discord" // 依頼を聞かずに Discord のボイスチャンネルに入り、抜けるまで待ち受けを止める
)

// Discord は本人を入れるボイスチャンネル。Client Secret とトークンはキーチェーンに置く
type Discord struct {
	ClientID  string `toml:"client_id"`  // Developer Portal のアプリの ID
	ChannelID string `toml:"channel_id"` // ボイスチャンネルの ID
}

// Skills は AI を通さずに処理するスキル。既定ではすべて無効
type Skills struct {
	Lists   Lists   `toml:"lists"`
	Weather Weather `toml:"weather"`
	Music   Music   `toml:"music"`
}

type Config struct {
	// AI：codex（Codex CLI）または claude（Claude Code CLI）
	Provider        string  `toml:"provider"`
	AIExecutable    string  `toml:"ai_executable"`
	Workdir         string  `toml:"workdir"`
	Sandbox         string  `toml:"sandbox"`
	AITimeout       float64 `toml:"ai_timeout"`
	Model           string  `toml:"model"`
	ReasoningEffort string  `toml:"reasoning_effort"`

	// 検出
	ONNXRuntime string     `toml:"onnxruntime_library"`
	WakeModel   string     `toml:"wake_model"`
	WakeWords   []WakeWord `toml:"wake_words"`
	VADModel    string     `toml:"vad_model"`

	// 文字起こし。whisper_server が空なら whisper_url の起動済みサーバーを使う
	WhisperServer  string  `toml:"whisper_server"`
	WhisperModel   string  `toml:"whisper_model"`
	WhisperURL     string  `toml:"whisper_url"`
	WhisperThreads int     `toml:"whisper_threads"`
	GPU            bool    `toml:"gpu"`
	STTTimeout     float64 `toml:"stt_timeout"`

	// 読み上げ。voicevox_engine を指定すると、未起動のときだけ起動する
	VoicevoxURL    string `toml:"voicevox_url"`
	VoicevoxEngine string `toml:"voicevox_engine"`
	Speaker        int    `toml:"speaker"`
	VoicevoxCredit string `toml:"voicevox_credit"`

	// 音声デバイス。名前の一部。空なら OS の既定
	InputDevice  string `toml:"input_device"`
	OutputDevice string `toml:"output_device"`
	ReceiptCue   bool   `toml:"receipt_cue"`
	PreventSleep bool   `toml:"prevent_sleep"`

	// whisper-server と VOICEVOX を使わなくなってから止めるまでの秒数。0 なら起動時から常駐させる
	ServiceIdleTimeout float64 `toml:"service_idle_timeout"`

	Capture      domain.CaptureSettings `toml:"capture"`
	Conversation Conversation           `toml:"conversation"`
	Skills       Skills                 `toml:"skills"`
	Discord      Discord                `toml:"discord"`

	base string
}

func defaults() Config {
	return Config{
		Provider: "codex", Workdir: ".", Sandbox: "read-only", AITimeout: 180,
		ONNXRuntime:  ".vendor/onnxruntime/onnxruntime-osx-arm64-1.29.0/lib/libonnxruntime.1.29.0.dylib",
		WakeModel:    ".models/openwakeword/hey_mycroft_v0.1.onnx",
		VADModel:     ".models/openwakeword/silero_vad.onnx",
		WhisperModel: ".models/whisper/ggml-base.bin", WhisperURL: "http://127.0.0.1:8178",
		WhisperThreads: 4, GPU: true, STTTimeout: 60,
		VoicevoxURL: "http://127.0.0.1:50021", VoicevoxCredit: "VOICEVOX:四国めたん",
		ReceiptCue: true, PreventSleep: true, ServiceIdleTimeout: 300,
		Capture:      domain.DefaultCaptureSettings(),
		Conversation: Conversation{TTL: 300, MaxTurns: 6, MaxCharacters: 16000},
		Skills:       Skills{Lists: Lists{Shopping: "買い物", ToDo: "ToDo", Memo: "メモ"}},
	}
}

// legacy は Python 版だけにあった項目。移行方法を示す
var legacy = map[string]string{
	"whisper_cli": "whisper_server（whisper-serverのパス）に置き換えてください",
	"device":      "input_device（デバイス名の一部）に置き換えてください。一覧は wakeword devices で確認できます",
}

func Load(path string) (*Config, error) {
	c := defaults()
	meta, err := toml.DecodeFile(path, &c)
	if err != nil {
		return nil, fmt.Errorf("設定ファイルを読み込めません: %w", err)
	}
	var unknown []string
	for _, key := range meta.Undecoded() {
		name := key.String()
		if hint, ok := legacy[name]; ok {
			return nil, fmt.Errorf("設定項目 %s: %s", name, hint)
		}
		unknown = append(unknown, name)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("設定ファイルに未対応の項目があります: %s", strings.Join(unknown, ", "))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.base = filepath.Dir(absolute)
	if c.AIExecutable == "" {
		c.AIExecutable = c.Provider // PATH から codex／claude を探す
	}
	if c.Speaker == 0 && !meta.IsDefined("voicevox_credit") {
		c.VoicevoxCredit = "VOICEVOX:四国めたん"
	} else if !meta.IsDefined("voicevox_credit") {
		c.VoicevoxCredit = ""
	}
	return &c, c.validate()
}

func (c *Config) validate() error {
	if err := c.Capture.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.VoicevoxCredit) == "" {
		return errors.New("選択音声のvoicevox_creditを指定してください")
	}
	if !slices.Contains([]string{"codex", "claude"}, c.Provider) {
		return errors.New("providerはcodexかclaudeにしてください")
	}
	if !slices.Contains([]string{"read-only", "workspace-write"}, c.Sandbox) {
		return errors.New("sandboxはread-onlyかworkspace-writeにしてください")
	}
	if c.STTTimeout <= 0 || c.STTTimeout > 600 || c.AITimeout <= 0 || c.AITimeout > 1800 {
		return errors.New("タイムアウトの設定が不正です")
	}
	seen := map[string]bool{WakeName(c.WakeModel): true}
	for i := range c.WakeWords {
		w := &c.WakeWords[i]
		if w.Action == "" {
			w.Action = WakeActionTurn
		}
		switch w.Action {
		case WakeActionTurn:
		case WakeActionDiscord:
			if !discordID(c.Discord.ClientID) || !discordID(c.Discord.ChannelID) {
				return errors.New("action = \"discord\"には[discord]のclient_idとchannel_id（数字のID）が必要です")
			}
		default:
			return fmt.Errorf("wake_wordsのactionは%sか%sにしてください: %s", WakeActionTurn, WakeActionDiscord, w.Action)
		}
		if name := WakeName(w.Model); w.Model == "" || seen[name] {
			return fmt.Errorf("wake_wordsのmodelが空か重複しています: %s", w.Model)
		} else {
			seen[name] = true
		}
	}
	if c.ServiceIdleTimeout < 0 || c.ServiceIdleTimeout > 86400 {
		return errors.New("service_idle_timeoutは0〜86400秒にしてください")
	}
	if c.WhisperThreads < 1 || c.WhisperThreads > 32 {
		return errors.New("whisper_threadsは1〜32にしてください")
	}
	cv := c.Conversation
	if !(1 <= cv.TTL && cv.TTL <= 86400 && 1 <= cv.MaxTurns && cv.MaxTurns <= 20 &&
		1000 <= cv.MaxCharacters && cv.MaxCharacters <= 20000) {
		return errors.New("会話履歴の上限設定が不正です")
	}
	if l := c.Skills.Lists; l.Enabled {
		for _, name := range []string{l.Shopping, l.ToDo, l.Memo} {
			if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 || strings.ContainsAny(name, "\r\n") {
				return errors.New("skills.listsのリスト名は空でなく100文字以内にしてください")
			}
		}
	}
	return nil
}

// Path は設定ファイルの場所を基準に解決する。
func (c *Config) Path(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			value = filepath.Join(home, value[2:])
		}
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(c.base, value)
}

// Executable は / を含む場合だけパスとして解決し、それ以外は PATH から探す名前のままにする。
func (c *Config) Executable(value string) string {
	if strings.Contains(value, "/") {
		return c.Path(value)
	}
	return value
}

// discordID は Discord の ID（数字だけ）かを返す。
func discordID(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// WakeName はモデルのファイル名から、ログや動作の対応づけに使う名前を作る（例: hey_jarvis_v0.1）。
func WakeName(model string) string {
	return strings.TrimSuffix(filepath.Base(model), filepath.Ext(model))
}

// WakeModels は待ち受けるウェイクワードのモデルを、wake_model を先頭にして返す。
func (c *Config) WakeModels() []string {
	models := []string{c.Path(c.WakeModel)}
	for _, w := range c.WakeWords {
		models = append(models, c.Path(w.Model))
	}
	return models
}

// Base は設定ファイルのあるディレクトリ。
func (c *Config) Base() string { return c.base }

func Seconds(v float64) time.Duration { return time.Duration(v * float64(time.Second)) }
