#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
VERSION_FILE="$ROOT_DIR/version.txt"
PUBLIC_BASE="${LAUNCHER_DEFAULT_PUBLIC_BASE:-https://www.xyapi.top/codex}"
WAILS_BIN="${WAILS_BIN:-$HOME/go/bin/wails}"

if [ ! -x "$WAILS_BIN" ]; then
  echo "缺少 Wails CLI，请先执行: go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" >&2
  exit 1
fi

VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
if [ -z "$VERSION" ]; then
  echo "launcher 版本号不能为空"
  exit 1
fi

python3 - "$ROOT_DIR/gui/wails.json" "$VERSION" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
version = sys.argv[2]
data = json.loads(path.read_text(encoding="utf-8"))
info = data.setdefault("info", {})
info["companyName"] = "Chat Codex"
info["productName"] = "码控"
info["productVersion"] = version
info["comments"] = "码控 launcher"
path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
PY

cd "$ROOT_DIR/gui"
"$WAILS_BIN" build \
  -platform darwin/arm64 \
  -clean \
  -ldflags "-s -w -X launcher/internal/version.Value=$VERSION -X launcher/internal/defaults.PublicBase=$PUBLIC_BASE" \
  -o chat-codex-launcher

echo "launcher gui mac build complete: version=$VERSION public_base=$PUBLIC_BASE"
