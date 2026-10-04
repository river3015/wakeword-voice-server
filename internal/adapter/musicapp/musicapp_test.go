package musicapp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeOsascript は引数を記録し、body を実行するシェルスクリプト。
func fakeOsascript(t *testing.T, body string) (*Music, string) {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + record + "'\n" + body + "\n"
	path := filepath.Join(dir, "osascript")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Music{Executable: path, Timeout: 5 * time.Second}, record
}

func TestPlaylistNameIsPassedAsArgument(t *testing.T) {
	m, record := fakeOsascript(t, "echo true")
	found, err := m.HasPlaylist(context.Background(), `作業用" & quit`)
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	args, _ := os.ReadFile(record)
	list := strings.Split(strings.TrimRight(string(args), "\n"), "\n")
	if i := slices.Index(list, "--"); i < 0 || !slices.Equal(list[i+1:], []string{`作業用" & quit`}) {
		t.Errorf("args = %q", list)
	}
}

func TestCurrent(t *testing.T) {
	m, _ := fakeOsascript(t, `printf 'Song\nArtist\n'`)
	track, err := m.Current(context.Background())
	if err != nil || track.Name != "Song" || track.Artist != "Artist" {
		t.Errorf("track = %+v, err = %v", track, err)
	}
	m, _ = fakeOsascript(t, `printf '\n'`)
	if track, _ := m.Current(context.Background()); track.Name != "" {
		t.Errorf("停止中なのに曲名がある: %+v", track)
	}
}

func TestPermissionError(t *testing.T) {
	m, _ := fakeOsascript(t, `echo 'execution error: Not authorized to send Apple events to Music. (-1743)' >&2; exit 1`)
	if err := m.Pause(context.Background()); err == nil || !strings.Contains(err.Error(), "ミュージックの操作が許可されていません") {
		t.Errorf("err = %v", err)
	}
}
