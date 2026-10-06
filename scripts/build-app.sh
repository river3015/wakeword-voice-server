#!/bin/bash
# 常駐用の build/WakewordVoiceServer.app を作る。
set -euo pipefail

cd "$(dirname "$0")/.."
IDENTITY="Wakeword Local Signing"
APP="build/WakewordVoiceServer.app"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"
cp macos/Info.plist "$APP/Contents/Info.plist"
go build -o "$APP/Contents/MacOS/wakeword" ./cmd/wakeword

if security find-certificate -c "$IDENTITY" >/dev/null 2>&1; then
  codesign --force --sign "$IDENTITY" "$APP"
else
  echo "警告: 証明書「${IDENTITY}」がないため、仮の署名にします。再ビルドのたびにマイク許可がやり直しになります。" >&2
  echo "      scripts/make-cert.sh を一度実行してください。" >&2
  codesign --force --sign - "$APP"
fi
codesign --verify "$APP"
echo "作成しました: $APP"
