# 想定技術スタックとPoC

## 利用前提

自宅で本人が使う非商用のツール。macOS（当面Apple Silicon）で常駐させる。英語のウェイクワードで起動し、依頼・文字起こし・返答・読み上げは日本語にする。音声処理はなるべくOSSを使い、ローカルで完結させる。AI処理はClaude／Codexの公式CLIを候補とする。

## 想定スタック

| 役割 | 想定技術 | 状態 |
| --- | --- | --- |
| 制御・状態管理 | Python 3.12、uv | Python 3.12.13で環境構築・テスト済み |
| マイク入力 | sounddevice／PortAudio | sounddevice 0.5.3を導入。内蔵マイク入力を確認済み |
| ウェイクワード | openWakeWord、ONNX Runtime | 公式Hey Mycroftテスト音声で実推論確認済み |
| 発話区間検出 | Silero VAD | 実WAVで発話収集・無音終端を確認済み |
| 日本語文字起こし | whisper.cpp、公式Whisper多言語モデル | ビルド・baseモデルで日本語合成音声を認識済み |
| AIへの依頼 | 公式Codex CLI、後からClaude Code | Codex実応答・追加依頼を確認。Claudeはアダプターのみ |
| 日本語読み上げ | VOICEVOX | 0.25.2を導入。四国めたんで実合成・再生を確認済み |
| 常駐 | 当初はターミナル起動。後からlaunchd | 起動スクリプト・継続待ち受けを実装。LaunchAgentは未登録 |

既定のウェイクワードは「Hey Mycroft」。公式WAVで検出を確認した。人の声での確認後に必要なら調整する。「Hey Jarvis」も比較候補であり、依頼内容とは別に検出する。openWakeWordの同梱モデルは非商用限定という前提で使う。ライセンスの詳細は[調査メモ](licensing-and-costs.md)を参照。

Python 3.12を想定する理由は、音声関連のネイティブ依存を含めて互換性を検証する範囲を絞るため。現環境のPython 3.14で標準ライブラリだけのテストは通過したが、音声パッケージの対応確認には使っていない。

## 最初のPoC

まず常時録音を始めず、録音済みファイルで各部品を独立に検証する。

1. WAVの形式チェック：16kHz、モノラル、16bit PCM。
2. 英語の呼びかけを録音したWAVで、openWakeWordの検出スコアを確認する。
3. 日本語の依頼を録音した別のWAVで、whisper.cppによる文字起こしを確認する。
4. 上記が動いたら、マイク入力・Silero VAD・受付音を接続する。
5. その後にAI一往復とVOICEVOXを接続する。AIの認証方式・作業ディレクトリ・実行権限を固定する。

この手順でファイルPoCから始め、その後マイク入力、発話検出、AI一往復、読み上げ、会話履歴、継続待ち受けを実装した。現在の実行方法は[運用手順](operations.md)、確認済み事項と残る検証は[完成監査](completion-audit.md)を参照する。

## 実行手順

形式チェックとテストは標準ライブラリだけで実行できる。

```sh
python3 -m wakeword_voice --help
python3 -m unittest discover -s tests -v
python3 -m wakeword_voice check-wav /absolute/path/request.wav
```

ウェイクワード検証用の環境構築候補：

```sh
uv venv --python 3.12
uv pip install --python .venv/bin/python '.[wake]'
.venv/bin/python -m wakeword_voice detect-wake /absolute/path/wake.wav --model /absolute/path/hey_jarvis.onnx
```

以下はフェーズ1時点の調査記録。現在の導入・実行結果は末尾のフェーズ2記録を参照。openWakeWord 0.6.0をPoCの検証対象とし、現Mac上でPython 3.12を対象に`uv pip compile pyproject.toml --extra wake --python-version 3.12`を実行して18パッケージの依存解決に成功した。ONNX Runtimeが選ばれ、TFLite Runtimeは解決結果に含まれなかった。これは依存解決単独ではモデル互換性や実推論の成功を示さない。その後の確認は末尾のフェーズ2記録を参照。

openWakeWordは指定したキーワードモデルだけでなく、共有の特徴抽出・メルスペクトログラムモデルも必要。上流の[モデル準備手順](https://github.com/dscripka/openWakeWord)に従って配置する。PoC側ではモデルを自動ダウンロードしない。

日本語文字起こし：

```sh
python3 -m wakeword_voice transcribe /absolute/path/request.wav --model /absolute/path/ggml-base.bin --whisper-cli /absolute/path/whisper-cli
```

既定では認識の有無と文字数だけを表示する。内容を確認するときのみ`--show-text`を付ける。日本語用には`.en`ではない多言語モデルを使う。whisper.cppの準備は[上流README](https://github.com/ggml-org/whisper.cpp)、CLI引数は[公式CLI例](https://github.com/ggml-org/whisper.cpp/tree/master/examples/cli)を参照する。

このファイルPoCのコマンドはユーザーが用意した音声ファイルを読む。capture／once／serveのマイク入力と保存方針は[発話収集](capture.md)と[運用](operations.md)を参照する。文字起こし結果は一時ディレクトリに生成し、成功・失敗にかかわらず終了時に削除する。録音ファイルは削除しない。標準出力の文字起こしを表示した場合、その保存先や端末ログは利用側で扱う。

## 初期PoC時点の検証記録

- 現環境：arm64、Python 3.14.8、uv 0.12.22。
- 標準ライブラリによる5件のテストが通過。WAV入力、不適切なサンプルレート、日本語指定と結果取得、一時結果削除、タイムアウト、失敗時の外部ログ非表示を検証。
- CLIヘルプと不正ファイル時のエラー処理を確認。
- PyPIのメタデータ取得による依存解決に成功。パッケージのインストールは行っていない。
- 単体テストではwhisper-cliの代わりにテストダブルを使用。実モデルでの別途検証は末尾に記載。
- フェーズ1時点では環境構築と実推論が残っていた。その後の結果は運用手順と完成監査を参照する。

## フェーズ2の実機確認と再現手順

2026-10-03、Apple Silicon上で実モデル推論を確認した。モデルはGitには含めず、`python3 scripts/download-models.py`で取得する。取得後にSHA-256を確認し、不一致は採用しない。ハッシュは今回の取得物を固定するための値で、第三者監査の証明ではない。

```sh
uv venv --python 3.12 --managed-python .venv
uv pip install --python .venv/bin/python '.[wake]'
python3 scripts/download-models.py
git clone https://github.com/ggml-org/whisper.cpp.git .vendor/whisper.cpp
git -C .vendor/whisper.cpp checkout 60c0be6ac8fa71b1a2ae2dd938a31a34a508e774
cmake -S .vendor/whisper.cpp -B .vendor/whisper.cpp/build -DCMAKE_BUILD_TYPE=Release -DWHISPER_BUILD_TESTS=OFF
cmake --build .vendor/whisper.cpp/build --config Release -j 4
```

openWakeWordの共有ONNXモデルはキーワードモデルと同じディレクトリに置く。パッケージ内部へコピーする必要はない。

陽性確認は上流openWakeWordのコミット`368c03716d1e92591906a84949bc477f3a834455`にある`tests/data/hey_mycroft_test.wav`を使用し、スコア1.0、先頭0.64秒のフレームで検出した。Hey Jarvisの合成音声は検出されなかった。実際に採用する呼びかけは、人の声で比較して決める。

日本語の接続確認ではmacOSのKyokoで「こんにちは。日本語で短く返事をしてください。」をファイル合成し、ffmpegで16kHz／16bit／モノラルに変換した。baseモデルの出力は「こんにちは、日本語で短く返事をしてください。」だった。macOS音声合成はこの検証用で、製品の読み上げをVOICEVOXから変更したわけではない。

単体テスト5件はPython 3.12でも通過。モデルダウンロードスクリプトは既存5ファイルのハッシュ照合を確認。ダウンロード経路は今回curl／上流スクリプトで実行したため、新しいスクリプトの初回取得は未検証。マイク録音、AIの呼び出し、VOICEVOXは未実施。

Hey Mycroftモデルは3秒の無音WAVでは未検出、最大スコア0.0だった。
