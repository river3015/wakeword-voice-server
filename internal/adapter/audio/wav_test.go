package audio

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWAVRoundTrip(t *testing.T) {
	pcm := PCM{SampleRate: 24000, Samples: []int16{1, -2, 3, 32767, -32768}}
	got, err := DecodeWAV(EncodeWAV(pcm))
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleRate != 24000 || !slices.Equal(got.Samples, pcm.Samples) {
		t.Errorf("got %+v", got)
	}
}

func TestRejectsInvalidWAV(t *testing.T) {
	stereo := EncodeWAV(PCM{SampleRate: 16000, Samples: []int16{1, 2}})
	stereo[22] = 2 // チャンネル数
	truncated := EncodeWAV(PCM{SampleRate: 16000, Samples: []int16{1, 2, 3}})
	for name, data := range map[string][]byte{
		"not wav":   []byte("invalid"),
		"stereo":    stereo,
		"truncated": truncated[:len(truncated)-2],
		"empty":     EncodeWAV(PCM{SampleRate: 16000}),
	} {
		if _, err := DecodeWAV(data); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
	}
}

func TestRequestWAVRequires16kHz(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.wav")
	os.WriteFile(path, EncodeWAV(PCM{SampleRate: 24000, Samples: []int16{1}}), 0o600)
	if _, err := ReadRequestWAV(path); err == nil {
		t.Error("24kHzを受け付けた")
	}
}

func TestWAVFileFramesArePadded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.wav")
	os.WriteFile(path, EncodeWAV(PCM{SampleRate: 16000, Samples: make([]int16, frameSamples+10)}), 0o600)
	source, err := WAVFile{Path: path}.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		frame, err := source.Next(context.Background())
		if err != nil || len(frame) != frameSamples {
			t.Fatalf("frame = %d, err = %v", len(frame), err)
		}
	}
	if _, err := source.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v", err)
	}
}
