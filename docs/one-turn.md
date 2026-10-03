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

`7z`は別途必要。エンジンは127.0.0.1:50021だけで待ち受け、辞書・設定変更APIは無効にする。実行時にmacOSのユーザーデータ領域にエンジン設定用ディレクトリが作成される。サーバーのアクセスログに発話が含まれ得るため、起動スクリプトは出力を破棄する。起動したターミナルでCtrl-Cを押すと停止する。

別ターミナルで以下を実行する。

```sh
cp config.example.toml config.local.toml
# ai_executableとworkdirを実環境に合わせて編集する。
.venv/bin/python -m wakeword_voice once --config config.local.toml
```

呼びかけ→日本語依頼→文字起こし→AI→VOICEVOX→スピーカー再生を一往復して終了する。テスト音声で試す場合は`--source-wav /absolute/path/combined.wav`、呼びかけなしの日本語WAVなら`--request-wav /absolute/path/request.wav`を指定する。前者・後者ともAIの利用量を消費する。

## AIの権限・データ

- providerはcodexまたはclaude。本人が公式CLIへログインして使う。API認証の場合は別途API課金。
- workdirは明示したディレクトリ。Codexの既定はread-only。workspace-writeを設定すればそのディレクトリ内の編集が可能。承認が必要な操作を自動で許可するフラグは付けない。承認が必要な作業は拒否される場合がある。
- Codexはユーザー設定を読み込まず、`--ephemeral`でネイティブの会話セッションを保存しない。CLIの認証は既存のものを使う。現在の対話チャットを再利用する仕組みではない。
- Claudeのread-only構成はRead／Glob／Grepのみを許可し、MCPツールを除外する。workspace-write指定時も権限モードはdefaultで、承認を迂回しない。Claude CLIは現Macに未導入で、実接続は未検証。
- 依頼文はコマンド引数へ入れず、標準入力で渡す。CLIの出力は一時ファイルへ置き、成功・失敗時に削除する。処理がタイムアウトしたら子プロセスのグループを終了する。
- 音声と返答は一時的にローカルで扱う。文字起こしした依頼は選択AIへ送信する。AIの返答全文をログへ出さず、状態と返答文字数だけ表示する。
- 読み上げ中はマイクを閉じる。1200文字を超える返答は読み上げを短縮する。
- 機密を読み上げないようAIへ指示するが、秘密検出を保証する機能ではない。機密情報を含む依頼では利用者が入力と作業ディレクトリを管理する。

## 検証記録（2026-10-03）

- 単体テスト17件通過。AI・合成・再生失敗時の待機状態への復帰、無音時にAIを呼ばないことを含む。
- Codex CLIは本人の既存ChatGPTログインで短い日本語返答を取得。APIキー認証へ切り替える操作はしていない。
- VOICEVOX 0.25.2の実エンジンで日本語音声を合成し、speaker 0が四国めたんであることをAPIから確認。
- 上流の英語呼びかけと日本語合成依頼を連結したWAVから、ウェイクワード検出・発話収集・Whisper base・Codex・VOICEVOX・afplay再生・待機復帰まで成功。返答は22文字だった。
- この一往復はマイクから人が話したものではない。人の声による一往復、会話履歴、常駐、長時間の安定性は未検証。

参照：[Claude CLI](https://code.claude.com/docs/en/cli-reference)、[VOICEVOX配布物](https://github.com/VOICEVOX/voicevox_engine/releases/tag/0.25.2)、[VOICEVOX利用規約](https://voicevox.hiroshiba.jp/term/)。
