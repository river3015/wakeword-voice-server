package voicevox

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
)

func TestSynthesizeQueriesThenSynthesizes(t *testing.T) {
	var routes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/audio_query":
			w.Write([]byte(`{"q":1}`))
		case "/synthesis":
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"q":1}` {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			w.Write(audio.EncodeWAV(audio.PCM{SampleRate: 24000, Samples: []int16{1}}))
		}
	}))
	defer server.Close()
	client, err := New(server.URL, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	wav, err := client.Synthesize(context.Background(), "こんにちは")
	if err != nil || string(wav[:4]) != "RIFF" {
		t.Fatalf("err = %v", err)
	}
	if len(routes) != 2 || routes[1] != "/synthesis?speaker=2" {
		t.Errorf("routes = %v", routes)
	}
}

func TestRejectsNonWAVAndRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid"))
	}))
	defer server.Close()
	client, _ := New(server.URL, 0, time.Second)
	if _, err := client.Synthesize(context.Background(), "こんにちは"); err == nil {
		t.Error("WAVでない返答を受け付けた")
	}
	if _, err := New("http://192.168.1.1:50021", 0, time.Second); err == nil {
		t.Error("リモートのエンジンを受け付けた")
	}
}
