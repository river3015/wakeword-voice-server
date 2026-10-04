// Package lists は「買い物リストに牛乳を追加して」のような決まった言い回しで、
// 買い物・やること・メモのリストへ追加し、読み上げる。保存先は Store に任せる。
package lists

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// Kind はリストの種類。保存先のリスト名は設定で対応づける。
type Kind string

const (
	Shopping Kind = "shopping"
	ToDo     Kind = "todo"
	Memo     Kind = "memo"
)

// spoken は読み上げに使う呼び名。
var spoken = map[Kind]string{Shopping: "買い物リスト", ToDo: "やることリスト", Memo: "メモ"}

// vocabulary は文字起こしのヒントにする語。
var vocabulary = map[Kind][]string{Shopping: {"買い物リスト"}, ToDo: {"やることリスト", "ToDoリスト"}, Memo: {"メモ"}}

const (
	// MaxItemCharacters を超える項目は誤認識の可能性が高いため登録しない
	MaxItemCharacters = 60
	// maxReadItems を超える分は件数だけ伝える
	maxReadItems = 10
)

// Store はリストの保存先。Add は保存した項目名を返し、登録後の読み返しに使う。
type Store interface {
	Add(ctx context.Context, list, item string) (string, error)
	Items(ctx context.Context, list string) ([]string, error)
}

// Skill は usecase.Skill を満たす。Names は Kind ごとの保存先のリスト名。
type Skill struct {
	Store Store
	Names map[Kind]string
}

func (s *Skill) Name() string { return "lists" }

// Vocabulary は設定したリストの呼び名。文字起こしのヒントに使う。
func (s *Skill) Vocabulary() []string {
	var words []string
	for _, kind := range []Kind{Shopping, ToDo, Memo} {
		if s.Names[kind] != "" {
			words = append(words, vocabulary[kind]...)
		}
	}
	return words
}

func (s *Skill) Match(text string) (usecase.Invocation, bool) {
	request, ok := Parse(text)
	if !ok {
		return nil, false
	}
	name := s.Names[request.Kind]
	if name == "" {
		return nil, false // 設定していない種類は AI へ渡す
	}
	if request.Item == "" {
		return func(ctx context.Context) (usecase.Outcome, error) {
			reply, err := s.read(ctx, request.Kind, name)
			return usecase.Outcome{Reply: reply}, err
		}, true
	}
	return func(ctx context.Context) (usecase.Outcome, error) {
		reply, err := s.add(ctx, request.Kind, name, request.Item)
		return usecase.Outcome{Reply: reply}, err
	}, true
}

func (s *Skill) add(ctx context.Context, kind Kind, name, item string) (string, error) {
	stored, err := s.Store.Add(ctx, name, item)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%sに「%s」を追加しました。", spoken[kind], stored), nil
}

func (s *Skill) read(ctx context.Context, kind Kind, name string) (string, error) {
	items, err := s.Store.Items(ctx, name)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return spoken[kind] + "は空です。", nil
	}
	shown := items[:min(len(items), maxReadItems)]
	reply := fmt.Sprintf("%sは%d件です。%s。", spoken[kind], len(items), strings.Join(shown, "、"))
	if rest := len(items) - len(shown); rest > 0 {
		reply += fmt.Sprintf("ほかに%d件あります。", rest)
	}
	return reply, nil
}

// Request は発話から読み取った依頼。Item が空なら読み上げの依頼。
type Request struct {
	Kind Kind
	Item string
}

const (
	shoppingWords = `(?:お?買い物|買物|かいもの|カイモノ)(?:リスト)?`
	todoWords     = `(?:(?i:todo)|トゥードゥー|やること|タスク)(?:リスト)?`
	memoWords     = `メモ(?:帳)?`
	// 追加の言い回し。「追加して」「入れといて」「登録お願いします」など
	addVerb = `(?:(?:追加|登録)(?:して(?:おいて|ください|くれる|ね)?|しといて|しておいて|お願い(?:します)?)?` +
		`|入れ(?:て|といて|ておいて)(?:ください)?)`
	readVerb = `(?:を|の中身を|の内容を)?(?:読み上げて|読んで|教えて)(?:ください)?`
)

var (
	listWords = map[Kind]*regexp.Regexp{
		Shopping: regexp.MustCompile(`^` + shoppingWords + `$`),
		ToDo:     regexp.MustCompile(`^` + todoWords + `$`),
		Memo:     regexp.MustCompile(`^` + memoWords + `$`),
	}
	anyList = `(` + shoppingWords + `|` + todoWords + `|` + memoWords + `)`
	// 「買い物リストに牛乳を追加して」
	listFirst = regexp.MustCompile(`^` + anyList + `に(.+?)を?` + addVerb + `$`)
	// 「牛乳を買い物リストに追加して」
	itemFirst = regexp.MustCompile(`^(.+?)を` + anyList + `に` + addVerb + `$`)
	// 「明日は燃えるゴミとメモして」「明日は燃えるゴミ、メモして」「メモ、明日は燃えるゴミ」
	memoAfter  = regexp.MustCompile(`^(.+?)(?:と|って|[、,]\s*)メモ(?:して|しといて|しておいて)(?:ください)?$`)
	memoBefore = regexp.MustCompile(`^メモ[、,]\s*(.+)$`)
	// 「買い物リストを読み上げて」「買い物リストに何がある？」
	readList  = regexp.MustCompile(`^` + anyList + readVerb + `$`)
	whatsList = regexp.MustCompile(`^` + anyList + `(?:に|には)何が(?:ある|入ってる)(?:の|か)?$`)
)

// Parse は発話全体が決まった言い回しに一致する場合だけ依頼を返す。
// 「買い物リストに何を追加すべき？」のような質問は一致させず、AI へ渡す。
func Parse(text string) (Request, bool) {
	text = normalize(text)
	if m := listFirst.FindStringSubmatch(text); m != nil {
		return addRequest(m[1], m[2])
	}
	if m := itemFirst.FindStringSubmatch(text); m != nil {
		return addRequest(m[2], m[1])
	}
	if m := memoAfter.FindStringSubmatch(text); m != nil {
		return addRequest("メモ", m[1])
	}
	if m := memoBefore.FindStringSubmatch(text); m != nil {
		return addRequest("メモ", m[1])
	}
	for _, re := range []*regexp.Regexp{readList, whatsList} {
		if m := re.FindStringSubmatch(text); m != nil {
			kind, ok := kindOf(m[1])
			return Request{Kind: kind}, ok
		}
	}
	return Request{}, false
}

func addRequest(list, item string) (Request, bool) {
	kind, ok := kindOf(list)
	if !ok {
		return Request{}, false
	}
	item, err := CleanItem(item)
	if err != nil {
		return Request{}, false
	}
	return Request{Kind: kind, Item: item}, true
}

func kindOf(word string) (Kind, bool) {
	for kind, re := range listWords {
		if re.MatchString(word) {
			return kind, true
		}
	}
	return "", false
}

// CleanItem は項目名から括弧や前後の記号を外し、保存できる形か確かめる。
func CleanItem(item string) (string, error) {
	item = strings.Trim(item, " 　、,。「」『』\"'")
	if item == "" {
		return "", errors.New("項目が空です")
	}
	if utf8.RuneCountInString(item) > MaxItemCharacters {
		return "", errors.New("項目が長すぎます")
	}
	if strings.IndexFunc(item, unicode.IsControl) >= 0 {
		return "", errors.New("項目に制御文字が含まれています")
	}
	return item, nil
}

// normalize は前後の空白と文末の記号を外す。文中の読点は「メモ、…」の判定に使うので残す。
func normalize(text string) string {
	return strings.TrimRight(strings.TrimSpace(text), " 　。.!！?？")
}
