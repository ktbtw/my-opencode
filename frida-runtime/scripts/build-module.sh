#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
MODULE="$ROOT/module"
MANIFEST="$ROOT/artifacts/manifest.json"
STAGE="$ROOT/.build/module"
DIST="$ROOT/dist"
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

rm -rf "$STAGE"
mkdir -p "$STAGE"
cp -R "$MODULE/." "$STAGE/"
for abi in arm64-v8a armeabi-v7a x86 x86_64; do
  file=$(jq -r ".artifacts[\"$abi\"].file" "$MANIFEST")
  expected=$(jq -r ".artifacts[\"$abi\"].sha256" "$MANIFEST")
  source="$ROOT/artifacts/$file"
  [ -n "$expected" ] && [ "$expected" != "null" ] || { echo "missing SHA-256 for $abi" >&2; exit 1; }
  [ -f "$source" ] || { echo "missing artifact: $source" >&2; exit 1; }
  actual=$(shasum -a 256 "$source" | awk '{print $1}')
  [ "$actual" = "$expected" ] || { echo "SHA-256 mismatch for $abi" >&2; exit 1; }
  target="$STAGE/system/bin/frida-runtime/$abi"
  mkdir -p "$target"
  cp "$source" "$target/frida-server"
  chmod 0755 "$target/frida-server"
done
mkdir -p "$DIST"
rm -f "$DIST/frida-runtime-test.zip"
(cd "$STAGE" && zip -qr "$DIST/frida-runtime-test.zip" .)
echo "$DIST/frida-runtime-test.zip"
