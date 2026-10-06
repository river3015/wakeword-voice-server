package onnx

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
)

// openWakeWord 0.6.0 の AudioFeatures／Model.predict と同じ手順・バッファ長で推論する。
const (
	melBins         = 32
	melWindow       = 76  // 埋め込みモデルに渡すメルフレーム数
	melMaxRows      = 970 // 約10秒
	embeddingSize   = 96
	featureMaxRows  = 120  // 約10秒
	classifierFrame = 16   // 判定モデルに渡す埋め込み数
	rawContext      = 480  // メルスペクトログラムの窓をつなぐために前フレームから足す標本数
	warmupFrames    = 5    // 初期化直後のスコアは0にする
	frameSamples    = 1280 // 80ms
	noiseSamples    = 16000 * 4
)

// WakeWord は複数のウェイクワードを判定する。重いメルスペクトログラムと埋め込みは共通で1回だけ計算し、
// ウェイクワードごとの判定モデル（小さい）だけを並べる。
type WakeWord struct {
	melspec, embedding *session
	classifiers        []*session
	raw                []int16     // 直近 frameSamples+rawContext 標本
	mel                [][]float32 // melBins 列の行
	features           [][]float32 // embeddingSize 列の行
	predictions        int
}

// NewWakeWord は判定モデルと、最初の判定モデルと同じディレクトリにある共有モデルを読み込む。
func NewWakeWord(modelPaths ...string) (*WakeWord, error) {
	if len(modelPaths) == 0 {
		return nil, errors.New("ウェイクワードの判定モデルを指定してください")
	}
	dir := filepath.Dir(modelPaths[0])
	w := &WakeWord{}
	var err error
	if w.melspec, err = newSession(filepath.Join(dir, "melspectrogram.onnx")); err != nil {
		return nil, err
	}
	if w.embedding, err = newSession(filepath.Join(dir, "embedding_model.onnx")); err != nil {
		w.Close()
		return nil, err
	}
	for _, path := range modelPaths {
		classifier, err := newSession(path)
		if err != nil {
			w.Close()
			return nil, err
		}
		w.classifiers = append(w.classifiers, classifier)
	}
	if err := w.Reset(); err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

func (w *WakeWord) Close() {
	w.melspec.destroy()
	w.embedding.destroy()
	for _, c := range w.classifiers {
		c.destroy()
	}
}

// Reset は上流と同じく、メルバッファを1で、埋め込みバッファを雑音の埋め込みで初期化する。
func (w *WakeWord) Reset() error {
	w.raw = w.raw[:0]
	w.mel = make([][]float32, melWindow)
	for i := range w.mel {
		w.mel[i] = ones(melBins)
	}
	w.predictions = 0
	noise := make([]int16, noiseSamples)
	for i := range noise {
		noise[i] = int16(rand.IntN(2000) - 1000)
	}
	features, err := w.embeddings(noise)
	if err != nil {
		return err
	}
	w.features = features
	return nil
}

// Predict は80msの1フレームを受け取り、判定モデルごとに0〜1のスコアを返す。
func (w *WakeWord) Predict(frame []int16) ([]float64, error) {
	if len(frame) != frameSamples {
		return nil, errors.New("ウェイクワード入力は1280標本にしてください")
	}
	w.raw = append(w.raw, frame...)
	if over := len(w.raw) - (frameSamples + rawContext); over > 0 {
		w.raw = w.raw[over:]
	}
	rows, err := w.melspectrogram(w.raw)
	if err != nil {
		return nil, err
	}
	w.mel = keepLast(append(w.mel, rows...), melMaxRows)

	window := w.mel[len(w.mel)-melWindow:]
	embedding, err := w.embed(flatten(window), 1)
	if err != nil {
		return nil, err
	}
	w.features = keepLast(append(w.features, embedding...), featureMaxRows)

	input, err := tensor(flatten(w.features[len(w.features)-classifierFrame:]), 1, classifierFrame, embeddingSize)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	scores := make([]float64, len(w.classifiers))
	for i, classifier := range w.classifiers {
		out, err := classifier.run(input)
		if err != nil {
			return nil, fmt.Errorf("wake classifier: %w", err)
		}
		scores[i] = float64(out[0].data[0])
	}
	if w.predictions < warmupFrames {
		w.predictions++
		clear(scores)
	}
	return scores, nil
}

func (w *WakeWord) melspectrogram(samples []int16) ([][]float32, error) {
	data := make([]float32, len(samples))
	for i, s := range samples {
		data[i] = float32(s) // 上流は正規化せず int16 の値をそのまま float にする
	}
	input, err := tensor(data, 1, int64(len(samples)))
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	out, err := w.melspec.run(input)
	if err != nil {
		return nil, fmt.Errorf("melspectrogram: %w", err)
	}
	values := out[0].data
	rows := make([][]float32, len(values)/melBins)
	for i := range rows {
		row := values[i*melBins : (i+1)*melBins]
		for j := range row {
			row[j] = row[j]/10 + 2 // 上流の melspec_transform
		}
		rows[i] = row
	}
	return rows, nil
}

func (w *WakeWord) embeddings(samples []int16) ([][]float32, error) {
	mel, err := w.melspectrogram(samples)
	if err != nil {
		return nil, err
	}
	var batch []float32
	count := 0
	for i := 0; i+melWindow <= len(mel); i += 8 {
		batch = append(batch, flatten(mel[i:i+melWindow])...)
		count++
	}
	return w.embed(batch, count)
}

func (w *WakeWord) embed(batch []float32, count int) ([][]float32, error) {
	input, err := tensor(batch, int64(count), melWindow, melBins, 1)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	out, err := w.embedding.run(input)
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	values := out[0].data
	rows := make([][]float32, count)
	for i := range rows {
		rows[i] = values[i*embeddingSize : (i+1)*embeddingSize]
	}
	return rows, nil
}

func ones(n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = 1
	}
	return v
}

func flatten(rows [][]float32) []float32 {
	var out []float32
	for _, r := range rows {
		out = append(out, r...)
	}
	return out
}

func keepLast[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
