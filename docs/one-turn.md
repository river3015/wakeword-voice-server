# AI連携と日本語読み上げ

音声：**VOICEVOX:四国めたん**（speaker 0、あまあま）。[音源利用規約](https://zunko.jp/con_ongen_kiyaku.html)を確認済み。音声を変更する場合は対応する利用規約とクレジットを更新する。

## VOICEVOXの準備

今回検証したバージョンは0.25.2、macOS arm64版。公式配布物は約1.8GB。Gitにはバイナリやモデルを含めない。

```sh
mkdir -p .vendor/voicevox
curl --fail --location https://github.com/VOICEVOX/voicevox_engine/releases/download/0.25.2/voicevox_engine-macos-arm64-0.25.2.7z.001 -o .vendor/voicevox/engine.7z.001
7z x .vendor/voicevox/engine.7z.001 -o.vendor/voicevox/engine
sh scripts/run-voicevox.sh
```

`7z`は別途必要。エンジンは127.0.0.1:50021だけで待ち受け、辞書・設定変更APIは無効にする。実行時にmacOSのユーザーデータ領域にエンジン設定用ディレクトリが作成される。サーバーのアクセスログに発話が含まれ得るため、出力は破棄する。`bin/wakeword`は、設定の`voicevox_engine`からエンジンを自動で起動する。別に管理したい場合は`sh scripts/run-voicevox.sh`で起動しておく。

```sh
cp config.example.toml config.local.toml
# ai_executableとworkdirを実環境に合わせて編集する。
bin/wakeword once --config config.local.toml
```

呼びかけ→日本語依頼→文字起こし→AI→VOICEVOX→スピーカー再生を一往復して終了する。テスト音声で試す場合は`--source-wav /absolute/path/combined.wav`、呼びかけなしの日本語WAVなら`--request-wav /absolute/path/request.wav`を指定する。どちらもAIの利用量を消費する。

## AIの権限・データ

- Go版はCodexのみ対応。本人が公式CLIへログインして使う。API認証の場合は別途API課金。Claudeのアダプターは、Python版にあった未検証のものを移植していない。
- `reasoning_effort`でCodexの推論の深さを指定できる（既定low）。`model`を空にするとCodexの既定モデルを使う。
- workdirは明示したディレクトリ。Codexの既定はread-only。workspace-writeを設定すればそのディレクトリ内の編集が可能。承認が必要な操作を自動で許可するフラグは付けない。承認が必要な作業は拒否される場合がある。
- Codexはユーザー設定を読み込まず、`--ephemeral`でネイティブの会話セッションを保存しない。CLIの認証は既存のものを使う。現在の対話チャットを再利用する仕組みではない。
- 依頼文はコマンド引数へ入れず、標準入力で渡す。返答は一時ファイルから読み、成功・失敗時に削除する。CLIの標準出力・エラーは保存しない。処理がタイムアウトしたら子プロセスのグループを終了する。
- 音声と返答は一時的にローカルで扱う。文字起こしした依頼は選択AIへ送信する。AIの返答全文をログへ出さず、状態と返答文字数だけ表示する。
- 読み上げ中はマイクを閉じる。返答は文ごとに合成・再生し、1100文字を超えた分は読み上げずに省略を案内する。履歴には全文を残す。
- 音声はメモリ上のWAVとしてwhisper-server・スピーカーへ渡し、ファイルに保存しない。
- 機密を読み上げないようAIへ指示するが、秘密検出を保証する機能ではない。機密情報を含む依頼では利用者が入力と作業ディレクトリを管理する。

## Go版の検証記録（2026-10-03）

- 連結WAVから、ウェイクワード検出・発話収集・whisper-server（Metal）・Codex・VOICEVOX・CoreAudio再生・待機復帰まで成功。Codexの推論設定を変えて計4回実行した。応答時間は[応答時間](latency.md)を参照。
- VOICEVOXが停止した状態から自動起動し、話者の初期化と一往復の後に停止したことを確認。
- 人の声による一往復は未検証。スピーカーから実際に聞こえたかは、再生の終了を確認しただけで、耳では確かめていない。

## Python版の検証記録（2026-10-03）

- 単体テスト17件通過。AI・合成・再生失敗時の待機状態への復帰、無音時にAIを呼ばないことを含む。
- Codex CLIは本人の既存ChatGPTログインで短い日本語返答を取得。APIキー認証へ切り替える操作はしていない。
- VOICEVOX 0.25.2の実エンジンで日本語音声を合成し、speaker 0が四国めたんであることをAPIから確認。
- 上流の英語呼びかけと日本語合成依頼を連結したWAVから、ウェイクワード検出・発話収集・Whisper base・Codex・VOICEVOX・afplay再生・待機復帰まで成功。返答は22文字だった。
- この一往復はマイクから人が話したものではない。人の声による一往復、会話履歴、常駐、長時間の安定性は未検証。

参照：[Claude CLI](https://code.claude.com/docs/en/cli-reference)、[VOICEVOX配布物](https://github.com/VOICEVOX/voicevox_engine/releases/tag/0.25.2)、[VOICEVOX利用規約](https://voicevox.hiroshiba.jp/term/)。
