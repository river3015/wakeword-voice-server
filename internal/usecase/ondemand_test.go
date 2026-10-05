package usecase

import (
	"context"
	"errors"
	"testing"
)

// countingService は準備と利用の回数を数える。
type countingService struct {
	prepared, acquired, released int
	err                          error
}

func (s *countingService) Prepare() { s.prepared++ }

func (s *countingService) Acquire(context.Context) error {
	if s.err != nil {
		return s.err
	}
	s.acquired++
	return nil
}

func (s *countingService) Release() { s.released++ }

func TestRunPreparesServicesAndReleasesAfterUse(t *testing.T) {
	runner, player, _ := newRunner(t, "挨拶して", &stubResponder{chunks: []string{"はい。こんにちは。"}})
	stt, tts := &countingService{}, &countingService{}
	runner.Transcriber = OnDemandTranscriber{Transcriber: runner.Transcriber, Service: stt}
	runner.Synthesizer = OnDemandSynthesizer{Synthesizer: runner.Synthesizer, Service: tts}

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stt.prepared != 1 || tts.prepared != 1 {
		t.Errorf("prepared = %d, %d", stt.prepared, tts.prepared)
	}
	if stt.acquired != 1 || stt.released != 1 || tts.acquired != len(player.spoken) || tts.released != tts.acquired {
		t.Errorf("stt = %+v, tts = %+v, spoken = %d", stt, tts, len(player.spoken))
	}
}

func TestRunSkipsPrepareWithoutSpeech(t *testing.T) {
	runner, _, _ := newRunner(t, "", &stubResponder{})
	runner.Listener = stubListener{}
	stt := &countingService{}
	runner.Transcriber = OnDemandTranscriber{Transcriber: runner.Transcriber, Service: stt}

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stt.prepared != 0 {
		t.Error("発話がないのに準備を始めた")
	}
}

func TestOnDemandTranscriberReportsStartFailure(t *testing.T) {
	stt := &countingService{err: errors.New("起動できません")}
	transcriber := OnDemandTranscriber{Transcriber: stubTranscriber("x"), Service: stt}
	if _, err := transcriber.Transcribe(context.Background(), []int16{1}); err == nil || stt.released != 0 {
		t.Errorf("err = %v, released = %d", err, stt.released)
	}
}
