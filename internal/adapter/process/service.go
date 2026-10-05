package process

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Service は使うときだけ起動しておく子プロセス（whisper-server や VOICEVOX）。
// Acquire で準備ができるまで待ち、Release から IdleTimeout の間使われなければ止める。
// 別に起動済みのものは使うだけで、止めない。途中で落ちた場合は、次に使うときに起動し直す。
type Service struct {
	Name         string
	Argv         []string // 空なら起動せず、起動済みのものだけを使う
	Dir          string
	StartTimeout time.Duration
	IdleTimeout  time.Duration // 0 なら止めない
	Ready        func(context.Context) error
	// Init は準備ができた後に一度だけ行う初期化。VOICEVOX の話者の読み込みなど。nil なら行わない
	Init func(context.Context) error

	mu       sync.Mutex
	ctx      context.Context // 起動はこの ctx で行い、呼び出し側の取り消しで他の利用者の起動を止めない
	cancel   context.CancelFunc
	starting *startAttempt // 起動中なら nil 以外
	up       bool          // 準備済み
	child    *Child        // 自分で起動した場合だけ nil 以外
	users    int
	idle     *time.Timer
	closed   bool
}

type startAttempt struct {
	done chan struct{}
	err  error
}

// Prepare は待たずに準備を始める。呼びかけを検出した時点で呼び、使うまでに起動を終えておく。
func (s *Service) Prepare() {
	go func() {
		if err := s.Acquire(context.Background()); err != nil {
			slog.Warn("service prepare failed", "service", s.Name, "error", err.Error())
			return
		}
		s.Release()
	}()
}

// Acquire は準備ができるまで待ち、使用中にする。使い終えたら Release を呼ぶ。
func (s *Service) Acquire(ctx context.Context) error {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return fmt.Errorf("%sは停止済みです", s.Name)
		}
		if s.up && s.alive() {
			s.users++
			if s.idle != nil {
				s.idle.Stop()
			}
			s.mu.Unlock()
			return nil
		}
		attempt := s.starting
		if attempt == nil {
			s.up, s.child = false, nil
			attempt = &startAttempt{done: make(chan struct{})}
			s.starting = attempt
			go s.start(attempt)
		}
		s.mu.Unlock()

		select {
		case <-attempt.done:
			if attempt.err != nil {
				return attempt.err
			}
			// 準備できた。直後に止められた場合に備え、ロックを取り直して確かめる
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Release は使用中を解除する。誰も使っていなければ、IdleTimeout 後に止める。
func (s *Service) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users > 0 {
		s.users--
	}
	if s.users > 0 || s.IdleTimeout <= 0 || s.closed {
		return
	}
	if s.idle == nil {
		s.idle = time.AfterFunc(s.IdleTimeout, s.stopIdle)
	} else {
		s.idle.Reset(s.IdleTimeout)
	}
}

// Close は自分で起動した子プロセスを止める。以後は使えない。
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel() // 起動中なら中断させる
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	child := s.child
	s.child, s.up = nil, false
	s.mu.Unlock()
	if child != nil {
		child.Stop()
	}
}

// alive は準備済みのものがまだ動いているかを返す。ロックを持って呼ぶ。
func (s *Service) alive() bool {
	if s.child != nil {
		select {
		case <-s.child.Done():
			slog.Warn("service exited", "service", s.Name)
			return false
		default:
			return true
		}
	}
	// 起動済みのものを使っている場合は、止まっていないかを確かめる
	probe, cancel := context.WithTimeout(s.baseContext(), time.Second)
	defer cancel()
	return s.Ready(probe) == nil
}

// baseContext はロックを持って呼ぶ。
func (s *Service) baseContext() context.Context {
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
	return s.ctx
}

func (s *Service) start(attempt *startAttempt) {
	s.mu.Lock()
	ctx := s.baseContext()
	s.mu.Unlock()

	began := time.Now()
	child, err := s.launch(ctx)
	if err == nil && s.Init != nil {
		if err = s.Init(ctx); err != nil {
			err = fmt.Errorf("%sを初期化できません: %w", s.Name, err)
		}
	}

	s.mu.Lock()
	if err == nil && s.closed {
		err = fmt.Errorf("%sは停止済みです", s.Name)
	}
	if err != nil && child != nil {
		defer child.Stop() // ロックを放してから止める
	} else if err == nil {
		s.up, s.child = true, child
	}
	s.starting = nil
	attempt.err = err
	close(attempt.done)
	s.mu.Unlock()

	if err == nil {
		slog.Info("service ready", "service", s.Name, "owned", child != nil, "ms", time.Since(began).Milliseconds())
	}
}

// launch は起動済みならそれを使い、そうでなければ子プロセスを起動する。
func (s *Service) launch(ctx context.Context) (*Child, error) {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	err := s.Ready(probe)
	cancel()
	if err == nil {
		return nil, nil
	}
	if len(s.Argv) == 0 {
		return nil, fmt.Errorf("%sに接続できません: %w", s.Name, err)
	}
	return Start(ctx, s.Name, s.Argv, s.Dir, s.StartTimeout, s.Ready)
}

func (s *Service) stopIdle() {
	s.mu.Lock()
	if s.users > 0 || s.starting != nil || s.child == nil {
		// 使用中、起動中、または起動済みのものを借りているだけなら止めない
		s.mu.Unlock()
		return
	}
	child := s.child
	s.child, s.up = nil, false
	s.mu.Unlock()
	child.Stop()
	slog.Info("service stopped", "service", s.Name, "reason", "idle")
}
