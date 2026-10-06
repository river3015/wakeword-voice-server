#!/bin/bash
# build/WakewordVoiceServer.app を ~/Applications に置き、ログイン時に起動するLaunchAgentとして登録する。
# 先に scripts/build-app.sh でアプリを作る。入れ直しにも使う。
# 使い方: scripts/install-launch-agent.sh [config.local.toml] / scripts/install-launch-agent.sh uninstall
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LABEL="io.github.river3015.wakeword-voice-server"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/Library/Logs/wakeword-voice-server.log"
BUILT_APP="$ROOT/build/WakewordVoiceServer.app"
APP="$HOME/Applications/WakewordVoiceServer.app"
BIN="$APP/Contents/MacOS/wakeword"
DOMAIN="gui/$(id -u)"

if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
  launchctl bootout "$DOMAIN/$LABEL"
  # bootout はプロセスの終了を待たずに戻る。子プロセスの片付けに最大5秒かかる
  for _ in {1..20}; do
    pgrep -f "$BIN" >/dev/null || break
    sleep 0.5
  done
fi

if [[ "${1:-}" == "uninstall" ]]; then
  rm -f "$PLIST"
  rm -rf "$APP"
  echo "登録を解除しました: ${LABEL}"
  exit 0
fi

CONFIG="$(cd "$(dirname "${1:-$ROOT/config.local.toml}")" && pwd)/$(basename "${1:-config.local.toml}")"
if [[ ! -f "$CONFIG" ]]; then
  echo "設定ファイルがありません: $CONFIG" >&2
  exit 1
fi
# 同じプロジェクトの重複起動はサーバー側でも拒否するが、ターミナルで動かしているものを先に止めてもらう
if pgrep -f "wakeword serve" >/dev/null; then
  echo "wakeword serve が動いています。先に止めてください（起動したターミナルでCtrl-C）。" >&2
  exit 1
fi
if [[ ! -d "$BUILT_APP" ]]; then
  echo "$BUILT_APP がありません。先に scripts/build-app.sh を実行してください。" >&2
  exit 1
fi
mkdir -p "$(dirname "$APP")"
rm -rf "$APP"
cp -R "$BUILT_APP" "$APP"

xml() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }
mkdir -p "$(dirname "$PLIST")"
cat >"$PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(printf %s "$BIN" | xml)</string>
    <string>serve</string>
    <string>--config</string>
    <string>$(printf %s "$CONFIG" | xml)</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$(printf %s "$(dirname "$CONFIG")" | xml)</string>
  <key>RunAtLoad</key>
  <true/>
  <!-- 異常終了したときだけ再起動する。「サーバー停止」での正常終了では再起動しない -->
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <!-- 状態と応答時間だけを出す。依頼や返答の内容は出さない -->
  <key>StandardOutPath</key>
  <string>$(printf %s "$LOG" | xml)</string>
  <key>StandardErrorPath</key>
  <string>$(printf %s "$LOG" | xml)</string>
</dict>
</plist>
PLIST

plutil -lint "$PLIST" >/dev/null
launchctl bootstrap "$DOMAIN" "$PLIST"
echo "登録しました: ${LABEL}（ログ: ${LOG}）"
