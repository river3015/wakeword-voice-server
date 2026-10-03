package onnx

// Detector は待ち受け中はウェイクワード、録音中は発話区間だけを推論する。
type Detector struct {
	Wake *WakeWord
	VAD  *VAD
}

func NewDetector(wakeModel, vadModel string) (*Detector, error) {
	wake, err := NewWakeWord(wakeModel)
	if err != nil {
		return nil, err
	}
	vad, err := NewVAD(vadModel)
	if err != nil {
		wake.Close()
		return nil, err
	}
	return &Detector{Wake: wake, VAD: vad}, nil
}

func (d *Detector) Scores(frame []int16, waiting bool) (wake, speech float64, err error) {
	if waiting {
		wake, err = d.Wake.Predict(frame)
	} else {
		speech, err = d.VAD.Predict(frame)
	}
	return wake, speech, err
}

func (d *Detector) Reset() error {
	d.VAD.Reset()
	return d.Wake.Reset()
}

func (d *Detector) Close() {
	d.Wake.Close()
	d.VAD.Close()
}
