package discord

import (
	"context"
	"fmt"
	"sync"
)

// session は張りっぱなしの接続。1つの goroutine が読み続け、命令の応答は nonce で振り分け、
// ボイスチャンネルから抜けたイベントは leftSignal で知らせる。
type session struct {
	ipc  *conn
	done chan struct{} // 接続が切れたら閉じる

	mu      sync.Mutex
	pending map[string]chan reply
	left    chan struct{} // 抜けたら閉じ、次のために作り直す
}

// reply は命令の応答。left は応答を受け取った時点の「抜けた」の通知先で、
// 応答より前に届いたイベントでは閉じず、後に届いたイベントでだけ閉じる。
type reply struct {
	message
	left <-chan struct{}
}

func newSession(ipc *conn) *session {
	s := &session{ipc: ipc, done: make(chan struct{}), pending: map[string]chan reply{}, left: make(chan struct{})}
	go s.readLoop()
	return s
}

func (s *session) readLoop() {
	defer close(s.done)
	for {
		m, err := s.ipc.read()
		if err != nil {
			return
		}
		s.mu.Lock()
		if waiter, ok := s.pending[m.Nonce]; ok && m.Nonce != "" {
			delete(s.pending, m.Nonce)
			waiter <- reply{message: m, left: s.left}
		} else if left(m) {
			close(s.left)
			s.left = make(chan struct{})
		}
		s.mu.Unlock()
	}
}

func (s *session) alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// command は命令を送り、応答を待つ。
func (s *session) command(ctx context.Context, cmd string, args any, evt string) (reply, error) {
	nonce := newNonce()
	waiter := make(chan reply, 1)
	s.mu.Lock()
	s.pending[nonce] = waiter
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, nonce)
		s.mu.Unlock()
	}()
	payload := map[string]any{"cmd": cmd, "args": args, "nonce": nonce}
	if evt != "" {
		payload["evt"] = evt
	}
	if err := s.ipc.send(opFrame, payload); err != nil {
		return reply{}, err
	}
	select {
	case r := <-waiter:
		if r.Evt == "ERROR" {
			return reply{}, fmt.Errorf("DiscordのRPCの%sに失敗しました: %s", cmd, rpcError(r.Data))
		}
		return r, nil
	case <-s.done:
		return reply{}, fmt.Errorf("DiscordのRPCの接続が切れました（%s）", cmd)
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
}

func (s *session) close() { s.ipc.close() }
