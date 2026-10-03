// Package domain は音声会話の概念だけを持つ。標準ライブラリ以外に依存しない。
package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Action は文字起こし結果が制御語だった場合の操作。
type Action int

const (
	ActionNone Action = iota // iota は 0, 1, 2... と連番を振る
	ActionEndConversation
	ActionStopServer
)

func (a Action) String() string {
	switch a {
	case ActionEndConversation:
		return "end_conversation"
	case ActionStopServer:
		return "stop_server"
	default:
		return "none"
	}
}

// Control は発話全体が制御語と一致する場合だけ操作を返す。
func Control(text string) Action {
	normalized := strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t\n。、.!！?？", r) {
			return -1 // -1 を返すとその文字を削除する
		}
		return r
	}, text)
	switch normalized {
	case "会話終了", "会話を終了", "キャンセル", "会話を終わります":
		return ActionEndConversation
	case "サーバー停止":
		return ActionStopServer
	}
	return ActionNone
}

// Turn は成功した一往復。
type Turn struct {
	User      string `json:"user"`
	Assistant string `json:"assistant"`
}

// Conversation は上限と有効期限つきでメモリにだけ履歴を保持する。
type Conversation struct {
	ttl           time.Duration
	maxTurns      int
	maxCharacters int
	now           func() time.Time // テストで時刻を差し替えるため関数で持つ
	turns         []Turn
	lastActivity  time.Time
}

func NewConversation(ttl time.Duration, maxTurns, maxCharacters int, now func() time.Time) (*Conversation, error) {
	if ttl < time.Second || ttl > 24*time.Hour || maxTurns < 1 || maxTurns > 20 ||
		maxCharacters < 1000 || maxCharacters > 20000 {
		return nil, errors.New("会話履歴の上限設定が不正です")
	}
	if now == nil {
		now = time.Now
	}
	return &Conversation{ttl: ttl, maxTurns: maxTurns, maxCharacters: maxCharacters, now: now}, nil
}

func (c *Conversation) Clear() {
	c.turns = nil
	c.lastActivity = time.Time{}
}

func (c *Conversation) Turns() int { return len(c.turns) }

func (c *Conversation) expire() {
	if !c.lastActivity.IsZero() && c.now().Sub(c.lastActivity) >= c.ttl {
		c.Clear()
	}
}

// Prompt は履歴があれば今回の依頼の前に付ける。
func (c *Conversation) Prompt(text string) string {
	c.expire()
	if len(c.turns) == 0 {
		return text
	}
	context, _ := json.Marshal(c.turns) // []Turn の Marshal は失敗しない
	return "以下はこの音声会話の過去の発話です。\n" + string(context) + "\n今回の依頼:\n" + text
}

// Remember は完全な往復だけを残し、文字数の上限を超えたら古いものから捨てる。
func (c *Conversation) Remember(text, reply string) {
	c.expire()
	if size(Turn{text, reply}) > c.maxCharacters {
		c.Clear()
		return
	}
	c.turns = append(c.turns, Turn{text, reply})
	if len(c.turns) > c.maxTurns {
		c.turns = c.turns[1:]
	}
	for total(c.turns) > c.maxCharacters {
		c.turns = c.turns[1:]
	}
	c.lastActivity = c.now()
}

// size は Python の len と同じく、バイト数ではなく文字数で数える。
func size(t Turn) int {
	return utf8.RuneCountInString(t.User) + utf8.RuneCountInString(t.Assistant)
}

func total(turns []Turn) int {
	sum := 0
	for _, t := range turns {
		sum += size(t)
	}
	return sum
}
