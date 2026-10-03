package domain

import (
	"strings"
	"testing"
	"time"
)

func TestHistoryAndExpiry(t *testing.T) {
	now := time.Unix(0, 0)
	history, err := NewConversation(10*time.Second, 6, 16000, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	history.Remember("合言葉は青い月", "覚えました")
	if !strings.Contains(history.Prompt("合言葉は？"), "青い月") {
		t.Error("履歴がプロンプトに含まれない")
	}
	now = now.Add(10 * time.Second)
	if got := history.Prompt("新しい依頼"); got != "新しい依頼" {
		t.Errorf("期限切れ後のプロンプト = %q", got)
	}
	if history.Turns() != 0 {
		t.Error("期限切れの履歴が残っている")
	}
}

func TestHistoryIsBounded(t *testing.T) {
	history, _ := NewConversation(time.Minute, 2, 1000, nil)
	for n := range 5 { // Go 1.22 以降は整数を range できる
		history.Remember(strings.Repeat(string(rune('0'+n)), 200), strings.Repeat("答え", 100))
	}
	if history.Turns() != 2 {
		t.Errorf("turns = %d, want 2", history.Turns())
	}
	history.Remember(strings.Repeat("x", 1001), "reply")
	if history.Turns() != 0 {
		t.Error("上限を超える一往復で履歴が消えていない")
	}
}

// テーブル駆動テスト：Go でよく使う書き方。
func TestControlRequiresExactPhrase(t *testing.T) {
	cases := []struct {
		text string
		want Action
	}{
		{"会話終了。", ActionEndConversation},
		{"サーバー停止", ActionStopServer},
		{"キャンセルについて説明して", ActionNone},
	}
	for _, c := range cases {
		if got := Control(c.text); got != c.want {
			t.Errorf("Control(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
