package discord

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memorySecrets) Get(service string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[service]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (m *memorySecrets) Set(service, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[service] = value
	return nil
}

// fakeDiscord は Discord アプリの IPC を真似る。受け取った命令を記録し、selectErr があれば失敗を返す。
type fakeDiscord struct {
	dir       string
	commands  chan map[string]any
	selectErr bool
	conn      chan *conn
	accepted  atomic.Int32
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	t.Helper()
	// Unix ソケットのパスは104バイトまでなので、短い場所に作る
	dir, err := os.MkdirTemp("/tmp", "ipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "discord-ipc-0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	f := &fakeDiscord{dir: dir, commands: make(chan map[string]any, 16), conn: make(chan *conn, 1)}
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			f.accepted.Add(1)
			go f.serve(&conn{c: c})
		}
	}()
	return f
}

func (f *fakeDiscord) serve(c *conn) {
	defer c.close()
	if _, err := c.readRaw(); err != nil { // handshake
		return
	}
	c.send(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "READY", "data": map[string]any{"v": 1}})
	for {
		body, err := c.readRaw()
		if err != nil {
			return
		}
		var m map[string]any
		json.Unmarshal(body, &m)
		f.commands <- m
		reply := map[string]any{"cmd": m["cmd"], "nonce": m["nonce"], "data": map[string]any{}}
		if m["cmd"] == "SELECT_VOICE_CHANNEL" {
			// 前の通話の「抜けた」が遅れて届いても、新しい通話を終わらせないことを確かめる
			c.send(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "VOICE_CHANNEL_SELECT",
				"data": map[string]any{"channel_id": nil}})
			if f.selectErr {
				reply["evt"] = "ERROR"
				reply["data"] = map[string]any{"code": 5003, "message": "already in a voice channel"}
			} else {
				c.send(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "VOICE_CHANNEL_SELECT",
					"data": map[string]any{"channel_id": "42"}})
				f.conn <- c
			}
		}
		c.send(opFrame, reply)
	}
}

// readRaw はテストの相手側で1フレームの本文を読む。
func (c *conn) readRaw() ([]byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(c.c, header[:]); err != nil {
		return nil, err
	}
	body := make([]byte, binary.LittleEndian.Uint32(header[4:]))
	_, err := io.ReadFull(c.c, body)
	return body, err
}

func newTestClient(t *testing.T, f *fakeDiscord, saved token) (*Client, *memorySecrets, *int) {
	t.Helper()
	refreshes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_secret") != "secret" || r.Form.Get("grant_type") != "refresh_token" ||
			r.Form.Get("refresh_token") != saved.RefreshToken {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		refreshes++
		json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 604800})
	}))
	t.Cleanup(server.Close)
	stored, _ := json.Marshal(saved)
	secrets := &memorySecrets{values: map[string]string{SecretService: "secret", TokenService: string(stored)}}
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	client := &Client{ClientID: "1", ChannelID: "42", Secrets: secrets, SocketDir: f.dir, TokenURL: server.URL,
		Now: func() time.Time { return now }}
	t.Cleanup(client.Close)
	return client, secrets, &refreshes
}

func TestJoinRefreshesTokenAndWaitsUntilLeft(t *testing.T) {
	f := newFakeDiscord(t)
	client, secrets, refreshes := newTestClient(t, f, token{RefreshToken: "old-refresh"})

	call, err := client.Join(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for range 3 {
		m := <-f.commands
		cmds = append(cmds, m["cmd"].(string))
		if m["cmd"] == "AUTHENTICATE" && m["args"].(map[string]any)["access_token"] != "new-access" {
			t.Errorf("access_token = %v", m["args"])
		}
		if m["cmd"] == "SELECT_VOICE_CHANNEL" && m["args"].(map[string]any)["channel_id"] != "42" {
			t.Errorf("channel_id = %v", m["args"])
		}
	}
	if want := []string{"AUTHENTICATE", "SUBSCRIBE", "SELECT_VOICE_CHANNEL"}; len(cmds) != 3 || cmds[0] != want[0] || cmds[1] != want[1] || cmds[2] != want[2] {
		t.Errorf("commands = %v", cmds)
	}
	var saved token
	stored, _ := secrets.Get(TokenService)
	json.Unmarshal([]byte(stored), &saved)
	if *refreshes != 1 || saved.RefreshToken != "new-refresh" || saved.AccessToken != "new-access" {
		t.Errorf("refreshes = %d, saved = %+v", *refreshes, saved)
	}

	waited := make(chan error, 1)
	go func() { waited <- call.Wait(context.Background()) }()
	select {
	case <-waited:
		t.Fatal("通話中に待ち受けへ戻った")
	case <-time.After(100 * time.Millisecond):
	}
	c := <-f.conn
	c.send(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "VOICE_CHANNEL_SELECT", "data": map[string]any{"channel_id": nil}})
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("抜けても戻らない")
	}
}

func TestJoinUsesValidAccessTokenWithoutRefresh(t *testing.T) {
	f := newFakeDiscord(t)
	expires := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	client, _, refreshes := newTestClient(t, f, token{AccessToken: "cached", RefreshToken: "r", ExpiresAt: expires})
	call, err := client.Join(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = call
	if m := <-f.commands; m["args"].(map[string]any)["access_token"] != "cached" || *refreshes != 0 {
		t.Errorf("args = %v, refreshes = %d", m["args"], *refreshes)
	}
}

func TestJoinReportsSelectError(t *testing.T) {
	f := newFakeDiscord(t)
	f.selectErr = true
	client, _, _ := newTestClient(t, f, token{RefreshToken: "old-refresh"})
	if _, err := client.Join(context.Background()); err == nil {
		t.Error("参加に失敗したのに成功した")
	}
}

func TestJoinFailsWithoutDiscord(t *testing.T) {
	client := &Client{ClientID: "1", ChannelID: "42", SocketDir: t.TempDir(),
		Secrets: &memorySecrets{values: map[string]string{TokenService: `{"access_token":"a","refresh_token":"r","expires_at":"2099-01-01T00:00:00Z"}`}}}
	if _, err := client.Join(context.Background()); err == nil {
		t.Error("Discord がないのに成功した")
	}
}

func TestJoinReusesConnection(t *testing.T) {
	f := newFakeDiscord(t)
	client, _, refreshes := newTestClient(t, f, token{RefreshToken: "old-refresh"})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		call, err := client.Join(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		c := <-f.conn
		c.send(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "VOICE_CHANNEL_SELECT", "data": map[string]any{"channel_id": nil}})
		if err := call.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.accepted.Load() != 1 || *refreshes != 1 {
		t.Errorf("connections = %d, refreshes = %d", f.accepted.Load(), *refreshes)
	}
}

func TestWaitReturnsWhenDiscordQuits(t *testing.T) {
	f := newFakeDiscord(t)
	client, _, _ := newTestClient(t, f, token{RefreshToken: "old-refresh"})
	call, err := client.Join(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	(<-f.conn).close()
	done := make(chan error, 1)
	go func() { done <- call.Wait(context.Background()) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Discord が終了しても戻らない")
	}
}
