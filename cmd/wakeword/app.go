package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
	"github.com/river3015/wakeword-voice-server/internal/adapter/claude"
	"github.com/river3015/wakeword-voice-server/internal/adapter/codex"
	"github.com/river3015/wakeword-voice-server/internal/adapter/onnx"
	"github.com/river3015/wakeword-voice-server/internal/adapter/process"
	"github.com/river3015/wakeword-voice-server/internal/adapter/reminders"
	"github.com/river3015/wakeword-voice-server/internal/adapter/voicevox"
	"github.com/river3015/wakeword-voice-server/internal/adapter/whisper"
	"github.com/river3015/wakeword-voice-server/internal/config"
	"github.com/river3015/wakeword-voice-server/internal/domain"
	"github.com/river3015/wakeword-voice-server/internal/skill/lists"
	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// app は起動した部品をまとめ、終了時に逆順で片付ける。
type app struct {
	cfg          *config.Config
	devices      *audio.Devices
	detector     *onnx.Detector
	whisper      *whisper.Client
	voicevox     *voicevox.Client
	responder    responder
	conversation *domain.Conversation
	skills       []usecase.Skill
	children     []*process.Child
	release      func()
	cancel       context.CancelCauseFunc
}

// start は部品を準備する。whisper-server と VOICEVOX は時間がかかるので並行して起動する。
// 子プロセスが途中で落ちたら、返す ctx を取り消してサーバーを止める。
func start(parent context.Context, cfg *config.Config, microphone bool) (*app, context.Context, error) {
	a := &app{cfg: cfg}
	ok := false
	defer func() {
		if !ok {
			a.close()
		}
	}()
	release, err := process.Lock(filepath.Join(cfg.Base(), ".runtime/server.lock"))
	if err != nil {
		return nil, nil, err
	}
	a.release = release

	a.responder = newResponder(cfg)
	if err := a.responder.Validate(); err != nil {
		return nil, nil, err
	}
	if a.whisper, err = whisper.New(cfg.WhisperURL, config.Seconds(cfg.STTTimeout)); err != nil {
		return nil, nil, err
	}
	if a.voicevox, err = voicevox.New(cfg.VoicevoxURL, cfg.Speaker, time.Minute); err != nil {
		return nil, nil, err
	}
	if a.conversation, err = domain.NewConversation(config.Seconds(cfg.Conversation.TTL),
		cfg.Conversation.MaxTurns, cfg.Conversation.MaxCharacters, nil); err != nil {
		return nil, nil, err
	}
	if a.skills, err = newSkills(parent, cfg); err != nil {
		return nil, nil, err
	}
	a.whisper.Prompt = vocabularyPrompt(a.skills)
	if a.devices, err = audio.NewDevices(); err != nil {
		return nil, nil, err
	}
	if err := onnx.Init(cfg.Path(cfg.ONNXRuntime)); err != nil {
		return nil, nil, err
	}
	if a.detector, err = onnx.NewDetector(cfg.Path(cfg.WakeModel), cfg.Path(cfg.VADModel)); err != nil {
		return nil, nil, fmt.Errorf("検出モデルを読み込めません: %w", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	launch := func(name string, argv []string, ready func(context.Context) error) {
		wg.Go(func() {
			child, err := process.Start(parent, name, argv, cfg.Base(), 2*time.Minute, ready)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			a.children = append(a.children, child)
		})
	}
	if !alreadyReady(parent, a.whisper.Ready) {
		if cfg.WhisperServer == "" {
			return nil, nil, errors.New("whisper-serverに接続できません。whisper_serverを設定するか起動してください")
		}
		launch("whisper-server", a.whisperArgs(), a.whisper.Ready)
	}
	if !alreadyReady(parent, a.voicevox.Ready) {
		if cfg.VoicevoxEngine == "" {
			return nil, nil, errors.New("VOICEVOXに接続できません。voicevox_engineを設定するか起動してください")
		}
		launch("VOICEVOX", a.voicevoxArgs(), a.voicevox.Ready)
	}
	if cfg.PreventSleep && microphone {
		// このプロセスが終わると caffeinate も終わる
		launch("caffeinate", []string{"/usr/bin/caffeinate", "-di", "-w", fmt.Sprint(os.Getpid())}, nil)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}
	if err := a.voicevox.Warmup(parent); err != nil {
		return nil, nil, fmt.Errorf("VOICEVOXの話者を初期化できません: %w", err)
	}

	ctx, cancel := context.WithCancelCause(parent)
	a.cancel = cancel
	for _, child := range a.children {
		go func() {
			select {
			case <-child.Done():
				cancel(fmt.Errorf("%sが終了しました", child.Name))
			case <-ctx.Done():
			}
		}()
	}
	ok = true
	slog.Info("ready", "provider", cfg.Provider, "voice", cfg.VoicevoxCredit)
	return a, ctx, nil
}

// responder は設定で選んだ AI CLI。どちらも usecase.Responder を満たす。
type responder interface {
	usecase.Responder
	Validate() error
}

func newResponder(cfg *config.Config) responder {
	executable, workdir, timeout := cfg.Executable(cfg.AIExecutable), cfg.Path(cfg.Workdir), config.Seconds(cfg.AITimeout)
	if cfg.Provider == "claude" {
		return &claude.CLI{Executable: executable, Workdir: workdir, Sandbox: cfg.Sandbox, Timeout: timeout,
			Model: cfg.Model, Effort: cfg.ReasoningEffort}
	}
	return &codex.CLI{Executable: executable, Workdir: workdir, Sandbox: cfg.Sandbox, Timeout: timeout,
		Model: cfg.Model, ReasoningEffort: cfg.ReasoningEffort}
}

// newSkills は設定で有効にしたスキルを、AI より先に試す順で並べる。
// 新しいスキルはここに追加する。
func newSkills(ctx context.Context, cfg *config.Config) ([]usecase.Skill, error) {
	var skills []usecase.Skill
	if l := cfg.Skills.Lists; l.Enabled {
		store := &reminders.Reminders{}
		if err := store.Check(ctx, []string{l.Shopping, l.ToDo, l.Memo}); err != nil {
			return nil, err
		}
		skills = append(skills, &lists.Skill{Store: store,
			Names: map[lists.Kind]string{lists.Shopping: l.Shopping, lists.ToDo: l.ToDo, lists.Memo: l.Memo}})
	}
	return skills, nil
}

// vocabularyPrompt はスキルの語彙を文字起こしのヒントにまとめる。
func vocabularyPrompt(skills []usecase.Skill) string {
	var words []string
	for _, skill := range skills {
		if v, ok := skill.(usecase.SkillVocabulary); ok {
			words = append(words, v.Vocabulary()...)
		}
	}
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, "、") + "。"
}

func alreadyReady(ctx context.Context, ready func(context.Context) error) bool {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return ready(probe) == nil
}

func (a *app) whisperArgs() []string {
	u, _ := url.Parse(a.cfg.WhisperURL) // 形式は whisper.New で確認済み
	args := []string{a.cfg.Path(a.cfg.WhisperServer), "-m", a.cfg.Path(a.cfg.WhisperModel), "-l", "ja",
		"--host", u.Hostname(), "--port", u.Port(), "-nt", "-t", fmt.Sprint(a.cfg.WhisperThreads)}
	if !a.cfg.GPU {
		args = append(args, "-ng")
	}
	return args
}

func (a *app) voicevoxArgs() []string {
	u, _ := url.Parse(a.cfg.VoicevoxURL)
	return []string{a.cfg.Path(a.cfg.VoicevoxEngine), "--host", u.Hostname(), "--port", u.Port(),
		"--cpu_num_threads", "4", "--disable_mutable_api"}
}

func (a *app) close() {
	if a.cancel != nil {
		a.cancel(nil)
	}
	for i := len(a.children) - 1; i >= 0; i-- {
		a.children[i].Stop()
	}
	if a.detector != nil {
		a.detector.Close()
	}
	if a.devices != nil {
		a.devices.Close()
	}
	if a.release != nil {
		a.release()
	}
}

func (a *app) speaker() *audio.Speaker {
	return &audio.Speaker{Devices: a.devices, Name: a.cfg.OutputDevice}
}

func (a *app) captureListener(opener usecase.FrameOpener, maxWait time.Duration) *usecase.CaptureListener {
	l := &usecase.CaptureListener{Opener: opener, Detector: a.detector, Settings: a.cfg.Capture,
		MaxWait: maxWait, OnState: logState}
	if a.cfg.ReceiptCue {
		cue, speaker := audio.Cue(), a.speaker()
		l.OnRecording = func() {
			// 録音を止めないよう別の goroutine で鳴らす。反響は settle_seconds の間捨てる
			go func() {
				if err := speaker.Play(context.Background(), cue); err != nil {
					slog.Warn("受付音を再生できません", "error", err.Error())
				}
			}()
		}
	}
	return l
}

func (a *app) microphoneListener(maxWait time.Duration) *usecase.CaptureListener {
	return a.captureListener(&audio.Microphone{Devices: a.devices, Name: a.cfg.InputDevice}, maxWait)
}

func (a *app) runner(listener usecase.Listener) *usecase.TurnRunner {
	return &usecase.TurnRunner{Listener: listener, Transcriber: a.whisper, Responder: a.responder,
		Synthesizer: a.voicevox, Player: a.speaker(), Conversation: a.conversation, Skills: a.skills, OnState: logState}
}

func logState(s usecase.State) { slog.Info("state", "state", string(s)) }
