# マイク入力と発話収集

## セットアップと実行

```sh
uv pip install --python .venv/bin/python '.[wake,audio]'
python3 scripts/download-models.py
.venv/bin/python -m wakeword_voice capture --wake-model .models/openwakeword/hey_mycroft_v0.1.onnx --vad-model .models/openwakeword/silero_vad.onnx --device 0
```

既定では30秒待ち受け、呼びかけ後の一つの依頼を収集して終了する。AI呼び出し・読み上げはこのコマンドでは行わない。`Ctrl-C`で停止できる。デバイス番号はMacの構成によって変わる。macOSのマイク許可が必要。許可設定を自動で変更しない。

録音済みWAVで同じ経路を試す場合：

```sh
.venv/bin/python -m wakeword_voice capture --wake-model .models/openwakeword/hey_mycroft_v0.1.onnx --vad-model .models/openwakeword/silero_vad.onnx --source-wav /absolute/path/combined.wav
```

音声を保存したい場合のみ`--save-audio /absolute/path/request.wav`を指定する。既定ではメモリ上に保持して破棄する。標準出力は収集の成否・理由・秒数だけ、標準エラーは状態だけを表示する。モデルはローカルで推論する。

## 状態と制限

- `waiting`：80msの音声フレームをウェイクワード検出へ渡す。
- `recording`：Silero VADで発話を判定する。開始前は直前320msのみ保持する。
- 発話開始前に5秒経過したら破棄する。
- 発話開始後に1.2秒の無音で終了する。合計発話時間が240ms未満なら破棄する。
- 録音上限は30秒。音声は最大でも上限と開始前320ms分に制限する。
- マイクのコールバックでは推論しない。最大25フレームのキューを使い、入力欠落・キュー飽和・3秒の入力停止をエラーにする。古い入力を黙って依頼として使わない。
- 収集終了／停止時にマイクを閉じる。AI処理と読み上げ中にマイクを開かない構成に接続する。

検出されたフレーム自体は依頼音声へ入れない。ただしウェイクワードの途中で検出される場合、その後の語尾が録音に含まれる可能性がある。受付音と短い抑制時間の調整は一往復の実機確認で行う。

## 検証記録（2026-10-03）

- 状態管理の5件と既存5件、合計10件の単体テストが通過。
- openWakeWord上流のHey Mycroft音声＋日本語合成音声＋無音を連結して、呼びかけ検出、録音開始、無音終了、待機状態への復帰を実モデルで確認。5.04秒の依頼音声を収集した。
- 収集した音声をWhisper baseで認識し、「こんにちは、日本語で短く返事をしてください。」を取得した。
- 内蔵マイクを3秒間開き、38フレームを取得。保存・外部送信は行わず正常終了。
- GPU経路はこのsandboxで失敗したため、Whisperの既定はCPUに変更。`transcribe --gpu`で明示的に選択できる。
- 人がマイクへ呼びかけて依頼する一連の動作、距離・雑音下の精度は未検証。受付音、AI、読み上げ、会話継続は次のフェーズ。
