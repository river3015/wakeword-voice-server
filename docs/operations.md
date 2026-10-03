# 常駐運用と会話の使い方

音声：**VOICEVOX:四国めたん**。

## 起動と停止

依存・モデル・VOICEVOXの準備は[技術構成](technology-stack.md)と[一往復の手順](one-turn.md)を参照。config.local.tomlで本人の公式CLI実行ファイル、作業ディレクトリ、音声デバイスを指定する。

```sh
.venv/bin/python scripts/run-server.py
```

このコマンドは必要ならVOICEVOXを起動し、音声サーバーを継続運転する。同一プロジェクトの重複起動は拒否する。サーバーが動いている間だけcaffeinateの画面・システムスリープ防止を使う。恒久的なスリープ設定は変更しない。Macは電源に接続し、蓋を開けて使う。

停止は起動したターミナルでCtrl-C。自分で起動したVOICEVOXとサーバーを終了し、スリープ防止も解除する。既に別途起動していたVOICEVOXは停止しない。VOICEVOXを別管理する場合は`sh scripts/run-voicevox.sh`を使う。

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

自動起動は任意。plist生成は登録を行わない。

```sh
python3 scripts/create-launch-agent.py
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

マイク権限を確認してから登録する。ターミナル起動時の許可がLaunchAgentにも適用されるとは限らないため、登録後に実機確認する。サービスの状態ログは.runtimeへ保存する。VOICEVOXのアクセスログは保存しない。故障時に再起動するKeepAlive設定なので、停止はbootoutで行う。この環境ではplist生成とplutil検証だけを実行しており、登録は行っていない。

## 検証記録（2026-10-03）

- 単体テスト27件通過：履歴期限・上限、完全一致の制御語、履歴破棄、失敗後の次サイクル復帰、停止時の履歴破棄、受付反響の除外を含む。
- 3秒のマイク待ち受けを2回実施し、無音時にAIを呼ばず正常終了。
- Codexへ実際に2回依頼し、合言葉を次の依頼でも参照できることを確認。内容はログへ出していない。
- LaunchAgentのplist構文検証に成功。登録・ログイン後の自動起動は未検証。
- 人の声による呼びかけ、終了命令、離れた場所からの操作、長時間運転は未検証。

追加確認：2分（30秒×4回）の内蔵マイク待ち受けが誤起動なしで正常終了。OSのスリープ防止assertion登録と終了後の解除を確認。重複起動が拒否された。VOICEVOX停止状態から起動スクリプトがエンジンを起動し、短い待ち受け後に終了した。入力遅延対策後のマイク入力も3秒で38フレーム取得。実プロセスのタイムアウト終了と古い／欠落入力の拒否をテストへ追加した。
