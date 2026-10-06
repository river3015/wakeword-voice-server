#!/bin/bash
# アプリの署名に使う自己署名の証明書を、ログインキーチェーンに作る（最初に一度だけ）。
# 毎回同じ証明書で署名すると、再ビルドしてもmacOSのマイク許可が保たれる。
# 仮の署名（ad-hoc）はビルドのたびに変わり、許可がやり直しになる。
set -euo pipefail

NAME="Wakeword Local Signing"

if security find-certificate -c "$NAME" >/dev/null 2>&1; then
  echo "証明書は作成済みです: $NAME"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
pass="$(uuidgen)"

# システムのLibreSSLが書くPKCS#12は、security importで読める
/usr/bin/openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -subj "/CN=$NAME" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning" \
  -keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
/usr/bin/openssl pkcs12 -export -inkey "$tmp/key.pem" -in "$tmp/cert.pem" \
  -out "$tmp/id.p12" -passout "pass:$pass"
# -T で、codesign がキーチェーンの確認なしに鍵を使えるようにする
security import "$tmp/id.p12" -k "$HOME/Library/Keychains/login.keychain-db" \
  -P "$pass" -T /usr/bin/codesign >/dev/null

echo "証明書を作成しました: $NAME"
