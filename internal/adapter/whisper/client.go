// Package whisper は常駐させた whisper.cpp の whisper-server で文字起こしする。
// 依頼ごとにモデルを読み込み直さないので、whisper-cli を毎回起動するより速い。
package whisper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
	"github.com/river3015/wakeword-voice-server/internal/adapter/localhttp"
)

type Client struct {
	base string
	http *http.Client
	// Prompt は認識させたい語彙のヒント。空なら送らない。スキルの言い回しの誤認識を減らすために使う
	Prompt string
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	base, err := localhttp.ParseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{base: base, http: localhttp.NewClient(timeout)}, nil
}

// Transcribe は音声をメモリ上の WAV にして送る。一時ファイルは作らない。
func (c *Client) Transcribe(ctx context.Context, pcm []int16) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "request.wav")
	if err != nil {
		return "", err
	}
	file.Write(audio.EncodeWAV(audio.PCM{SampleRate: 16000, Samples: pcm}))
	for key, value := range map[string]string{
		"language": "ja", "response_format": "text", "temperature": "0", "no_timestamps": "true",
	} {
		form.WriteField(key, value)
	}
	if c.Prompt != "" {
		form.WriteField("prompt", c.Prompt)
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/inference", &body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("whisper-serverに接続できません: %w", err)
	}
	defer response.Body.Close()
	text, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		// 本文には発話内容が含まれ得るので、エラーには載せない
		return "", fmt.Errorf("whisper-serverが失敗しました: HTTP %d", response.StatusCode)
	}
	return strings.TrimSpace(string(text)), nil
}

// Ready はモデルの読み込みが終わっているかを返す。
func (c *Client) Ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("whisper-serverの準備ができていません")
	}
	return nil
}

// Warmup は無音を一度文字起こしし、起動直後の最初の依頼で文字起こしが遅くならないようにする。
func (c *Client) Warmup(ctx context.Context) error {
	_, err := c.Transcribe(ctx, make([]int16, 16000/2))
	return err
}
