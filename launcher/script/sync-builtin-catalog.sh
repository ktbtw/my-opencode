#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE="$ROOT_DIR/backend/internal/api/builtin/catalog_v4.json"
TARGET="$ROOT_DIR/launcher/internal/app/builtin/catalog_v4.json"
TARGET_GZIP="$TARGET.gz"
TARGET_GZIP_TMP="$TARGET_GZIP.tmp"

if [ ! -f "$SOURCE" ]; then
  echo "缺少服务端内置能力目录: $SOURCE" >&2
  exit 1
fi

mkdir -p "$(dirname "$TARGET")"
cp "$SOURCE" "$TARGET"
cmp -s "$SOURCE" "$TARGET"
gzip -9 -n -c "$SOURCE" > "$TARGET_GZIP_TMP"
mv "$TARGET_GZIP_TMP" "$TARGET_GZIP"
gzip -t "$TARGET_GZIP"
cmp -s "$SOURCE" <(gzip -dc "$TARGET_GZIP")

echo "Launcher 内置能力目录已同步: ${TARGET}（嵌入资源: ${TARGET_GZIP}）"
