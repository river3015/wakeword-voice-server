#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# エンジンのアクセスログには発話が含まれ得るため、標準出力・標準エラーを保存しない。
exec .vendor/voicevox/engine/macos-arm64/run --host 127.0.0.1 --port 50021 --cpu_num_threads 4 --disable_mutable_api >/dev/null 2>&1
