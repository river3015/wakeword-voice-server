package onnx

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
)

// Python 版（openWakeWord 0.6.0）と同じ WAV を流し、フレームごとのスコアを比べる。
// モデル・録音・基準値は Git に含めないので、無い環境では飛ばす。
// 基準値は scripts/golden-scores.py で .runtime/golden/scores.json に作る。
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func setup(t *testing.T) (*Detector, map[string]struct{ Wake, VAD []float64 }) {
	t.Helper()
	base := root()
	lib := filepath.Join(base, ".vendor/onnxruntime/onnxruntime-osx-arm64-1.29.0/lib/libonnxruntime.1.29.0.dylib")
	models := filepath.Join(base, ".models/openwakeword")
	data, err := os.ReadFile(filepath.Join(base, ".runtime/golden/scores.json"))
	if err != nil {
		t.Skip("基準値がありません: ", err)
	}
	if _, err := os.Stat(lib); err != nil {
		t.Skip("ONNX Runtime がありません")
	}
	var golden map[string]struct{ Wake, VAD []float64 }
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if err := Init(lib); err != nil {
		t.Fatal(err)
	}
	d, err := NewDetector([]string{filepath.Join(models, "hey_mycroft_v0.1.onnx")}, filepath.Join(models, "silero_vad.onnx"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, golden
}

func TestScoresMatchPython(t *testing.T) {
	d, golden := setup(t)
	for name, want := range golden {
		samples, err := audio.ReadRequestWAV(filepath.Join(root(), ".recordings", name+".wav"))
		if err != nil {
			t.Skip(err)
		}
		if err := d.Reset(); err != nil {
			t.Fatal(err)
		}
		detected := false
		maxWake, maxVAD := 0.0, 0.0
		for i := 0; i*frameSamples < len(samples); i++ {
			frame := make([]int16, frameSamples)
			copy(frame, samples[i*frameSamples:])
			scores, err := d.Wake.Predict(frame)
			if err != nil {
				t.Fatal(err)
			}
			wake := scores[0]
			speech, err := d.VAD.Predict(frame)
			if err != nil {
				t.Fatal(err)
			}
			detected = detected || wake >= 0.5
			// 初期の埋め込みは乱数の雑音なので、雑音が判定窓から抜ける16フレーム以降を比べる
			if i >= classifierFrame {
				maxWake = max(maxWake, math.Abs(wake-want.Wake[i]))
			}
			maxVAD = max(maxVAD, math.Abs(speech-want.VAD[i]))
			if i >= classifierFrame && math.Abs(wake-want.Wake[i]) > 0.02 {
				t.Errorf("%s frame %d: wake = %.4f, python = %.4f", name, i, wake, want.Wake[i])
			}
			if math.Abs(speech-want.VAD[i]) > 0.001 {
				t.Errorf("%s frame %d: vad = %.4f, python = %.4f", name, i, speech, want.VAD[i])
			}
		}
		t.Logf("%s: frames = %d, max diff wake = %.2g, vad = %.2g", name, len(want.Wake), maxWake, maxVAD)
		pythonDetected := false
		for _, v := range want.Wake {
			pythonDetected = pythonDetected || v >= 0.5
		}
		if detected != pythonDetected {
			t.Errorf("%s: detected = %v, python = %v", name, detected, pythonDetected)
		}
	}
}
