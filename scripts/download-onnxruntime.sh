#!/bin/sh
# ONNX Runtime（MIT）の公式配布物を .vendor に取得する。onnxruntime_go v1.36.0 は 1.29 以上の C API を使う。
# SHA-256 は 2026-10-03 に取得した配布物の値で、不一致なら展開しない。
set -eu
cd "$(dirname "$0")/.."
version=1.29.0
name=onnxruntime-osx-arm64-$version
sha256=d0706fc34f315d8c88639d0a8c81f2e09e815f282cabed3493c06a054352cf92
if [ -f ".vendor/onnxruntime/$name/lib/libonnxruntime.$version.dylib" ]; then
  echo "取得済み: .vendor/onnxruntime/$name"
  exit 0
fi
mkdir -p .vendor/onnxruntime
archive=.vendor/onnxruntime/$name.tgz
curl --fail --location -o "$archive" "https://github.com/microsoft/onnxruntime/releases/download/v$version/$name.tgz"
echo "$sha256  $archive" | shasum -a 256 -c -
tar -xzf "$archive" -C .vendor/onnxruntime
rm "$archive"
echo "取得しました: .vendor/onnxruntime/$name"
