package music

import (
	"context"
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		text string
		want Request
		ok   bool
	}{
		{"音楽をかけて", Request{Command: Play}, true},
		{"曲を流して。", Request{Command: Play}, true},
		{"再生して", Request{Command: Play}, true},
		{"音楽を止めて", Request{Command: Pause}, true},
		{"一時停止", Request{Command: Pause}, true},
		{"ストップ", Request{Command: Pause}, true},
		{"次の曲", Request{Command: Next}, true},
		{"曲をスキップして", Request{Command: Next}, true},
		{"前の曲に戻して", Request{Command: Previous}, true},
		{"今かかってる曲は何？", Request{Command: NowPlaying}, true},
		{"この曲は何", Request{Command: NowPlaying}, true},
		{"今かかってる曲はなん?", Request{Command: NowPlaying}, true},
		{"作業用のプレイリストをかけて", Request{PlayPlaylist, "作業用"}, true},
		{"プレイリスト、ドライブを再生して", Request{PlayPlaylist, "ドライブ"}, true},
		// 音楽についての質問は AI へ渡す
		{"おすすめの曲を教えて", Request{}, false},
		{"音楽の歴史について", Request{}, false},
		{"プレイリストの作り方を教えて", Request{}, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
		}
	}
}

type fakePlayer struct {
	calls     []string
	playlists []string
	track     Track
}

func (f *fakePlayer) record(name string) error       { f.calls = append(f.calls, name); return nil }
func (f *fakePlayer) Play(context.Context) error     { return f.record("play") }
func (f *fakePlayer) Pause(context.Context) error    { return f.record("pause") }
func (f *fakePlayer) Next(context.Context) error     { return f.record("next") }
func (f *fakePlayer) Previous(context.Context) error { return f.record("previous") }
func (f *fakePlayer) PlayPlaylist(_ context.Context, name string) error {
	return f.record("playlist:" + name)
}
func (f *fakePlayer) HasPlaylist(_ context.Context, name string) (bool, error) {
	return slices.Contains(f.playlists, name), nil
}
func (f *fakePlayer) Current(context.Context) (Track, error) { return f.track, nil }

func run(t *testing.T, s *Skill, text string) (string, func(context.Context) error) {
	t.Helper()
	invoke, ok := s.Match(text)
	if !ok {
		t.Fatalf("Match(%q) が一致しない", text)
	}
	outcome, err := invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return outcome.Reply, outcome.AfterSpeech
}

func TestPlayStartsAfterSpeechAndPauseBefore(t *testing.T) {
	player := &fakePlayer{playlists: []string{"作業用"}}
	s := &Skill{Player: player}

	reply, after := run(t, s, "音楽をかけて")
	if len(player.calls) != 0 || after == nil || reply != "音楽を再生します。" {
		t.Fatalf("読み上げ前に再生した: calls = %v, reply = %q", player.calls, reply)
	}
	after(context.Background())

	if reply, after = run(t, s, "音楽を止めて"); after != nil || reply != "音楽を止めました。" {
		t.Errorf("reply = %q", reply)
	}
	_, after = run(t, s, "作業用のプレイリストをかけて")
	after(context.Background())
	if want := []string{"play", "pause", "playlist:作業用"}; !slices.Equal(player.calls, want) {
		t.Errorf("calls = %v, want %v", player.calls, want)
	}
	if reply, after = run(t, s, "ドライブのプレイリストをかけて"); after != nil || reply != "「ドライブ」というプレイリストは見つかりませんでした。" {
		t.Errorf("reply = %q", reply)
	}
}

func TestNowPlaying(t *testing.T) {
	player := &fakePlayer{}
	s := &Skill{Player: player}
	if reply, _ := run(t, s, "今かかってる曲は何"); reply != "今は音楽を再生していません。" {
		t.Errorf("reply = %q", reply)
	}
	player.track = Track{Name: "Song", Artist: "Artist"}
	if reply, _ := run(t, s, "この曲は何"); reply != "今の曲はArtistの「Song」です。" {
		t.Errorf("reply = %q", reply)
	}
}
