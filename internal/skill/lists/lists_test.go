package lists

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		text string
		want Request
		ok   bool
	}{
		{"買い物リストに牛乳を追加して。", Request{Shopping, "牛乳"}, true},
		{"買い物リストに、牛乳追加", Request{Shopping, "牛乳"}, true},
		{"お買い物リストに「卵」を入れといて", Request{Shopping, "卵"}, true},
		{"牛乳を買い物リストに追加してください", Request{Shopping, "牛乳"}, true},
		{"ToDoリストに請求書の支払いを追加して", Request{ToDo, "請求書の支払い"}, true},
		{"TODOに歯医者の予約を登録して", Request{ToDo, "歯医者の予約"}, true},
		{"やることリストに洗濯を入れて", Request{ToDo, "洗濯"}, true},
		{"明日は燃えるゴミの日とメモして", Request{Memo, "明日は燃えるゴミの日"}, true},
		{"メモ、駐車場は3階", Request{Memo, "駐車場は3階"}, true},
		{"明日は燃えるゴミの日、メモして。", Request{Memo, "明日は燃えるゴミの日"}, true},
		{"カイモノリストに牛乳を追加して", Request{Shopping, "牛乳"}, true},
		{"ToDoリストに、会社の予約を追加して", Request{ToDo, "会社の予約"}, true},
		{"メモして", Request{}, false},
		{"メモにWi-Fiの更新を追加", Request{Memo, "Wi-Fiの更新"}, true},
		{"買い物リストを読み上げて", Request{Kind: Shopping}, true},
		{"やることリストに何がある？", Request{Kind: ToDo}, true},
		{"メモを教えて", Request{Kind: Memo}, true},
		// 質問や似た話題は AI へ渡す
		{"買い物リストに何を追加すべき？", Request{}, false},
		{"買い物リストのアプリのおすすめを教えて", Request{}, false},
		{"メモの取り方を教えて", Request{}, false},
		{"メモってどうやって取るの", Request{}, false},
		{"牛乳を追加して", Request{}, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
		}
	}
}

func TestCleanItemRejectsLongOrControl(t *testing.T) {
	for _, item := range []string{"", "「」", string(make([]rune, MaxItemCharacters+1)), "牛乳\n卵"} {
		if _, err := CleanItem(item); err == nil {
			t.Errorf("CleanItem(%q) を受け付けた", item)
		}
	}
}

type memoryStore struct {
	lists map[string][]string
	err   error
}

func (m *memoryStore) Add(_ context.Context, list, item string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.lists[list] = append(m.lists[list], item)
	return item, nil
}

func (m *memoryStore) Items(_ context.Context, list string) ([]string, error) {
	return m.lists[list], m.err
}

func newSkill() (*Skill, *memoryStore) {
	store := &memoryStore{lists: map[string][]string{}}
	return &Skill{Store: store, Names: map[Kind]string{Shopping: "買い物", ToDo: "ToDo"}}, store
}

func run(t *testing.T, s *Skill, text string) (string, error) {
	t.Helper()
	invoke, ok := s.Match(text)
	if !ok {
		t.Fatalf("Match(%q) が一致しない", text)
	}
	return invoke(context.Background())
}

func TestAddReadsBackStoredItem(t *testing.T) {
	s, store := newSkill()
	reply, err := run(t, s, "買い物リストに牛乳を追加して")
	if err != nil || reply != "買い物リストに「牛乳」を追加しました。" || len(store.lists["買い物"]) != 1 {
		t.Errorf("reply = %q, err = %v, lists = %v", reply, err, store.lists)
	}
}

func TestReadLimitsItems(t *testing.T) {
	s, store := newSkill()
	if reply, _ := run(t, s, "買い物リストを読み上げて"); reply != "買い物リストは空です。" {
		t.Errorf("reply = %q", reply)
	}
	for n := range 12 {
		store.lists["買い物"] = append(store.lists["買い物"], fmt.Sprint(n))
	}
	want := "買い物リストは12件です。0、1、2、3、4、5、6、7、8、9。ほかに2件あります。"
	if reply, _ := run(t, s, "買い物リストを読み上げて"); reply != want {
		t.Errorf("reply = %q", reply)
	}
}

func TestUnconfiguredKindAndStoreError(t *testing.T) {
	s, store := newSkill()
	if _, ok := s.Match("メモ、駐車場は3階"); ok {
		t.Error("設定していないメモに一致した")
	}
	store.err = errors.New("denied")
	if _, err := run(t, s, "ToDoに洗濯を追加"); err == nil {
		t.Error("保存先のエラーを返さない")
	}
}
