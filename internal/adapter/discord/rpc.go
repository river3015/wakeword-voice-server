// Package discord は Discord デスクトップアプリのローカル RPC（Unix ソケットの IPC）で、
// 本人のアカウントをボイスチャンネルに入れ、抜けるまで待つ。
// Bot やユーザーのトークンで本人のアカウントを操作する方法（self-bot）は規約違反なので使わない。
package discord

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IPC のフレームの種類。
const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

// conn は IPC の1接続。同時に送受信しないよう、使う側で順番を守る。
type conn struct {
	c       net.Conn
	writeMu sync.Mutex
}

// dial は Discord が開いている discord-ipc-0〜9 のうち、最初につながったものを使う。
func dial(ctx context.Context, dir string) (*conn, error) {
	if dir == "" {
		dir = tempDir()
	}
	var dialer net.Dialer
	for i := range 10 {
		c, err := dialer.DialContext(ctx, "unix", filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i)))
		if err == nil {
			return &conn{c: c}, nil
		}
	}
	return nil, errors.New("Discordのアプリに接続できません。Discordが起動しているか確認してください")
}

// tempDir は Discord が IPC のソケットを置くユーザーの一時ディレクトリ。
// launchd の下では TMPDIR がない場合があるので、getconf で求める。
func tempDir() string {
	if dir := os.Getenv("TMPDIR"); dir != "" {
		return dir
	}
	out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
	if err != nil {
		return os.TempDir()
	}
	return strings.TrimSpace(string(out))
}

func (c *conn) send(op uint32, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:], op)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(body)))
	copy(frame[8:], body)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.c.Write(frame)
	return err
}

// message は受け取ったフレーム。
type message struct {
	Cmd   string          `json:"cmd"`
	Evt   string          `json:"evt"`
	Nonce string          `json:"nonce"`
	Data  json.RawMessage `json:"data"`
}

// read は1フレームを読む。ping には pong を返し、close は終了として扱う。
func (c *conn) read() (message, error) {
	for {
		var header [8]byte
		if _, err := io.ReadFull(c.c, header[:]); err != nil {
			return message{}, err
		}
		op, size := binary.LittleEndian.Uint32(header[0:]), binary.LittleEndian.Uint32(header[4:])
		if size > 1<<20 {
			return message{}, errors.New("DiscordのRPCの応答が大きすぎます")
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(c.c, body); err != nil {
			return message{}, err
		}
		switch op {
		case opPing:
			if err := c.send(opPong, json.RawMessage(body)); err != nil {
				return message{}, err
			}
			continue
		case opClose:
			return message{}, fmt.Errorf("DiscordがRPCの接続を閉じました: %s", rpcError(body))
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			return message{}, err
		}
		return m, nil
	}
}

// handshake は接続直後に一度だけ行う。
func (c *conn) handshake(clientID string) error {
	if err := c.send(opHandshake, map[string]any{"v": 1, "client_id": clientID}); err != nil {
		return err
	}
	m, err := c.read()
	if err != nil {
		return err
	}
	if m.Evt != "READY" {
		return fmt.Errorf("DiscordのRPCに接続できません: %s", rpcError(m.Data))
	}
	return nil
}

// command は命令を送り、同じ nonce の応答を返す。途中に届いたイベントは onEvent に渡す。
func (c *conn) command(cmd string, args any, evt string, onEvent func(message)) (json.RawMessage, error) {
	nonce := newNonce()
	payload := map[string]any{"cmd": cmd, "args": args, "nonce": nonce}
	if evt != "" {
		payload["evt"] = evt
	}
	if err := c.send(opFrame, payload); err != nil {
		return nil, err
	}
	for {
		m, err := c.read()
		if err != nil {
			return nil, err
		}
		if m.Nonce != nonce {
			if onEvent != nil {
				onEvent(m)
			}
			continue
		}
		if m.Evt == "ERROR" {
			return nil, fmt.Errorf("DiscordのRPCの%sに失敗しました: %s", cmd, rpcError(m.Data))
		}
		return m.Data, nil
	}
}

func (c *conn) close() { c.c.Close() }

// rpcError はエラーの番号と説明だけを取り出す。
func rpcError(data []byte) string {
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) != nil || (e.Code == 0 && e.Message == "") {
		return "不明なエラー"
	}
	return fmt.Sprintf("%d %s", e.Code, e.Message)
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// voiceChannel は VOICE_CHANNEL_SELECT と SELECT_VOICE_CHANNEL の data。抜けたら ChannelID は空。
type voiceChannel struct {
	ChannelID string `json:"channel_id"`
	ID        string `json:"id"`
}

// left は VOICE_CHANNEL_SELECT のイベントが、ボイスチャンネルから抜けたことを表すかを返す。
func left(m message) bool {
	if m.Evt != "VOICE_CHANNEL_SELECT" {
		return false
	}
	var v voiceChannel
	return json.Unmarshal(m.Data, &v) == nil && v.ChannelID == ""
}

// deadline は ctx の期限と取り消しを接続の読み書きに反映する。返した関数で元に戻す。
func (c *conn) deadline(ctx context.Context) func() {
	if d, ok := ctx.Deadline(); ok {
		_ = c.c.SetDeadline(d)
	}
	stop := context.AfterFunc(ctx, func() { _ = c.c.SetDeadline(time.Now()) })
	return func() {
		stop()
		_ = c.c.SetDeadline(time.Time{})
	}
}
