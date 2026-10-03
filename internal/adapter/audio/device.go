package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

const (
	frameSamples = 1280
	queueFrames  = 25 // 2秒分。処理が遅れて溜まった古い音を新しい発話と取り違えないよう上限を設ける
)

// Devices は CoreAudio の入出力をまとめて扱う。プロセスで1つ作る。
type Devices struct {
	ctx *malgo.AllocatedContext
}

func NewDevices() (*Devices, error) {
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendCoreaudio}, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("音声デバイスを初期化できません: %w", err)
	}
	return &Devices{ctx: ctx}, nil
}

func (d *Devices) Close() {
	_ = d.ctx.Uninit()
	d.ctx.Free()
}

// List はデバイス名の一覧を返す。設定ファイルの input_device／output_device に名前の一部を書く。
func (d *Devices) List(capture bool) ([]string, error) {
	infos, err := d.ctx.Devices(kind(capture))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(infos))
	for i := range infos {
		names[i] = infos[i].Name()
		if infos[i].IsDefault != 0 {
			names[i] += " (既定)"
		}
	}
	return names, nil
}

func kind(capture bool) malgo.DeviceType {
	if capture {
		return malgo.Capture
	}
	return malgo.Playback
}

// find は名前の一部が一致するデバイスを探す。空なら OS の既定を使う。
// 返す DeviceID はデバイスの初期化が終わるまで生きている必要がある。
func (d *Devices) find(capture bool, name string) (*malgo.DeviceID, error) {
	if name == "" {
		return nil, nil
	}
	infos, err := d.ctx.Devices(kind(capture))
	if err != nil {
		return nil, err
	}
	for i := range infos {
		if strings.Contains(infos[i].Name(), name) {
			id := infos[i].ID
			return &id, nil
		}
	}
	return nil, fmt.Errorf("音声デバイス %q が見つかりません", name)
}

func pointer(id *malgo.DeviceID) unsafe.Pointer {
	if id == nil {
		return nil
	}
	return id.Pointer()
}

// Microphone は待ち受けのたびにマイクを開く。usecase.FrameOpener を満たす。
type Microphone struct {
	Devices *Devices
	Name    string
}

type stampedFrame struct {
	received time.Time
	samples  []int16
}

type micSource struct {
	device *malgo.Device
	frames chan stampedFrame
	broken atomic.Bool
	buffer []int16 // コールバックの goroutine だけが触る
}

func (m *Microphone) Open(ctx context.Context) (usecase.FrameSource, error) {
	id, err := m.Devices.find(true, m.Name)
	if err != nil {
		return nil, err
	}
	config := malgo.DefaultDeviceConfig(malgo.Capture)
	config.Capture.Format = malgo.FormatS16
	config.Capture.Channels = 1
	config.Capture.DeviceID = pointer(id)
	config.SampleRate = 16000
	config.PeriodSizeInFrames = frameSamples
	source := &micSource{frames: make(chan stampedFrame, queueFrames)}
	device, err := malgo.InitDevice(m.Devices.ctx.Context, config, malgo.DeviceCallbacks{Data: source.receive})
	if err != nil {
		return nil, fmt.Errorf("マイクを開けません。デバイスとmacOSのマイク許可を確認してください: %w", err)
	}
	source.device = device
	if err := device.Start(); err != nil {
		device.Uninit()
		return nil, fmt.Errorf("マイクを開始できません: %w", err)
	}
	return source, nil
}

// receive は CoreAudio のスレッドから呼ばれる。ここでは待たず、溢れたら欠落として印を付ける。
func (s *micSource) receive(_, input []byte, count uint32) {
	for i := 0; i+1 < len(input); i += 2 {
		s.buffer = append(s.buffer, int16(binary.LittleEndian.Uint16(input[i:])))
	}
	for len(s.buffer) >= frameSamples {
		frame := append([]int16(nil), s.buffer[:frameSamples]...)
		s.buffer = s.buffer[frameSamples:]
		select {
		case s.frames <- stampedFrame{time.Now(), frame}:
		default:
			s.broken.Store(true)
		}
	}
}

func (s *micSource) Next(ctx context.Context) ([]int16, error) {
	if s.broken.Load() {
		return nil, errors.New("マイク入力の欠落を検出しました")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(3 * time.Second):
		return nil, errors.New("マイク入力が途切れました")
	case f := <-s.frames:
		if time.Since(f.received) > time.Second {
			return nil, errors.New("マイク入力が遅延しました")
		}
		return f.samples, nil
	}
}

func (s *micSource) Close() error {
	s.device.Uninit()
	return nil
}

// Speaker は WAV をメモリから直接再生する。外部コマンドや一時ファイルを使わない。
type Speaker struct {
	Devices *Devices
	Name    string
}

func (s *Speaker) Play(ctx context.Context, wav []byte) error {
	pcm, err := DecodeWAV(wav)
	if err != nil {
		return err
	}
	id, err := s.Devices.find(false, s.Name)
	if err != nil {
		return err
	}
	config := malgo.DefaultDeviceConfig(malgo.Playback)
	config.Playback.Format = malgo.FormatS16
	config.Playback.Channels = 1
	config.Playback.DeviceID = pointer(id)
	config.SampleRate = uint32(pcm.SampleRate)

	position, tail := 0, 0
	done := make(chan struct{})
	var once sync.Once
	data := func(output, _ []byte, count uint32) {
		for i := 0; i+1 < len(output); i += 2 {
			var v int16
			if position < len(pcm.Samples) {
				v = pcm.Samples[position]
				position++
			}
			binary.LittleEndian.PutUint16(output[i:], uint16(v))
		}
		if position >= len(pcm.Samples) {
			// 最後の音がデバイスのバッファから出きるよう、無音を2回送ってから終える
			if tail++; tail > 2 {
				once.Do(func() { close(done) })
			}
		}
	}
	device, err := malgo.InitDevice(s.Devices.ctx.Context, config, malgo.DeviceCallbacks{Data: data})
	if err != nil {
		return fmt.Errorf("スピーカーを開けません: %w", err)
	}
	defer device.Uninit()
	if err := device.Start(); err != nil {
		return fmt.Errorf("再生を開始できません: %w", err)
	}
	limit := time.Duration(len(pcm.Samples))*time.Second/time.Duration(pcm.SampleRate) + 5*time.Second
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(limit):
		return errors.New("再生が終わりません")
	}
}

// Cue は受付音（880Hz・0.1秒）の WAV。
func Cue() []byte {
	samples := make([]int16, 1600)
	for i := range samples {
		samples[i] = int16(3000 * math.Sin(2*math.Pi*880*float64(i)/16000))
	}
	return EncodeWAV(PCM{SampleRate: 16000, Samples: samples})
}
