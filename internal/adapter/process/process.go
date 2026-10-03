// Package process は whisper-server や VOICEVOX などの子プロセスを起動・停止する。
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Child は起動した子プロセス。プロセスグループごと停止する。
type Child struct {
	Name string
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

// Start は子プロセスを起動し、ready が成功するまで待つ。ready が nil なら待たない。
// 標準出力・エラーには発話内容が含まれ得るので保存しない。
func Start(ctx context.Context, name string, argv []string, dir string, timeout time.Duration,
	ready func(context.Context) error) (*Child, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%sを起動できません: %w", name, err)
	}
	c := &Child{Name: name, cmd: cmd, done: make(chan struct{})}
	go func() {
		c.err = cmd.Wait()
		close(c.done)
	}()
	if ready == nil {
		return c, nil
	}
	deadline := time.Now().Add(timeout)
	for {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		err := ready(probe)
		cancel()
		if err == nil {
			return c, nil
		}
		select {
		case <-c.done:
			return nil, fmt.Errorf("%sが起動直後に終了しました", name)
		case <-ctx.Done():
			c.Stop()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			c.Stop()
			return nil, fmt.Errorf("%sの起動がタイムアウトしました", name)
		}
	}
}

// Done は子プロセスが終了すると閉じる。
func (c *Child) Done() <-chan struct{} { return c.done }

// Stop は SIGTERM を送り、5秒で終わらなければ SIGKILL する。
func (c *Child) Stop() {
	select {
	case <-c.done:
		return
	default:
	}
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		<-c.done
	}
}

// Lock は同じプロジェクトの重複起動を防ぐ。返した関数で解放する。
func Lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("このプロジェクトの音声サーバーは起動済みです")
		}
		return nil, err
	}
	return func() { file.Close() }, nil
}
