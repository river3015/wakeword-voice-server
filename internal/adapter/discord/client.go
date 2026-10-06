package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// キーチェーンに保存する項目の名前。値は利用者が保存する（Client Secret）か、Authorize で保存する（トークン）。
const (
	SecretService = "wakeword-discord-client-secret"
	TokenService  = "wakeword-discord-rpc-token"
	// redirectURI は Developer Portal の OAuth2 に登録したもの。ブラウザで開くことはない
	redirectURI = "http://127.0.0.1"
	tokenURL    = "https://discord.com/api/v10/oauth2/token"
)

// Secrets は秘密値の保存先。macOS ではキーチェーン。
type Secrets interface {
	Get(service string) (string, error)
	Set(service, value string) error
}

// Client は本人のアカウントを、設定したボイスチャンネルに入れる。
type Client struct {
	ClientID  string // Developer Portal のアプリの ID
	ChannelID string // 入るボイスチャンネルの ID
	Secrets   Secrets
	HTTP      *http.Client // nil なら10秒で打ち切る既定のクライアント
	SocketDir string       // IPC のソケットのあるディレクトリ。テスト用。空なら利用者の一時ディレクトリ
	TokenURL  string       // テスト用。空なら Discord の OAuth2 のトークンの URL
	Now       func() time.Time

	mu      sync.Mutex
	session *session
}

// token はキーチェーンに保存するトークン。
type token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Authorize は最初に一度だけ行う。Discord のアプリに確認画面が出るので、利用者が承認する。
func (c *Client) Authorize(ctx context.Context) error {
	ipc, err := c.connect(ctx)
	if err != nil {
		return err
	}
	defer ipc.close()
	defer ipc.deadline(ctx)()
	data, err := ipc.command("AUTHORIZE", map[string]any{"client_id": c.ClientID, "scopes": []string{"rpc"}}, "", nil)
	if err != nil {
		return err
	}
	var granted struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(data, &granted); err != nil || granted.Code == "" {
		return errors.New("Discordから認可コードを受け取れません")
	}
	_, err = c.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {granted.Code},
		"redirect_uri": {redirectURI}})
	return err
}

// Connect は Discord との接続を、認証とイベントの購読まで済ませて張っておく。
// Discord のアプリは接続の確立（handshake）に数秒〜数十秒かかることがあるので、起動時に呼んでおき、
// 呼びかけのときは通話に入る命令だけを送る。接続済みなら何もしない。
func (c *Client) Connect(ctx context.Context) error {
	_, err := c.ensure(ctx)
	return err
}

// Maintain は ctx が取り消されるまで接続を張り続ける。切れたら（Discord の再起動など）張り直し、
// 張れなければ retry だけ待ってやり直す。
func (c *Client) Maintain(ctx context.Context, retry time.Duration, onError func(error)) {
	for ctx.Err() == nil {
		s, err := c.ensure(ctx)
		if err != nil {
			if ctx.Err() == nil && onError != nil {
				onError(err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(retry):
			}
			continue
		}
		select {
		case <-ctx.Done():
		case <-s.done:
		}
	}
}

// Close は張っている接続を閉じる。通話はそのまま残る。
func (c *Client) Close() {
	c.mu.Lock()
	s := c.session
	c.session = nil
	c.mu.Unlock()
	if s != nil {
		s.close()
	}
}

// Call は入った通話。Wait で抜けるまで待つ。
type Call struct {
	session *session
	left    <-chan struct{}
}

// Join は本人をボイスチャンネルに入れ、抜けたことを知らせる Call を返す。
func (c *Client) Join(ctx context.Context) (*Call, error) {
	s, err := c.ensure(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.command(ctx, "SELECT_VOICE_CHANNEL", map[string]any{"channel_id": c.ChannelID, "timeout": 10}, "")
	if err != nil {
		return nil, err
	}
	// 入った後に届く「抜けた」だけを待つ
	return &Call{session: s, left: r.left}, nil
}

// Wait は本人がボイスチャンネルから抜ける（Discord を終了した場合を含む）まで待つ。
// ctx が取り消されたら、通話はそのままにして戻る。
func (call *Call) Wait(ctx context.Context) error {
	select {
	case <-call.left:
		return nil
	case <-call.session.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ensure は生きている接続を返し、なければ張り直す。
func (c *Client) ensure(ctx context.Context) (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil && c.session.alive() {
		return c.session, nil
	}
	access, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	ipc, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	s := newSession(ipc)
	if _, err := s.command(ctx, "AUTHENTICATE", map[string]any{"access_token": access}, ""); err != nil {
		s.close()
		return nil, err
	}
	if _, err := s.command(ctx, "SUBSCRIBE", map[string]any{}, "VOICE_CHANNEL_SELECT"); err != nil {
		s.close()
		return nil, err
	}
	c.session = s
	return s, nil
}

func (c *Client) connect(ctx context.Context) (*conn, error) {
	ipc, err := dial(ctx, c.SocketDir)
	if err != nil {
		return nil, err
	}
	defer ipc.deadline(ctx)()
	if err := ipc.handshake(c.ClientID); err != nil {
		ipc.close()
		return nil, err
	}
	return ipc, nil
}

// accessToken は保存したトークンが1時間以上有効ならそれを使い、そうでなければ更新する。
func (c *Client) accessToken(ctx context.Context) (string, error) {
	saved, err := c.Secrets.Get(TokenService)
	if err != nil {
		return "", errors.New("Discordの認可がありません。wakeword discord-auth を実行してください")
	}
	var t token
	if err := json.Unmarshal([]byte(saved), &t); err != nil || t.RefreshToken == "" {
		return "", errors.New("保存したDiscordのトークンを読めません。wakeword discord-auth を実行し直してください")
	}
	if t.AccessToken != "" && c.now().Add(time.Hour).Before(t.ExpiresAt) {
		return t.AccessToken, nil
	}
	refreshed, err := c.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}})
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// exchange はトークンを取得し、キーチェーンに保存する。更新トークンは使うたびに変わる。
func (c *Client) exchange(ctx context.Context, form url.Values) (token, error) {
	secret, err := c.Secrets.Get(SecretService)
	if err != nil {
		return token{}, fmt.Errorf("DiscordのClient Secretがキーチェーン（%s）にありません", SecretService)
	}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", secret)
	endpoint := c.TokenURL
	if endpoint == "" {
		endpoint = tokenURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return token{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "wakeword-voice-server (local)")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return token{}, fmt.Errorf("Discordのトークンを取得できません: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return token{}, err
	}
	if response.StatusCode != http.StatusOK {
		// 本文にはトークンが含まれ得るので、エラーには載せない
		return token{}, fmt.Errorf("Discordのトークンを取得できません: HTTP %d", response.StatusCode)
	}
	var granted struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &granted); err != nil || granted.AccessToken == "" || granted.RefreshToken == "" {
		return token{}, errors.New("Discordのトークンの形式が不正です")
	}
	t := token{AccessToken: granted.AccessToken, RefreshToken: granted.RefreshToken,
		ExpiresAt: c.now().Add(time.Duration(granted.ExpiresIn) * time.Second)}
	saved, _ := json.Marshal(t)
	if err := c.Secrets.Set(TokenService, string(saved)); err != nil {
		return token{}, fmt.Errorf("Discordのトークンを保存できません: %w", err)
	}
	return t, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
