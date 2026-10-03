package whisper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/river3015/wakeword-voice-server/internal/adapter/audio"
)

func TestTranscribeSendsWAVForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("file")
		if err != nil || r.URL.Path != "/inference" || r.FormValue("language") != "ja" ||
			r.FormValue("response_format") != "text" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		data := make([]byte, 1024)
		n, _ := file.Read(data)
		if pcm, err := audio.DecodeWAV(data[:n]); err != nil || pcm.SampleRate != 16000 || len(pcm.Samples) != 3 {
			http.Error(w, "bad wav", http.StatusBadRequest)
			return
		}
		w.Write([]byte(" こんにちは\n"))
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Transcribe(context.Background(), []int16{1, 2, 3})
	if err != nil || text != "こんにちは" {
		t.Errorf("text = %q, err = %v", text, err)
	}
}

func TestErrorDoesNotIncludeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "秘密の発話", http.StatusInternalServerError)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	_, err := client.Transcribe(context.Background(), []int16{1})
	if err == nil || err.Error() != "whisper-serverが失敗しました: HTTP 500" {
		t.Errorf("err = %v", err)
	}
}
