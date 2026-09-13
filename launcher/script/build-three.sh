#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist-three"
ENTRY="$ROOT_DIR/cmd/launcher"
VERSION_FILE="$ROOT_DIR/version.txt"
WAILS_BIN="${WAILS_BIN:-$HOME/go/bin/wails}"

if [ ! -f "$VERSION_FILE" ]; then
  echo "缺少版本文件: $VERSION_FILE"
  exit 1
fi

VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
PUBLIC_BASE="${LAUNCHER_DEFAULT_PUBLIC_BASE:-https://www.xyapi.top/codex}"
if [ -z "$VERSION" ]; then
  echo "launcher 版本号不能为空"
  exit 1
fi

bash "$ROOT_DIR/script/sync-builtin-catalog.sh"

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

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

build_background_target() {
  local key="$1"
  local goos="$2"
  local goarch="$3"
  local bin_name="$4"
  local dir_name="launcher-${key}"
  local out_dir="$DIST_DIR/$dir_name/bin"

  mkdir -p "$out_dir"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -ldflags="-s -w -X launcher/internal/version.Value=$VERSION -X launcher/internal/defaults.PublicBase=$PUBLIC_BASE" -o "$out_dir/$bin_name" "$ENTRY"

  cat > "$DIST_DIR/$dir_name/package.json" <<EOF
{
  "name": "$dir_name",
  "version": "$VERSION",
  "os": ["$goos"],
  "cpu": ["$goarch"]
}
EOF
}

build_gui_target() {
  local key="$1"
  local platform="$2"
  local archive="$DIST_DIR/chat-codex-launcher-${key}.zip"
  local tunnel_helper="$DIST_DIR/launcher-${key}/bin/chat-codex-tunnel"

  if [ "$key" = "windows-x64" ]; then
    tunnel_helper="$DIST_DIR/launcher-${key}/bin/chat-codex-tunnel.exe"
  fi

  if [ ! -x "$WAILS_BIN" ]; then
    echo "缺少 Wails CLI，请先执行: go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" >&2
    exit 1
  fi

  (
    cd "$ROOT_DIR/gui"
    "$WAILS_BIN" build \
      -platform "$platform" \
      -clean \
      -ldflags "-s -w -X launcher/internal/version.Value=$VERSION -X launcher/internal/defaults.PublicBase=$PUBLIC_BASE" \
      -o chat-codex-launcher
  )

  case "$key" in
    darwin-arm64)
      local app_bundle="$ROOT_DIR/gui/build/bin/码控.app"
      if [ ! -d "$app_bundle" ]; then
        echo "缺少 macOS GUI 产物: $app_bundle" >&2
        exit 1
      fi
      cp "$tunnel_helper" "$app_bundle/Contents/MacOS/chat-codex-tunnel"
      chmod 755 "$app_bundle/Contents/MacOS/chat-codex-tunnel"
      codesign --force --deep --sign - "$app_bundle"
      COPYFILE_DISABLE=1 bsdtar --no-xattrs --no-mac-metadata \
        -C "$ROOT_DIR/gui/build/bin" \
        -a -cf "$archive" \
        "$(basename "$app_bundle")"
      ;;
    windows-x64)
      if [ ! -f "$ROOT_DIR/gui/build/bin/chat-codex-launcher" ]; then
        echo "缺少 Windows GUI 产物: $ROOT_DIR/gui/build/bin/chat-codex-launcher" >&2
        exit 1
      fi
      local out_dir="$DIST_DIR/.chat-codex-launcher-windows-x64"
      rm -rf "$out_dir"
      mkdir -p "$out_dir"
      cp "$ROOT_DIR/gui/build/bin/chat-codex-launcher" "$out_dir/chat-codex-launcher.exe"
      cp "$tunnel_helper" "$out_dir/chat-codex-tunnel.exe"
      (
        cd "$out_dir"
        zip -q -r "$archive" chat-codex-launcher.exe chat-codex-tunnel.exe
      )
      rm -rf "$out_dir"
      ;;
    *)
      echo "未知 GUI 平台: $key" >&2
      exit 1
      ;;
  esac
}

build_background_target "darwin-arm64" "darwin" "arm64" "launcher"
build_background_target "windows-x64" "windows" "amd64" "launcher.exe"

build_tunnel_target() {
  local key="$1"
  local goos="$2"
  local goarch="$3"
  local bin_name="$4"
  local out_dir="$DIST_DIR/tunnel"
  mkdir -p "$out_dir"
  (
    cd "$ROOT_DIR/../tunnel"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w" -o "$out_dir/chat-codex-tunnel-${key}${bin_name}" ./cmd/chat-codex-tunnel
  )
}

build_tunnel_target "darwin-arm64" "darwin" "arm64" ""
build_tunnel_target "darwin-x64" "darwin" "amd64" ""
build_tunnel_target "linux-x64" "linux" "amd64" ""
build_tunnel_target "linux-arm64" "linux" "arm64" ""
build_tunnel_target "windows-x64" "windows" "amd64" ".exe"
build_tunnel_target "windows-arm64" "windows" "arm64" ".exe"

cp "$DIST_DIR/tunnel/chat-codex-tunnel-darwin-arm64" "$DIST_DIR/launcher-darwin-arm64/bin/chat-codex-tunnel"
cp "$DIST_DIR/tunnel/chat-codex-tunnel-windows-x64.exe" "$DIST_DIR/launcher-windows-x64/bin/chat-codex-tunnel.exe"
chmod 755 "$DIST_DIR/launcher-darwin-arm64/bin/chat-codex-tunnel"

(
  cd "$DIST_DIR/launcher-darwin-arm64/bin"
  zip -q -r ../../launcher-darwin-arm64.zip *
)

(
  cd "$DIST_DIR/launcher-windows-x64/bin"
  zip -q -r ../../launcher-windows-x64.zip *
)

build_gui_target "darwin-arm64" "darwin/arm64"
build_gui_target "windows-x64" "windows/amd64"

echo "launcher build-three complete: version=$VERSION public_base=$PUBLIC_BASE with GUI downloads"
