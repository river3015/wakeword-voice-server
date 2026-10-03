package usecase

import (
	"context"
	"time"
)

// Server は一往復を繰り返す。失敗しても待ち時間をおいて次の呼びかけを待つ。
type Server struct {
	Runner   *TurnRunner
	OnResult func(Result)
	OnError  func(error)
	// Sleep は待ち時間。テストで差し替える。取り消されたら false を返す
	Sleep func(ctx context.Context, d time.Duration) bool
}

// Serve は ctx が取り消されるか、停止の指示か、maxCycles 回（0なら無制限）で終わる。
func (s *Server) Serve(ctx context.Context, maxCycles int) {
	defer func() {
		s.Runner.Conversation.Clear()
		s.Runner.notify(StateStopped)
	}()
	sleep := s.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) bool {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(d):
				return true
			}
		}
	}
	failures := 0
	for cycle := 0; maxCycles == 0 || cycle < maxCycles; cycle++ {
		if ctx.Err() != nil {
			return
		}
		result, err := s.Runner.Run(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			s.Runner.notify(StateError)
			if s.OnError != nil {
				s.OnError(err)
			}
			// 2、4、8、10秒…と待つ。失敗した依頼は自動で再送しない
			if !sleep(ctx, min(time.Duration(1<<min(failures, 4))*time.Second, 10*time.Second)) {
				return
			}
			continue
		}
		failures = 0
		if s.OnResult != nil {
			s.OnResult(result)
		}
		if result.Stop {
			return
		}
	}
}
