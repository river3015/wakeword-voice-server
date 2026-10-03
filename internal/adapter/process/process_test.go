package process

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStartWaitsForReadyAndStops(t *testing.T) {
	calls := 0
	ready := func(context.Context) error {
		if calls++; calls < 3 {
			return errors.New("not yet")
		}
		return nil
	}
	child, err := Start(context.Background(), "sleep", []string{"/bin/sleep", "30"}, "", 5*time.Second, ready)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	child.Stop()
	select {
	case <-child.Done():
	default:
		t.Fatal("停止後も動いている")
	}
	if calls != 3 || time.Since(start) > 2*time.Second {
		t.Errorf("calls = %d, stop took %v", calls, time.Since(start))
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	never := func(context.Context) error { return errors.New("never") }
	if _, err := Start(context.Background(), "false", []string{"/usr/bin/false"}, "", 5*time.Second, never); err == nil {
		t.Error("終了したプロセスを起動済みと扱った")
	}
}

func TestLockRejectsSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.lock")
	release, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(path); err == nil {
		t.Error("二重にロックできた")
	}
	release()
	again, err := Lock(path)
	if err != nil {
		t.Fatal("解放後にロックできない: ", err)
	}
	again()
}
