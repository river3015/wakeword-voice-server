# OSS候補の利用条件と費用

調査日：2026-10-03。公式リポジトリ・公式文書を参照。個人のMacで本人が使う構成を主な想定とする。業務利用、配布、サービス提供は条件を分けて扱う。認識精度・速度・macOS上の動作は未検証。

## 判断

音声処理をローカルに置けば、その処理にクラウドの従量課金は不要。コードのライセンス、モデルのライセンス、生成音声の規約、AIサービスの契約はそれぞれ別に確認する。

前回候補のPorcupineを前提にせず、OSSの候補を優先する。openWakeWordも同梱モデルに非商用制限があるため、業務利用まで含めて無条件に採用できる候補とはしない。

## 音声処理の候補

| 部品 | コードの条件 | モデル・音声の条件 | ローカル実行のサービス利用料 | 判断 |
| --- | --- | --- | --- | --- |
| whisper.cpp + 公式Whisperモデル | MIT | 公式Whisperの重みもMIT | なし | 文字起こしの第一候補 |
| Silero VAD | MIT | 公式配布のVADもMITとして公開 | なし | 発話検出の第一候補 |
| openWakeWord | Apache-2.0 | 同梱学習済みモデルはCC BY-NC-SA 4.0 | なし | 個人の非商用試作なら候補。業務利用は保留 |
| sherpa-onnx | Apache-2.0 | 選択モデルごとに確認が必要 | なし | キーワード検出の代替候補。モデル選定未完了 |
| VOICEVOX ENGINE | LGPL v3／別ライセンスのデュアル | 音声ごとの利用規約が別途適用 | なし | 日本語読み上げの候補 |
| VOICEVOX CORE | ソースと現行ビルド成果物はMIT。0.16未満の配布バイナリは別ライセンス | モデルや生成音声までMITと判断しない | なし | ENGINEを使わず直接組み込む場合の候補 |
| Piperの現行開発先 | GPL-3.0 | 音声モデルごとのMODEL_CARDを確認 | なし | 今回は優先しない。公式音声一覧で日本語を確認できず |
| Open JTalk | Modified BSD | 選択する辞書・音声モデルは別途確認 | なし | 日本語TTSの代替候補。モデル監査は未完了 |
| macOSの音声合成 | OS付属機能。OSSとして扱わない | OSの利用条件に従う | 別途音声API課金を使わない構成が可能 | OSS優先方針では補助候補 |

Go版の実装で使うライブラリ：ONNX Runtime（MIT）、onnxruntime_go（MIT）、malgo／miniaudio（パブリックドメイン。miniaudioはMIT No Attributionも選べる）、BurntSushi/toml（MIT）。各リポジトリのLICENSEファイルで確認した。

「なし」はローカル実行に伴うベンダーへの定額・従量利用料が不要という意味。Mac、電気、ストレージ、モデルのダウンロード、開発・保守、独自学習の計算資源は別。

MIT／Apache-2.0でも配布時の著作権・ライセンス表示等は必要。GPL／LGPLを含む成果物を配布する場合は、組み込み方と変更範囲に応じたソース提供等の条件を確認する。単に個人のMacで実行することと、依存物を同梱して配布することを分ける。

openWakeWordは公式README上で英語のみ対応。英語の呼びかけと日本語の依頼を分ける構成は候補になるが、認識は未検証。独自学習しても学習データや合成音声の条件が消えるわけではない。外部のモデル生成サービスの料金・規約をOSS本体の条件と混同しない。

VOICEVOX Nemoはクレジット表記を条件に商用・非商用の生成音声利用が可能。ただし機械学習での利用等は禁止されている。ウェイクワード学習用の合成データには使わない。キャラクター音声を選ぶ場合は各キャラクターの規約も確認する。音声だけの利用でのクレジットの出し方は公式Q&Aと選択音声の規約に従って設計する。

### 音声処理の参照元

- [whisper.cpp LICENSE](https://github.com/ggml-org/whisper.cpp/blob/master/LICENSE)
- [Whisper README：コードとモデル重みのMIT表記](https://github.com/openai/whisper#license)
- [Silero VAD README](https://github.com/snakers4/silero-vad)
- [openWakeWord README：License／Language Support](https://github.com/dscripka/openWakeWord)
- [sherpa-onnx：モデルごとのライセンス確認](https://k2-fsa.github.io/sherpa/onnx/kws/apk-cn.html)
- [VOICEVOX ENGINE](https://github.com/VOICEVOX/voicevox_engine)
- [VOICEVOX CORE](https://github.com/VOICEVOX/voicevox_core#ライセンス)
- [VOICEVOX Q&A](https://voicevox.hiroshiba.jp/qa/)
- [VOICEVOX Nemo利用規約](https://voicevox.hiroshiba.jp/nemo/term/)
- [Piper](https://github.com/OHF-Voice/piper1-gpl)、[音声一覧とモデルの条件](https://github.com/OHF-Voice/piper1-gpl/blob/main/docs/VOICES.md)
- [Open JTalk](https://open-jtalk.sourceforge.net/)

## Porcupineの扱い

公式FAQは企業向けFree Trialと有料利用の構成を説明し、個人・非商用向けの専用無料／有料プランはないとしている。一方、SDK文書には無料AccessKeyの案内もある。無料キーを取得できることを、無期限・全用途で無料の根拠にはしない。今回のOSS優先構成では選定対象から外す。有料条件の確定や見積依頼は行っていない。

参照：[Picovoice公式FAQ](https://picovoice.ai/docs/faq/general/)、[Porcupine SDK](https://picovoice.ai/docs/porcupine/)。

## Claude／Codexの条件と料金

音声部分をOSSにしても、Claude／OpenAIのモデルサービスはOSSにはならず、その利用料は残る。Codex CLI本体はApache-2.0だが、接続先モデルの利用が無料になるわけではない。Claude CodeはAnthropicの利用規約に従う。

### 公式CLIを本人の契約で使う

- Claude Proは月払い20 USD、年払い200 USD、Maxは月100 USDから。Claude Codeを含むが利用上限がある。価格は税別の掲載額。
- Claude Codeの未改変バイナリを本人が公式フローで認証して使う方式を候補とする。第三者アプリでClaudeアカウントの認証情報やセッショントークンを収集・中継する方式は採用しない。
- 公式文書はプログラムからの`claude -p`利用を案内している。ただし今回の常駐音声ラッパーに対する個別の承認を得たわけではない。本人によるCLI操作の自動化として構成する判断と、第三者への製品・サービス提供を分ける。後者には商用条件や認証方法の追加確認が必要。
- Claude Codeでは`ANTHROPIC_API_KEY`が設定されていると、サブスクリプションではなくAPI課金になると公式ヘルプに記載されている。
- Codex CLIを含むChatGPT Plusは月20 USD、Proは月100 USDから。Free／Goの案内はデスクトップ利用が中心なので、CLIの無料利用を前提にしない。`codex exec`は公式のスクリプト実行方式。
- 既に対象プランを契約しており、含まれる利用量の範囲で使うなら、新たなAIサービス契約を追加せずに試せる可能性がある。本人のプラン・認証・追加クレジット設定は未確認。APIキー、追加利用・クレジットは別費用になる。

参照：[Claude料金](https://claude.com/pricing)、[Claude CodeのPro／Max利用](https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan)、[Claude Code法務・認証条件](https://code.claude.com/docs/en/legal-and-compliance)、[プログラム実行](https://code.claude.com/docs/en/headless)、[Codex料金](https://learn.chatgpt.com/docs/pricing)、[Codex非対話実行](https://learn.chatgpt.com/docs/non-interactive-mode)、[Codex CLIライセンス](https://github.com/openai/codex/blob/main/docs/license.md)。

### APIキーで使う

サブスクリプションの利用量とは別に従量課金。音声認識と読み上げをローカルにすれば、音声APIの利用料は不要で、AIへのテキスト・ツール処理に課金される。

例としてClaude Sonnet 5.5の標準API掲載額は入力100万トークンあたり2 USD、出力100万トークンあたり10 USD。入力2,000・出力500トークンだけの一往復なら、計算上0.009 USD、同条件を1,000回なら9 USD。これは直接API利用の単純な例で、Claude Code一回の実行費用の見積もりではない。履歴、リポジトリ、システム指示、推論、ツール呼び出しや反復で総量は増える。キャッシュ・追加ツール料金等はこの計算に含めない。

CodexのAPI利用も選択モデルのAPI料金による。サブスクリプション内の利用量や追加クレジット料金とAPIトークン料金を混同しない。モデル未選定のため固定の一回料金は出さない。

参照：[Claude API料金](https://platform.claude.com/docs/en/about-claude/pricing)、[CodexのAPI利用と課金](https://learn.chatgpt.com/docs/pricing)。

## 現時点の構成案と残る確認

これは選定案であり、採用確定ではない。

1. 文字起こしはwhisper.cpp＋公式Whisper、発話検出はSilero VADを優先する。
2. ウェイクワードは個人の非商用試作ならopenWakeWordを候補にする。業務利用まで含めるなら、モデルの条件を確認したsherpa-onnx、またはWhisperでローカル認識した文字列から呼びかけを判定する試作を検討する。後者は専用ウェイクワード検出より計算量が増える可能性があり、動作検証が必要。
3. 日本語読み上げはVOICEVOXを候補とし、音声・モデルとクレジットの条件を確定する。条件を抑えたい場合はOpen JTalkの具体的な音声・辞書を追加調査する。
4. AIはまず本人の公式CLIを呼び、認証方式を明示する。待ち受けだけでAIを呼ばず、依頼が確定したときだけ呼ぶ。

残る作業は、具体的なモデル・バージョンと依存物のライセンス確認、個人／業務での利用範囲の確定、本人のAI契約と課金経路の確認。その後にmacOSでインストールと一往復の動作検証を行う。パッケージのインストール、契約、購入、AIの有料呼び出しは今回行っていない。
