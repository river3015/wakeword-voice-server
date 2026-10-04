// Package musicapp は macOS のミュージック.app を osascript で操作する。
// ライブラリにある曲とプレイリストだけを扱う。
package musicapp

import (
	"context"
	"strings"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/applescript"
	"github.com/river3015/wakeword-voice-server/internal/skill/music"
)

const (
	// 止まっていて再生する曲がなければ、ライブラリ全体を再生する
	playScript = `on run argv
	tell application "Music"
		if player state is stopped then
			play library playlist 1
		else
			play
		end if
	end tell
end run`
	playPlaylistScript = `on run argv
	tell application "Music" to play user playlist (item 1 of argv)
end run`
	hasPlaylistScript = `on run argv
	tell application "Music" to return (exists user playlist (item 1 of argv)) as text
end run`
	pauseScript    = `tell application "Music" to pause`
	nextScript     = `tell application "Music" to next track`
	previousScript = `tell application "Music" to previous track`
	// 1行目に曲名、2行目にアーティスト。止まっていれば空
	currentScript = `on run argv
	tell application "Music"
		if player state is stopped then return ""
		return (name of current track) & linefeed & (artist of current track)
	end tell
end run`
)

type Music struct {
	Executable string // 空なら /usr/bin/osascript
	Timeout    time.Duration
}

func (m *Music) run(ctx context.Context, script string, args ...string) (string, error) {
	runner := applescript.Runner{App: "ミュージック", Executable: m.Executable, Timeout: m.Timeout}
	return runner.Run(ctx, script, args...)
}

func (m *Music) exec(ctx context.Context, script string, args ...string) error {
	_, err := m.run(ctx, script, args...)
	return err
}

// Check はミュージック.app を操作できるかを確かめる。初回はmacOSが自動化の許可を求める。
func (m *Music) Check(ctx context.Context) error {
	_, err := m.run(ctx, `tell application "Music" to return player state as text`)
	return err
}

func (m *Music) Play(ctx context.Context) error     { return m.exec(ctx, playScript) }
func (m *Music) Pause(ctx context.Context) error    { return m.exec(ctx, pauseScript) }
func (m *Music) Next(ctx context.Context) error     { return m.exec(ctx, nextScript) }
func (m *Music) Previous(ctx context.Context) error { return m.exec(ctx, previousScript) }

func (m *Music) PlayPlaylist(ctx context.Context, name string) error {
	return m.exec(ctx, playPlaylistScript, name)
}

func (m *Music) HasPlaylist(ctx context.Context, name string) (bool, error) {
	out, err := m.run(ctx, hasPlaylistScript, name)
	return strings.TrimSpace(out) == "true", err
}

func (m *Music) Current(ctx context.Context) (music.Track, error) {
	out, err := m.run(ctx, currentScript)
	if err != nil {
		return music.Track{}, err
	}
	lines := applescript.Lines(out)
	var track music.Track
	if len(lines) > 0 {
		track.Name = lines[0]
	}
	if len(lines) > 1 {
		track.Artist = lines[1]
	}
	return track, nil
}
