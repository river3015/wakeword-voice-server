"""開発用：Python版openWakeWordのフレームごとのスコアを書き出し、Go版の推論と比べる基準にする。

Python版は削除済みのため、比較用の環境を別に用意する。録音・基準値はGitに含めない。
  uv venv --python 3.12 .venv
  uv pip install --python .venv/bin/python openwakeword==0.6.0 onnxruntime 'numpy<3'
  .venv/bin/python scripts/golden-scores.py wake combined   # .recordings/wake.wav などを読む
  go test ./internal/adapter/onnx/
"""
import json
from pathlib import Path
import sys
import wave

import numpy as np
import onnxruntime as ort
from openwakeword.model import Model
from openwakeword.vad import VAD

ort.disable_telemetry_events()
root = Path(__file__).resolve().parents[1]
models = root / '.models/openwakeword'
result = {}
for name in sys.argv[1:]:
    model = Model(wakeword_models=[str(models / 'hey_mycroft_v0.1.onnx')], inference_framework='onnx',
                  melspec_model_path=str(models / 'melspectrogram.onnx'),
                  embedding_model_path=str(models / 'embedding_model.onnx'))
    vad = VAD(model_path=str(models / 'silero_vad.onnx'))
    wake_scores, vad_scores = [], []
    with wave.open(str(root / '.recordings' / f'{name}.wav'), 'rb') as source:
        while data := source.readframes(1280):
            samples = np.frombuffer(data, dtype='<i2')
            samples = np.pad(samples, (0, 1280 - len(samples)))
            wake_scores.append(float(max(model.predict(samples).values())))
            vad_scores.append(float(vad.predict(samples, frame_size=640)))
    result[name] = {'wake': wake_scores, 'vad': vad_scores}
output = root / '.runtime/golden/scores.json'
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(result))
print(output)
