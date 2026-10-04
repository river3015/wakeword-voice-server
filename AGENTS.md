# Working agreements

共通の作業方針は `~/.codex/AGENTS.md`（`~/.claude/CLAUDE.md` から同じファイルを参照）に従う。ここにはこのリポジトリ固有のことだけを書く。

## このプロジェクトの方針

- 利用目的・要件・データの扱いを判断するときはREADME.mdを参照する。初期の対象OSはmacOS、連携先の候補はClaudeとCodex。
- 未選定の技術や未検証のCLI仕様を、確定事項として記載しない。
- まずはウェイクワードから音声での返答までの一往復を実装し、実機で確認する。
- 待ち受け音声の外部送信や音声・会話履歴の永続保存は、READMEの方針とユーザーの合意に従う。
- AI連携時の作業ディレクトリと実行権限を明確にし、既存の承認機構を無断で無効化しない。
- 依存ライブラリのソース（`.vendor/`）とモデル（`.models/`）は、該当ライブラリを調べるとき以外は探索しない。

## 必要なときに読む文書

作業開始時にまとめて読まない。該当する作業のときだけ開く。

| 作業 | 文書 |
| --- | --- |
| 構成・依存の導入 | `docs/technology-stack.md` |
| マイク入力・発話収集 | `docs/capture.md` |
| AI連携・読み上げ | `docs/one-turn.md` |
| 常駐運用 | `docs/operations.md` |
| 応答時間の計測・改善 | `docs/latency.md` |
| フェーズの進行・完成判定 | `docs/implementation-phases.md`、`docs/completion-audit.md` |
| OSSのライセンス・費用 | `docs/licensing-and-costs.md` |
