// Package onnx は ONNX Runtime で openWakeWord と Silero VAD を動かす。
package onnx

import (
	"errors"
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	initOnce sync.Once
	initErr  error
)

// Init は ONNX Runtime の共有ライブラリを読み込む。プロセスで一度だけ実行される。
func Init(libraryPath string) error {
	initOnce.Do(func() {
		ort.SetSharedLibraryPath(libraryPath)
		if err := ort.InitializeEnvironment(); err != nil {
			initErr = fmt.Errorf("ONNX Runtimeを初期化できません: %w", err)
			return
		}
		initErr = ort.DisableTelemetry()
	})
	return initErr
}

// session は入出力名をモデルから読み取った推論セッション。
type session struct {
	s       *ort.DynamicAdvancedSession
	outputs int
}

func newSession(path string) (*session, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer options.Destroy()
	// openWakeWord と同じく1スレッドにする。80msごとの小さな推論なので並列化の利点が小さい
	if err := options.SetIntraOpNumThreads(1); err != nil {
		return nil, err
	}
	if err := options.SetInterOpNumThreads(1); err != nil {
		return nil, err
	}
	s, err := ort.NewDynamicAdvancedSession(path, names(inputs), names(outputs), options)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &session{s: s, outputs: len(outputs)}, nil
}

func names(info []ort.InputOutputInfo) []string {
	result := make([]string, len(info))
	for i, v := range info {
		result[i] = v.Name
	}
	return result
}

// output は推論結果の float32 テンソルをコピーしたもの。ONNX 側のメモリはすぐ解放する。
type output struct {
	data  []float32
	shape ort.Shape
}

func (s *session) run(inputs ...ort.Value) ([]output, error) {
	values := make([]ort.Value, s.outputs) // nil の出力は ONNX Runtime が確保する
	if err := s.s.Run(inputs, values); err != nil {
		return nil, err
	}
	result := make([]output, len(values))
	var err error
	for i, v := range values {
		if t, ok := v.(*ort.Tensor[float32]); ok {
			result[i] = output{data: append([]float32(nil), t.GetData()...), shape: t.GetShape()}
		} else {
			err = errors.New("float32以外の出力です")
		}
		v.Destroy()
	}
	return result, err
}

func (s *session) destroy() {
	if s != nil {
		s.s.Destroy()
	}
}

// tensor は入力テンソルを作り、呼び出し側で Destroy する。
func tensor(data []float32, shape ...int64) (*ort.Tensor[float32], error) {
	return ort.NewTensor(ort.NewShape(shape...), data)
}
