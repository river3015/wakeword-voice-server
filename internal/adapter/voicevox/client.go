// Package voicevox はローカルの VOICEVOX エンジンで一文ずつ音声合成する。
package voicevox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/river3015/wakeword-voice-server/internal/adapter/localhttp"
)

const maxResponse = 16 << 20

type Client struct {
	base    string
	speaker int
	http    *http.Client
}

func New(baseURL string, speaker int, timeout time.Duration) (*Client, error) {
	base, err := localhttp.ParseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if speaker < 0 {
		return nil, errors.New("VOICEVOXのspeakerが不正です")
	}
	return &Client{base: base, speaker: speaker, http: localhttp.NewClient(timeout)}, nil
}

func (c *Client) Synthesize(ctx context.Context, sentence string) ([]byte, error) {
	if strings.TrimSpace(sentence) == "" || utf8.RuneCountInString(sentence) > 1200 {
		return nil, errors.New("読み上げ文は1〜1200文字にしてください")
	}
	query := url.Values{"text": {sentence}, "speaker": {fmt.Sprint(c.speaker)}}
	audioQuery, err := c.post(ctx, "/audio_query?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	wav, err := c.post(ctx, fmt.Sprintf("/synthesis?speaker=%d", c.speaker), audioQuery)
	if err != nil {
		return nil, err
	}
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, errors.New("VOICEVOXからWAV音声を取得できません")
	}
	return wav, nil
}

func (c *Client) post(ctx context.Context, route string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+route, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("VOICEVOXへの接続に失敗しました: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponse {
		return nil, errors.New("VOICEVOXの返答が大きすぎます")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("VOICEVOXの音声合成に失敗しました: HTTP %d", response.StatusCode)
	}
	return data, nil
}

// Warmup は話者の音声モデルを読み込み、短い文を一度合成して、最初の依頼で合成が遅くならないようにする。
// 話者の読み込みだけでは、起動直後の最初の合成が1秒以上遅いままだった。
func (c *Client) Warmup(ctx context.Context) error {
	if _, err := c.post(ctx, fmt.Sprintf("/initialize_speaker?speaker=%d&skip_reinit=true", c.speaker), nil); err != nil {
		return err
	}
	_, err := c.Synthesize(ctx, "はい。")
	return err
}

// Ready はエンジンが応答するかを返す。
func (c *Client) Ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/version", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("VOICEVOXの準備ができていません")
	}
	return nil
}
