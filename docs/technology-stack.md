# 想定技術スタックとPoC

## 利用前提

自宅で本人が使う非商用のツール。macOS（当面Apple Silicon）で常駐させる。英語のウェイクワードで起動し、依頼・文字起こし・返答・読み上げは日本語にする。音声処理はなるべくOSSを使い、ローカルで完結させる。AI処理はClaude／Codexの公式CLIを候補とする。

## 想定スタック

| 役割 | 想定技術 | 状態 |
| --- | --- | --- |
| 制御・状態管理 | Python 3.12、uv | PoCコードを作成。Python 3.12環境の構築は未完了 |
| マイク入力 | sounddevice／PortAudio | 未導入。macOSのマイク許可も実機確認が必要 |
| ウェイクワード | openWakeWord、ONNX Runtime | 録音済みWAV向けアダプター作成。実モデルでは未検証 |
| 発話区間検出 | Silero VAD | 未実装 |
| 日本語文字起こし | whisper.cpp、公式Whisper多言語モデル | CLIアダプター作成。バイナリ・モデルは未導入 |
| AIへの依頼 | 公式Codex CLI、後からClaude Code | 未接続。現環境でCodex実行ファイルのみ確認 |
| 日本語読み上げ | VOICEVOX | 未導入。音声選択とクレジット対応が必要 |
| 常駐 | 当初はターミナル起動。後からlaunchd | 未実装 |

ウェイクワードは未確定。「Hey Jarvis」は候補であり、依頼内容とは別に検出する。openWakeWordの同梱モデルは非商用限定という前提で使う。ライセンスの詳細は[調査メモ](licensing-and-costs.md)を参照。

Python 3.12を想定する理由は、音声関連のネイティブ依存を含めて互換性を検証する範囲を絞るため。現環境のPython 3.14で標準ライブラリだけのテストは通過したが、音声パッケージの対応確認には使っていない。

## 最初のPoC

まず常時録音を始めず、録音済みファイルで各部品を独立に検証する。

1. WAVの形式チェック：16kHz、モノラル、16bit PCM。
2. 英語の呼びかけを録音したWAVで、openWakeWordの検出スコアを確認する。
3. 日本語の依頼を録音した別のWAVで、whisper.cppによる文字起こしを確認する。
4. 上記が動いたら、マイク入力・Silero VAD・受付音を接続する。
5. その後にAI一往復とVOICEVOXを接続する。AIの認証方式・作業ディレクトリ・実行権限を固定する。

現在のPoCは1〜3のコマンドを用意した段階。AI呼び出し、マイク入力、読み上げ、会話履歴、待ち受け復帰は含まない。`detect-wake`の直後に`transcribe`を自動実行する構成でもない。

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

インストールと実行は未検証。openWakeWord 0.6.0をPoCの検証対象とし、現Mac上でPython 3.12を対象に`uv pip compile pyproject.toml --extra wake --python-version 3.12`を実行して18パッケージの依存解決に成功した。ONNX Runtimeが選ばれ、TFLite Runtimeは解決結果に含まれなかった。これはモデル互換性や実推論の成功を示すものではない。

openWakeWordは指定したキーワードモデルだけでなく、共有の特徴抽出・メルスペクトログラムモデルも必要。上流の[モデル準備手順](https://github.com/dscripka/openWakeWord)に従って配置する。PoC側ではモデルを自動ダウンロードしない。

日本語文字起こし：

```sh
python3 -m wakeword_voice transcribe /absolute/path/request.wav --model /absolute/path/ggml-base.bin --whisper-cli /absolute/path/whisper-cli
```

既定では認識の有無と文字数だけを表示する。内容を確認するときのみ`--show-text`を付ける。日本語用には`.en`ではない多言語モデルを使う。whisper.cppの準備は[上流README](https://github.com/ggml-org/whisper.cpp)、CLI引数は[公式CLI例](https://github.com/ggml-org/whisper.cpp/tree/master/examples/cli)を参照する。

録音はユーザーが用意したファイルのみ読む。文字起こし結果は一時ディレクトリに生成し、成功・失敗にかかわらず終了時に削除する。録音ファイルは削除しない。標準出力の文字起こしを表示した場合、その保存先や端末ログは利用側で扱う。

## 実施済みの検証と残る作業

- 現環境：arm64、Python 3.14.8、uv 0.12.22。
- 標準ライブラリによる5件のテストが通過。WAV入力、不適切なサンプルレート、日本語指定と結果取得、一時結果削除、タイムアウト、失敗時の外部ログ非表示を検証。
- CLIヘルプと不正ファイル時のエラー処理を確認。
- PyPIのメタデータ取得による依存解決に成功。パッケージのインストールは行っていない。
- whisper-cliの代わりにテストダブルを使用。実音声認識やウェイクワード検出の成功を示すものではない。
- 残る作業：Python 3.12の環境構築、依存・モデルの取得とライセンス確認、録音済みWAVでの実推論。その後マイクとVADを接続する。
