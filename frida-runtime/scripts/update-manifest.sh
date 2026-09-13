#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
MANIFEST="$ROOT/artifacts/manifest.json"
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
command -v shasum >/dev/null 2>&1 || { echo "shasum is required" >&2; exit 1; }

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
cp "$MANIFEST" "$tmp"
for abi in arm64-v8a armeabi-v7a x86 x86_64; do
  file="frida-server-$abi"
  path="$ROOT/artifacts/$file"
  [ -f "$path" ] || continue
  sha=$(shasum -a 256 "$path" | awk '{print $1}')
  jq --arg abi "$abi" --arg file "$file" --arg sha "$sha" \
    '.artifacts[$abi].file = $file | .artifacts[$abi].sha256 = $sha' "$tmp" > "$tmp.next"
  mv "$tmp.next" "$tmp"
done
mv "$tmp" "$MANIFEST"
trap - EXIT
