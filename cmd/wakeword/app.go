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
	"github.com/river3015/wakeword-voice-server/internal/adapter/musicapp"
	"github.com/river3015/wakeword-voice-server/internal/adapter/onnx"
	"github.com/river3015/wakeword-voice-server/internal/adapter/openmeteo"
	"github.com/river3015/wakeword-voice-server/internal/adapter/process"
	"github.com/river3015/wakeword-voice-server/internal/adapter/reminders"
	"github.com/river3015/wakeword-voice-server/internal/adapter/voicevox"
	"github.com/river3015/wakeword-voice-server/internal/adapter/whisper"
	"github.com/river3015/wakeword-voice-server/internal/config"
	"github.com/river3015/wakeword-voice-server/internal/domain"
	"github.com/river3015/wakeword-voice-server/internal/skill/lists"
	"github.com/river3015/wakeword-voice-server/internal/skill/music"
	"github.com/river3015/wakeword-voice-server/internal/skill/weather"
	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// app は起動した部品をまとめ、終了時に逆順で片付ける。
type app struct {
	cfg          *config.Config
	devices      *audio.Devices
	detector     *onnx.Detector
	transcriber  usecase.Transcriber
	synthesizer  usecase.Synthesizer
	responder    responder
	conversation *domain.Conversation
	skills       []usecase.Skill
	services     []*process.Service // 使うときだけ起動する whisper-server や VOICEVOX
	children     []*process.Child   // 起動中ずっと動かす caffeinate
	release      func()
	cancel       context.CancelCauseFunc
}

// start は部品を準備する。whisper-server と VOICEVOX は、service_idle_timeout が 0 なら
// ここで並行して起動し、それ以外は呼びかけを検出したときに起動する。
// caffeinate が途中で落ちたら、返す ctx を取り消してサーバーを止める。
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
	if a.conversation, err = domain.NewConversation(config.Seconds(cfg.Conversation.TTL),
		cfg.Conversation.MaxTurns, cfg.Conversation.MaxCharacters, nil); err != nil {
		return nil, nil, err
	}
	if a.skills, err = newSkills(parent, cfg); err != nil {
		return nil, nil, err
	}
	if err := a.newTranscriber(); err != nil {
		return nil, nil, err
	}
	if err := a.newSynthesizer(); err != nil {
		return nil, nil, err
	}
	if a.devices, err = audio.NewDevices(); err != nil {
		return nil, nil, err
	}
	if err := onnx.Init(cfg.Path(cfg.ONNXRuntime)); err != nil {
		return nil, nil, err
	}
	if a.detector, err = onnx.NewDetector(cfg.Path(cfg.WakeModel), cfg.Path(cfg.VADModel)); err != nil {
		return nil, nil, fmt.Errorf("検出モデルを読み込めません: %w", err)
	}
	if cfg.PreventSleep && microphone {
		// このプロセスが終わると caffeinate も終わる
		child, err := process.Start(parent, "caffeinate",
			[]string{"/usr/bin/caffeinate", "-di", "-w", fmt.Sprint(os.Getpid())}, cfg.Base(), 0, nil)
		if err != nil {
			return nil, nil, err
		}
		a.children = append(a.children, child)
	}
	if cfg.ServiceIdleTimeout == 0 {
		if err := a.startServices(parent); err != nil {
			return nil, nil, err
		}
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
	slog.Info("ready", "provider", cfg.Provider, "voice", cfg.VoicevoxCredit,
		"service_idle_timeout", cfg.ServiceIdleTimeout)
	return a, ctx, nil
}

// newTranscriber は whisper-server を使う文字起こしを組み立てる。
func (a *app) newTranscriber() error {
	cfg := a.cfg
	client, err := whisper.New(cfg.WhisperURL, config.Seconds(cfg.STTTimeout))
	if err != nil {
		return err
	}
	client.Prompt = vocabularyPrompt(a.skills)
	var argv []string
	if cfg.WhisperServer != "" {
		if err := requireFiles(cfg.Path(cfg.WhisperServer), cfg.Path(cfg.WhisperModel)); err != nil {
			return err
		}
		argv = a.whisperArgs()
	}
	service := a.newService("whisper-server", argv, client.Ready, client.Warmup)
	a.transcriber = usecase.OnDemandTranscriber{Transcriber: client, Service: service}
	return nil
}

// newSynthesizer は VOICEVOX を使う読み上げを組み立てる。
// 常駐プロセスが要らない読み上げ（macOS の say など）に替える場合は、Service を使わずにここで差し替える。
func (a *app) newSynthesizer() error {
	cfg := a.cfg
	client, err := voicevox.New(cfg.VoicevoxURL, cfg.Speaker, time.Minute)
	if err != nil {
		return err
	}
	var argv []string
	if cfg.VoicevoxEngine != "" {
		if err := requireFiles(cfg.Path(cfg.VoicevoxEngine)); err != nil {
			return err
		}
		argv = a.voicevoxArgs()
	}
	service := a.newService("VOICEVOX", argv, client.Ready, client.Warmup)
	a.synthesizer = usecase.OnDemandSynthesizer{Synthesizer: client, Service: service}
	return nil
}

// newService は argv が空なら起動済みのものだけを使う Service を作る。
func (a *app) newService(name string, argv []string, ready, init func(context.Context) error) *process.Service {
	service := &process.Service{Name: name, Argv: argv, Dir: a.cfg.Base(), StartTimeout: 2 * time.Minute,
		IdleTimeout: config.Seconds(a.cfg.ServiceIdleTimeout), Ready: ready, Init: init}
	a.services = append(a.services, service)
	return service
}

// startServices は全ての Service を並行して起動し、準備ができるまで待つ。
func (a *app) startServices(ctx context.Context) error {
	errs := make([]error, len(a.services))
	var wg sync.WaitGroup
	for i, service := range a.services {
		wg.Go(func() {
			if errs[i] = service.Acquire(ctx); errs[i] == nil {
				service.Release()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// prepare は呼びかけを検出した時点で、文字起こしと読み上げの準備を始める。
func (a *app) prepare() { usecase.Prepare(a.transcriber, a.synthesizer) }

func requireFiles(paths ...string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("ファイルがありません: %w", err)
		}
	}
	return nil
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
	if w := cfg.Skills.Weather; w.Enabled {
		forecaster := &openmeteo.Client{Latitude: w.Latitude, Longitude: w.Longitude}
		if err := forecaster.Validate(); err != nil {
			return nil, err
		}
		skills = append(skills, &weather.Skill{Forecaster: forecaster})
	}
	if cfg.Skills.Music.Enabled {
		player := &musicapp.Music{}
		if err := player.Check(ctx); err != nil {
			return nil, err
		}
		skills = append(skills, &music.Skill{Player: player})
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
	for _, service := range a.services {
		service.Close()
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
		MaxWait: maxWait, OnState: logState, OnRecording: a.prepare}
	if a.cfg.ReceiptCue {
		cue, speaker := audio.Cue(), a.speaker()
		l.OnRecording = func() {
			a.prepare()
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
	return &usecase.TurnRunner{Listener: listener, Transcriber: a.transcriber, Responder: a.responder,
		Synthesizer: a.synthesizer, Player: a.speaker(), Conversation: a.conversation, Skills: a.skills, OnState: logState}
}

func logState(s usecase.State) { slog.Info("state", "state", string(s)) }
