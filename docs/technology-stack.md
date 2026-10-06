# 技術構成と導入手順

## 利用前提

自宅で本人が使う非商用のツール。macOS（Apple Silicon）で常駐させる。英語のウェイクワードで起動し、依頼・文字起こし・返答・読み上げは日本語にする。音声処理はOSSを使い、ローカルで完結させる。AI処理は公式Codex CLIまたはClaude Code CLIを使い、設定で切り替える。

## スタック

2026-10-03にPython版のPoCからGo版へ移行した。Python版はGit履歴（d4f87c9まで）に残っている。

| 役割 | 技術 | 状態 |
| --- | --- | --- |
| 制御・状態管理 | Go 1.25 | 実装・単体テスト済み |
| マイク入力・再生 | malgo v0.11.26（miniaudio、CoreAudio） | 内蔵マイク入力・スピーカー再生を確認。外部コマンドや一時ファイルを使わない |
| ウェイクワード | openWakeWordのONNXモデル＋onnxruntime_go v1.36.0／ONNX Runtime 1.29.0 | 前処理をGoで再実装。Python版とフレームごとのスコアが一致 |
| 発話区間検出 | Silero VAD（openWakeWord配布のv4形式） | 同上 |
| 日本語文字起こし | whisper.cppのwhisper-server（呼びかけ時に起動し、使わなければ止める）、公式Whisper多言語モデル | Metal使用で3.7秒の依頼を約0.17秒で認識 |
| AIへの依頼 | 公式Codex CLI（`codex exec`）、Claude Code CLI（`claude -p`、逐次受信） | どちらも実応答を確認 |
| 日本語読み上げ | VOICEVOX ENGINE 0.25.2 | 文ごとに合成し、再生と並行して次の文を合成 |
| 常駐 | `wakeword serve`。LaunchAgent用plistを生成可能 | ターミナル起動を確認。LaunchAgentは未登録 |

既定のウェイクワードは「Hey Mycroft」。`[[wake_words]]`で「Hey Jarvis」などを追加できる。メルスペクトログラムと埋め込みは共通で1回だけ計算し、ウェイクワードごとの小さな判定モデルだけを並べる。2026-10-07の計測では、80msの1フレームの処理が約1.5ms、判定モデル1つの追加分は約0.025msだった。openWakeWordの同梱モデルは非商用限定という前提で使う。ライセンスは[調査メモ](licensing-and-costs.md)を参照。

## 構成

クリーンアーキテクチャの考え方で、外部とつながる部分をポート（インターフェース）で切り離している。依存の向きは外側から内側への一方向だけ。

```text
cmd/wakeword/          main：設定を読み、部品を組み立てる。子プロセスの起動・停止
internal/domain/       会話履歴、制御語、録音の判定（Recorder）。外部依存なし
internal/usecase/      一往復の進行、待ち受けの繰り返し、ポートの定義
internal/adapter/
  audio/               マイク入力・再生（malgo）、WAV
  onnx/                openWakeWord、Silero VAD
  whisper/             whisper-serverのクライアント
  codex/               Codex CLIの実行
  claude/              Claude Code CLIの実行と逐次受信
  voicevox/            VOICEVOXのクライアント
  localhttp/           ループバック限定のHTTPクライアント
  process/             子プロセスと重複起動防止のロック
  fake/                学習・試験用の偽物の部品（cmd/fake-turn で使う）
internal/config/       TOML設定
```

返答は受信・合成・再生の3段をgoroutineで並行に流す。Claude CLIは返答を逐次受け取り、文ができた順に合成する。Codex CLIは完成した返答をまとめて返すが、文ごとに合成して最初の文から再生するので、長い返答でも最初の声が早く出る。

## 導入手順

必要なもの：Go 1.25以上、Xcode Command Line Tools（cgo用）、CMake、7z、Codex CLIまたはClaude Code CLIへのログイン。

```sh
python3 scripts/download-models.py        # openWakeWord・Silero VAD・Whisperのモデル（SHA-256照合）
sh scripts/download-onnxruntime.sh        # ONNX Runtime 1.29.0（SHA-256照合）
git clone https://github.com/ggml-org/whisper.cpp.git .vendor/whisper.cpp
git -C .vendor/whisper.cpp checkout 60c0be6ac8fa71b1a2ae2dd938a31a34a508e774
cmake -S .vendor/whisper.cpp -B .vendor/whisper.cpp/build -DCMAKE_BUILD_TYPE=Release -DWHISPER_BUILD_TESTS=OFF
cmake --build .vendor/whisper.cpp/build --config Release -j 4
go build -o bin/wakeword ./cmd/wakeword
```

VOICEVOXの取得は[一往復の手順](one-turn.md)を参照する。モデル・配布物・ビルド成果物はGitに含めない。

```sh
cp config.example.toml config.local.toml   # ai_executable などを実環境に合わせる
bin/wakeword devices                       # 入出力デバイス名の確認
go test ./...                              # 単体テスト（モデルがあればONNXの比較テストも動く）
```

## 検証記録

### Python版PoC（2026-10-03）

openWakeWord 0.6.0、Silero VAD、whisper.cpp、Codex CLI、VOICEVOXで、録音済みWAVからの一往復、内蔵マイクでの継続待ち受け、Codexへの追加依頼を確認した。陽性確認には上流openWakeWordのコミット`368c03716d1e92591906a84949bc477f3a834455`にある`tests/data/hey_mycroft_test.wav`を使い、先頭0.64秒のフレームでスコア1.0だった。日本語の接続確認では、macOSのKyokoで「こんにちは。日本語で短く返事をしてください。」を合成し、16kHzに変換して使った。

### Go版（2026-10-03）

- 単体テスト：domain、usecase（並行パイプライン、失敗時の停止、読み上げ短縮、待ち受けの繰り返し）、各adapter。`go test -race`で通過。
- ONNX推論：Python版で書き出したフレームごとのスコアと比較し、ウェイクワードの差は0、VADの差は最大3e-08。`scripts/golden-scores.py`で基準値を作る。上流のHey Mycroft音声は0.64秒・スコア1.0、無音は未検出で、Python版と同じ。
- 実機：録音済みWAVからwhisper-server・Codex・VOICEVOX・スピーカー再生までの一往復、内蔵マイクの3秒待ち受け2回、子プロセスの自動起動と終了時の停止を確認。
- 速度の比較は[応答時間](latency.md)を参照。
- 人の声による一往復、長時間運転、LaunchAgentでの起動は未検証。
