package reminders

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
func fakeOsascript(t *testing.T, body string) (*Reminders, string) {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + record + "'\n" + body + "\n"
	path := filepath.Join(dir, "osascript")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Reminders{Executable: path, Timeout: 5 * time.Second}, record
}

func TestAddPassesValuesAsArguments(t *testing.T) {
	r, record := fakeOsascript(t, `echo "-牛乳\" & quit"`)
	stored, err := r.Add(context.Background(), "買い物", `-牛乳" & quit`)
	if err != nil || stored != `-牛乳" & quit` {
		t.Fatalf("stored = %q, err = %v", stored, err)
	}
	args, _ := os.ReadFile(record)
	list := strings.Split(strings.TrimRight(string(args), "\n"), "\n")
	// 値はスクリプトに埋め込まず、-- の後ろに引数として渡す
	if i := slices.Index(list, "--"); i < 0 || !slices.Equal(list[i+1:], []string{"買い物", `-牛乳" & quit`}) {
		t.Errorf("args = %q", list)
	}
	if strings.Contains(strings.Join(list[:slices.Index(list, "--")], "\n"), "牛乳") {
		t.Error("項目がスクリプトに埋め込まれている")
	}
}

func TestCheckReportsMissingLists(t *testing.T) {
	r, _ := fakeOsascript(t, `printf '買い物\nリマインダー\n'`)
	if err := r.Check(context.Background(), []string{"買い物"}); err != nil {
		t.Fatal(err)
	}
	err := r.Check(context.Background(), []string{"買い物", "ToDo", "メモ"})
	if err == nil || !strings.Contains(err.Error(), "ToDo、メモ") {
		t.Errorf("err = %v", err)
	}
}

func TestErrorKeepsOnlyNumber(t *testing.T) {
	r, _ := fakeOsascript(t, `echo '0:1: execution error: 秘密の項目 is not allowed. (-1728)' >&2; exit 1`)
	_, err := r.Items(context.Background(), "買い物")
	if err == nil || !strings.Contains(err.Error(), "-1728") || strings.Contains(err.Error(), "秘密") {
		t.Errorf("err = %v", err)
	}
	r, _ = fakeOsascript(t, `echo 'execution error: Not authorized to send Apple events to Reminders. (-1743)' >&2; exit 1`)
	if _, err := r.Items(context.Background(), "買い物"); err == nil || !strings.Contains(err.Error(), "オートメーション") {
		t.Errorf("err = %v", err)
	}
}

func TestTimeout(t *testing.T) {
	r, _ := fakeOsascript(t, "sleep 10")
	r.Timeout = 200 * time.Millisecond
	_, err := r.Items(context.Background(), "買い物")
	if err == nil || !strings.Contains(err.Error(), "タイムアウト") {
		t.Errorf("err = %v", err)
	}
}
