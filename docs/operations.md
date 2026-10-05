# 常駐運用と会話の使い方

音声：**VOICEVOX:四国めたん**。

## 起動と停止

依存・モデル・VOICEVOXの準備は[技術構成](technology-stack.md)と[一往復の手順](one-turn.md)を参照。config.local.tomlでCodex CLIの実行ファイル、作業ディレクトリ、音声デバイスを指定する。

```sh
go build -o bin/wakeword ./cmd/wakeword
bin/wakeword serve --config config.local.toml
```

whisper-serverとVOICEVOXは、待ち受け中は止めておく。ウェイクワードを検出した時点で並行して起動し、話している間に準備（無音の文字起こしと短い文の合成）を終える。最後に使ってから`service_idle_timeout`（既定300秒）使わなければ止める。待ち受け中のメモリは約0.4GB減る（2026-10-05の計測で、whisper-server約210MB、VOICEVOX約220MB）。`service_idle_timeout = 0`にすると、従来どおり起動時に両方を準備して常駐させる。既に起動しているものはそのまま使い、停止もしない。同一プロジェクトの重複起動は拒否する。`prevent_sleep = true`の場合、サーバーが動いている間だけcaffeinateで画面・システムのスリープを防ぐ。恒久的なスリープ設定は変更しない。Macは電源に接続し、蓋を開けて使う。

停止は起動したターミナルでCtrl-C（SIGTERMでも可）。自分で起動した子プロセスを終了し、スリープ防止も解除する。whisper-serverやVOICEVOXが途中で終了した場合は、次に使うときに起動し直す。起動に失敗した場合は`error`状態になり、次の呼びかけで再び起動を試みる。caffeinateが終了した場合はサーバーも異常終了し、LaunchAgentで運用する場合はlaunchdが再起動する。

状態はJSON行で標準エラーへ、一往復の結果と`timings_ms`は標準出力へ出す。依頼・返答の内容は出さない。子プロセスの出力は発話を含み得るため保存しない。

## 会話

1. 「Hey Mycroft」と呼びかける。
2. 受付音の後に、日本語で依頼する。受付から既定320msは反響対策のため捨てる。
3. 読み上げが終わった後、もう一度ウェイクワードで追加依頼できる。
4. 最後の成功応答から5分間、最大6往復・16000文字の履歴をメモリ上に保持する。
5. ウェイクワードの後に「会話終了」または「キャンセル」とだけ話すと履歴を破棄する。「サーバー停止」とだけ話すとサーバーを停止する。

制御語を含む説明文は制御命令として扱わない。再起動、停止、履歴期限切れでも履歴を破棄する。現在の設計は、毎回ウェイクワードで追加依頼を受け付ける。読み上げやAI処理の途中に音声で割り込む機能はない。処理中に止めたい場合はCtrl-Cを使う。

## 障害時の動作

認識・AI・読み上げ・マイク入力のエラーは`error`状態を表示し、2〜10秒の待ち時間後に新しい呼びかけを待つ。失敗した依頼を自動再送しない。無音の待ち受けや発話が短すぎる場合はAIを呼ばない。音声や返答内容は標準ログへ出さない。

ネットワークのない状態では、ローカル検出と音声認識は使えるが、Claude／Codexの応答は取得できない。VOICEVOXはlocalhostだけを使い、HTTPプロキシやリダイレクトで外部へ音声合成文を転送しない。

Codexのread-onlyでは編集の依頼は実行できない。自宅の特定リポジトリを音声で編集する場合は、workdirを明示しsandboxをworkspace-writeにする。承認の迂回や権限拡大のフラグは付けないため、承認が必要な操作は拒否される場合がある。実環境デプロイや購入を音声認識だけで許可しない。

## 任意のログイン時起動

自動起動は任意。plistの生成だけでは登録されない。`go run`ではなく、ビルドした実行ファイルから生成する。

```sh
bin/wakeword launch-agent --config config.local.toml
plutil -lint .runtime/local.wakeword-voice-server.plist
mkdir -p "$HOME/Library/LaunchAgents"
cp .runtime/local.wakeword-voice-server.plist "$HOME/Library/LaunchAgents/"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/local.wakeword-voice-server.plist"
launchctl print "gui/$(id -u)/local.wakeword-voice-server"
```

停止・登録解除：

```sh
launchctl bootout "gui/$(id -u)/local.wakeword-voice-server"
rm "$HOME/Library/LaunchAgents/local.wakeword-voice-server.plist"
```

マイク権限を確認してから登録する。plistには実行ファイルと設定ファイルの絶対パスが入るので、移動した場合は再生成する。実行ファイルを再ビルドした後もマイク許可が維持されるかは未確認。ターミナル起動時の許可がLaunchAgentにも適用されるとは限らないため、登録後に実機確認する。サービスの状態ログは.runtimeへ保存する。VOICEVOXのアクセスログは保存しない。故障時に再起動するKeepAlive設定なので、停止はbootoutで行う。この環境ではplist生成とplutil検証だけを実行しており、登録は行っていない。

## Go版の検証記録（2026-10-03）

- 内蔵マイクの3秒待ち受けを2回実施。無音時にAIを呼ばず、2回とも約3.2秒で正常終了した。ビルド直後の初回だけ、最初の待ち受けが約40秒かかった。新しい実行ファイルに対するmacOSのマイク許可の確認とみられる（推測）。
- whisper-serverとVOICEVOXを自動起動し、終了後に子プロセスやcaffeinateが残らないことを確認。
- 重複起動の拒否、子プロセスの準備待ち・起動直後の終了検出、失敗後の待ち受け復帰は単体テストで確認。
- LaunchAgentの登録、人の声、長時間運転は未検証。

## Python版の検証記録（2026-10-03）

- 単体テスト27件通過：履歴期限・上限、完全一致の制御語、履歴破棄、失敗後の次サイクル復帰、停止時の履歴破棄、受付反響の除外を含む。
- 3秒のマイク待ち受けを2回実施し、無音時にAIを呼ばず正常終了。
- Codexへ実際に2回依頼し、合言葉を次の依頼でも参照できることを確認。内容はログへ出していない。
- LaunchAgentのplist構文検証に成功。登録・ログイン後の自動起動は未検証。
- 人の声による呼びかけ、終了命令、離れた場所からの操作、長時間運転は未検証。

追加確認：2分（30秒×4回）の内蔵マイク待ち受けが誤起動なしで正常終了。OSのスリープ防止assertion登録と終了後の解除を確認。重複起動が拒否された。VOICEVOX停止状態から起動スクリプトがエンジンを起動し、短い待ち受け後に終了した。入力遅延対策後のマイク入力も3秒で38フレーム取得。実プロセスのタイムアウト終了と古い／欠落入力の拒否をテストへ追加した。

障害復帰の追加確認：既存の日本語合成WAVを使い、最初のサイクルだけ存在しないAI実行ファイルを指定した。`error`状態と待機後、次サイクルで実Codex・VOICEVOX・再生が成功し、停止時にメモリ履歴が破棄された。マイク録音は行っていない。これはCLI起動失敗からの復帰検証であり、ネットワーク断やマイク切断の実検証ではない。
