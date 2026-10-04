// Package music は「音楽をかけて」「次の曲」のような決まった言い回しで音楽を操作する。
// 操作は Player に任せる。再生を始める操作は、返答を読み上げた後に行う。
package music

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/river3015/wakeword-voice-server/internal/usecase"
)

// Track は再生中の曲。Name が空なら再生していない。
type Track struct {
	Name, Artist string
}

type Player interface {
	Play(ctx context.Context) error
	PlayPlaylist(ctx context.Context, name string) error
	HasPlaylist(ctx context.Context, name string) (bool, error)
	Pause(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
	Current(ctx context.Context) (Track, error)
}

type Command int

const (
	None Command = iota
	Play
	PlayPlaylist
	Pause
	Next
	Previous
	NowPlaying
)

// Request は発話から読み取った操作。Playlist は PlayPlaylist のときだけ使う。
type Request struct {
	Command  Command
	Playlist string
}

type Skill struct {
	Player Player
}

func (s *Skill) Name() string { return "music" }

func (s *Skill) Vocabulary() []string { return []string{"プレイリスト"} }

func (s *Skill) Match(text string) (usecase.Invocation, bool) {
	request, ok := Parse(text)
	if !ok {
		return nil, false
	}
	return func(ctx context.Context) (usecase.Outcome, error) { return s.run(ctx, request) }, true
}

func (s *Skill) run(ctx context.Context, r Request) (usecase.Outcome, error) {
	switch r.Command {
	case Play:
		return usecase.Outcome{Reply: "音楽を再生します。", AfterSpeech: s.Player.Play}, nil
	case PlayPlaylist:
		// プレイリストがなければ、読み上げの前にわかるよう先に確かめる
		found, err := s.Player.HasPlaylist(ctx, r.Playlist)
		if err != nil {
			return usecase.Outcome{}, err
		}
		if !found {
			return usecase.Outcome{Reply: fmt.Sprintf("「%s」というプレイリストは見つかりませんでした。", r.Playlist)}, nil
		}
		play := func(ctx context.Context) error { return s.Player.PlayPlaylist(ctx, r.Playlist) }
		return usecase.Outcome{Reply: fmt.Sprintf("プレイリスト「%s」を再生します。", r.Playlist), AfterSpeech: play}, nil
	case Pause:
		// 返答が音楽に重ならないよう、先に止める
		if err := s.Player.Pause(ctx); err != nil {
			return usecase.Outcome{}, err
		}
		return usecase.Outcome{Reply: "音楽を止めました。"}, nil
	case Next:
		return usecase.Outcome{Reply: "次の曲にします。", AfterSpeech: s.Player.Next}, nil
	case Previous:
		return usecase.Outcome{Reply: "前の曲にします。", AfterSpeech: s.Player.Previous}, nil
	case NowPlaying:
		track, err := s.Player.Current(ctx)
		if err != nil {
			return usecase.Outcome{}, err
		}
		if track.Name == "" {
			return usecase.Outcome{Reply: "今は音楽を再生していません。"}, nil
		}
		if track.Artist == "" {
			return usecase.Outcome{Reply: fmt.Sprintf("今の曲は「%s」です。", track.Name)}, nil
		}
		return usecase.Outcome{Reply: fmt.Sprintf("今の曲は%sの「%s」です。", track.Artist, track.Name)}, nil
	}
	return usecase.Outcome{}, fmt.Errorf("未対応の操作です: %d", r.Command)
}

const (
	musicWords = `(?:音楽|曲|ミュージック|BGM)`
	playVerb   = `(?:かけて|再生して|流して|つけて)(?:ください)?`
	// maxPlaylistCharacters を超える名前は誤認識の可能性が高いため扱わない
	maxPlaylistCharacters = 50
)

var rules = []struct {
	re      *regexp.Regexp
	command Command
}{
	// 「音楽をかけて」「再生して」
	{regexp.MustCompile(`^(?:` + musicWords + `を?)?` + playVerb + `$|^再生$`), Play},
	// 「音楽を止めて」「一時停止」「ストップ」
	{regexp.MustCompile(`^(?:` + musicWords + `を?)?(?:止めて|とめて|停止(?:して)?|一時停止(?:して)?|ストップ(?:して)?)(?:ください)?$`), Pause},
	// 「次の曲」「曲をスキップして」
	{regexp.MustCompile(`^(?:次の曲(?:に(?:して|進んで))?|(?:曲を)?スキップ(?:して)?)(?:ください)?$`), Next},
	// 「前の曲」「前の曲に戻して」
	{regexp.MustCompile(`^前の曲(?:に(?:して|戻して))?(?:ください)?$`), Previous},
	// 「今かかってる曲は何？」「この曲は何」
	{regexp.MustCompile(`^(?:今|いま)?(?:かかってる|かかっている|流れてる|流れている|再生中の|この)(?:曲|音楽)(?:は|って)?(?:何|なに|なん)?(?:ですか)?$`), NowPlaying},
}

// playlist は「作業用のプレイリストをかけて」「プレイリスト作業用を再生して」。
var playlist = regexp.MustCompile(`^(?:(.+?)の?プレイリスト|プレイリスト[、,]?\s*(.+?))を?` + playVerb + `$`)

// Parse は発話全体が決まった言い回しに一致する場合だけ操作を返す。
func Parse(text string) (Request, bool) {
	text = strings.TrimRight(strings.TrimSpace(text), " 　。.!！?？")
	for _, rule := range rules {
		if rule.re.MatchString(text) {
			return Request{Command: rule.command}, true
		}
	}
	if m := playlist.FindStringSubmatch(text); m != nil {
		name := strings.Trim(m[1]+m[2], " 　、,「」『』\"'")
		if name == "" || utf8.RuneCountInString(name) > maxPlaylistCharacters {
			return Request{}, false
		}
		return Request{Command: PlayPlaylist, Playlist: name}, true
	}
	return Request{}, false
}
