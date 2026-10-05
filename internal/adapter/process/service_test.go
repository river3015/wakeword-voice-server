package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestService は、起動中だけ印のファイルを置く sh を子プロセスにする。
func newTestService(t *testing.T, idle time.Duration) (*Service, *atomic.Int32) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "running")
	var inits atomic.Int32
	s := &Service{Name: "test",
		Argv:         []string{"/bin/sh", "-c", `touch "$0"; trap 'rm -f "$0"; exit 0' TERM; sleep 30 & wait`, marker},
		StartTimeout: 5 * time.Second, IdleTimeout: idle,
		Ready: func(context.Context) error {
			if _, err := os.Stat(marker); err != nil {
				return errors.New("not ready")
			}
			return nil
		},
		Init: func(context.Context) error { inits.Add(1); return nil }}
	t.Cleanup(s.Close)
	return s, &inits
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("条件を満たしませんでした")
}

func TestServiceStartsOnceForConcurrentUsers(t *testing.T) {
	s, inits := newTestService(t, 0)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := s.Acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			s.Release()
		})
	}
	wg.Wait()
	if inits.Load() != 1 {
		t.Errorf("inits = %d", inits.Load())
	}
}

func TestServiceStopsWhenIdleAndRestarts(t *testing.T) {
	s, inits := newTestService(t, 100*time.Millisecond)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	child := s.child
	time.Sleep(200 * time.Millisecond)
	select {
	case <-child.Done():
		t.Fatal("使用中に止めた")
	default:
	}
	s.Release()
	eventually(t, func() bool {
		select {
		case <-child.Done():
			return true
		default:
			return false
		}
	})
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()
	if inits.Load() != 2 {
		t.Errorf("inits = %d", inits.Load())
	}
}

func TestServiceRestartsAfterCrash(t *testing.T) {
	s, inits := newTestService(t, 0)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()
	s.child.Stop() // 外から落とされた場合
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()
	if inits.Load() != 2 {
		t.Errorf("inits = %d", inits.Load())
	}
}

func TestServiceUsesRunningInstanceWithoutStopping(t *testing.T) {
	var launched atomic.Bool
	s := &Service{Name: "external", Argv: []string{"/usr/bin/false"}, IdleTimeout: 10 * time.Millisecond,
		Ready: func(context.Context) error { return nil },
		Init:  func(context.Context) error { launched.Store(true); return nil }}
	defer s.Close()
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()
	if s.child != nil || !launched.Load() {
		t.Errorf("child = %v, init = %v", s.child, launched.Load())
	}
}

func TestServiceWithoutArgvFailsWhenNotRunning(t *testing.T) {
	s := &Service{Name: "missing", Ready: func(context.Context) error { return errors.New("down") }}
	defer s.Close()
	if err := s.Acquire(context.Background()); err == nil {
		t.Error("起動できないのに成功した")
	}
}

func TestServiceCloseStopsChild(t *testing.T) {
	s, _ := newTestService(t, 0)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	child := s.child
	s.Release()
	s.Close()
	select {
	case <-child.Done():
	default:
		t.Fatal("Close 後も動いている")
	}
	if err := s.Acquire(context.Background()); err == nil {
		t.Error("Close 後に使えた")
	}
}
