package onnx

import (
	"errors"
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	vadChunk = 640 // openWakeWord の VAD と同じく40msごとに推論し平均する
	vadState = 2 * 1 * 64
)

// VAD は Silero VAD v4（h、c の状態を持つ形式）で発話らしさを返す。
type VAD struct {
	model *session
	h, c  []float32
}

func NewVAD(modelPath string) (*VAD, error) {
	s, err := newSession(modelPath)
	if err != nil {
		return nil, err
	}
	v := &VAD{model: s}
	v.Reset()
	return v, nil
}

func (v *VAD) Close() { v.model.destroy() }

func (v *VAD) Reset() {
	v.h = make([]float32, vadState)
	v.c = make([]float32, vadState)
}

func (v *VAD) Predict(frame []int16) (float64, error) {
	if len(frame)%vadChunk != 0 {
		return 0, errors.New("VAD入力は640標本の倍数にしてください")
	}
	sum := 0.0
	for start := 0; start < len(frame); start += vadChunk {
		score, err := v.chunk(frame[start : start+vadChunk])
		if err != nil {
			return 0, err
		}
		sum += score
	}
	return sum / float64(len(frame)/vadChunk), nil
}

func (v *VAD) chunk(samples []int16) (float64, error) {
	data := make([]float32, len(samples))
	for i, s := range samples {
		data[i] = float32(s) / 32767
	}
	input, err := tensor(data, 1, int64(len(samples)))
	if err != nil {
		return 0, err
	}
	defer input.Destroy()
	sr, err := ort.NewScalar[int64](16000)
	if err != nil {
		return 0, err
	}
	defer sr.Destroy()
	h, err := tensor(v.h, 2, 1, 64)
	if err != nil {
		return 0, err
	}
	defer h.Destroy()
	c, err := tensor(v.c, 2, 1, 64)
	if err != nil {
		return 0, err
	}
	defer c.Destroy()
	// 入力順はモデルの定義順：input、sr、h、c
	out, err := v.model.run(input, sr, h, c)
	if err != nil {
		return 0, fmt.Errorf("vad: %w", err)
	}
	v.h, v.c = out[1].data, out[2].data
	return float64(out[0].data[0]), nil
}
