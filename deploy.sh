#!/bin/bash
# chat-codex 部署脚本
# 用法: ./deploy.sh [all|--deploy-built-only|--backend-only|--frontend-only|--mobile-app-only|--android-only|--admin-web-only|--cli-only|--launcher-only|--runtime-only]

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEFAULT_SERVER="root@www.xyapi.top"
DEFAULT_PUBLIC_BASE="https://www.xyapi.top/codex"
DEFAULT_SSH_KEY="$ROOT_DIR/root.pem"
BACKEND_DIR="/opt/chat-codex/backend"
WEB_DIR="/opt/chat-codex/web"
ADMIN_WEB_DIR="/opt/chat-codex/admin-web"
CODEX_WEB_DIR="/opt/chat-codex/web-codex"
APK_DIR="/opt/chat-codex/apk"
CLI_DIR="/opt/chat-codex/apk/cli"
LAUNCHER_DIR="/opt/chat-codex/apk/launcher"
RUNTIME_DIR="/opt/chat-codex/apk/runtime"
DOMAIN_NGINX_CONF="${CHAT_CODEX_DOMAIN_NGINX_CONF:-/etc/nginx/conf.d/verify.conf}"
IP_NGINX_CONF="/www/server/panel/vhost/nginx/chat-codex.conf"
BINARY="chat-codex-server"
SERVICE="chat-codex-backend"
BACKEND_PORT="${CHAT_CODEX_BACKEND_PORT:-19081}"
MODE="all"
DRY_RUN=0
CLI_ROOT="$ROOT_DIR/my-opencode2/packages/opencode"
CLI_PKG_JSON="$CLI_ROOT/package.json"
APP_CHANGELOG_FILE="${CHAT_CODEX_APP_CHANGELOG_FILE:-$ROOT_DIR/flutter_app/release-notes.txt}"
APP_RELEASE_HISTORY_FILE="${CHAT_CODEX_APP_RELEASE_HISTORY_FILE:-$ROOT_DIR/flutter_app/release-history.json}"
CLI_CHANGELOG_FILE="${CHAT_CODEX_CLI_CHANGELOG_FILE:-$ROOT_DIR/my-opencode2/opencode-release-notes.txt}"

usage() {
  echo "用法: ./deploy.sh [all|--deploy-built-only|--backend-only|--frontend-only|--mobile-app-only|--android-only|--admin-web-only|--cli-only|--launcher-only|--runtime-only]"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    all|--deploy-built-only|--backend-only|--frontend-only|--mobile-app-only|--android-only|--admin-web-only|--cli-only|--launcher-only|--runtime-only)
      MODE="$1"
      ;;
    --dry-run)
      DRY_RUN=1
      ;;
    *)
      echo "不支持的参数: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift
done

case "$MODE" in
  all|--deploy-built-only|--backend-only|--frontend-only|--mobile-app-only|--android-only|--admin-web-only|--cli-only|--launcher-only|--runtime-only) ;;
  *)
    echo "不支持的参数: $MODE"
    usage
    exit 1
    ;;
esac

SERVER="${CHAT_CODEX_SERVER:-$DEFAULT_SERVER}"
PUBLIC_BASE="${CHAT_CODEX_PUBLIC_BASE:-$DEFAULT_PUBLIC_BASE}"
PUBLIC_BASE="${PUBLIC_BASE%/}"
SSH_KEY="${CHAT_CODEX_SSH_KEY:-$DEFAULT_SSH_KEY}"
SSH_ATTEMPTS="${CHAT_CODEX_SSH_ATTEMPTS:-5}"
SSH_CONTROL_HASH="$(printf '%s' "$SERVER|$SSH_KEY" | shasum -a 256 | awk '{print substr($1, 1, 16)}')"
SSH_CONTROL_PATH="${CHAT_CODEX_SSH_CONTROL_PATH:-/tmp/chatcodex-ssh-${SSH_CONTROL_HASH}.sock}"

public_base_to_relay() {
  local base="$1"
  if [[ "$base" == https://* ]]; then
    echo "wss://${base#https://}/ws/device"
    return
  fi
  if [[ "$base" == http://* ]]; then
    echo "ws://${base#http://}/ws/device"
    return
  fi
  echo "${base%/}/ws/device"
}

DEFAULT_RELAY_URL="$(public_base_to_relay "$PUBLIC_BASE")"

BUILD_BACKEND=0
BUILD_FRONTEND=0
BUILD_MOBILE_APP=0
ANDROID_ONLY=0
BUILD_ADMIN_WEB=0
BUILD_CLI=0
BUILD_LAUNCHER=0
DEPLOY_BACKEND=0
DEPLOY_FRONTEND=0
DEPLOY_MOBILE_APP=0
DEPLOY_ADMIN_WEB=0
DEPLOY_CLI=0
DEPLOY_LAUNCHER=0
DEPLOY_RUNTIME=0
VERIFY_FRONTEND=0
VERIFY_MOBILE_APP=0
VERIFY_ADMIN_WEB=0
VERIFY_CLI=0
VERIFY_LAUNCHER=0
VERIFY_RUNTIME=0

case "$MODE" in
  all)
    BUILD_BACKEND=1
    BUILD_FRONTEND=1
    BUILD_MOBILE_APP=1
    BUILD_ADMIN_WEB=1
    BUILD_CLI=1
    BUILD_LAUNCHER=1
    DEPLOY_BACKEND=1
    DEPLOY_FRONTEND=1
    DEPLOY_MOBILE_APP=1
    DEPLOY_ADMIN_WEB=1
    DEPLOY_CLI=1
    DEPLOY_LAUNCHER=1
    DEPLOY_RUNTIME=1
    VERIFY_FRONTEND=1
    VERIFY_MOBILE_APP=1
    VERIFY_ADMIN_WEB=1
    VERIFY_CLI=1
    VERIFY_LAUNCHER=1
    VERIFY_RUNTIME=1
    ;;
  --deploy-built-only)
    DEPLOY_BACKEND=1
    DEPLOY_FRONTEND=1
    DEPLOY_MOBILE_APP=1
    DEPLOY_ADMIN_WEB=1
    DEPLOY_CLI=1
    DEPLOY_LAUNCHER=1
    DEPLOY_RUNTIME=1
    VERIFY_FRONTEND=1
    VERIFY_MOBILE_APP=1
    VERIFY_ADMIN_WEB=1
    VERIFY_CLI=1
    VERIFY_LAUNCHER=1
    VERIFY_RUNTIME=1
    ;;
  --backend-only)
    BUILD_BACKEND=1
    DEPLOY_BACKEND=1
    ;;
  --frontend-only)
    BUILD_FRONTEND=1
    DEPLOY_FRONTEND=1
    VERIFY_FRONTEND=1
    ;;
  --mobile-app-only)
    BUILD_MOBILE_APP=1
    DEPLOY_MOBILE_APP=1
    VERIFY_MOBILE_APP=1
    ;;
  --android-only)
    BUILD_MOBILE_APP=1
    ANDROID_ONLY=1
    DEPLOY_MOBILE_APP=1
    VERIFY_MOBILE_APP=1
    ;;
  --admin-web-only)
    BUILD_ADMIN_WEB=1
    DEPLOY_ADMIN_WEB=1
    VERIFY_ADMIN_WEB=1
    ;;
  --cli-only)
    BUILD_CLI=1
    DEPLOY_CLI=1
    VERIFY_CLI=1
    ;;
  --launcher-only)
    BUILD_LAUNCHER=1
    DEPLOY_LAUNCHER=1
    VERIFY_LAUNCHER=1
    ;;
  --runtime-only)
    DEPLOY_RUNTIME=1
    VERIFY_RUNTIME=1
    ;;
esac

if { [ "$BUILD_MOBILE_APP" -eq 1 ] || [ "$DEPLOY_MOBILE_APP" -eq 1 ]; } && [ ! -f "$APP_CHANGELOG_FILE" ]; then
  echo "缺少前端更新说明文件: $APP_CHANGELOG_FILE"
  exit 1
fi
if { [ "$BUILD_MOBILE_APP" -eq 1 ] || [ "$DEPLOY_MOBILE_APP" -eq 1 ]; } && [ ! -f "$APP_RELEASE_HISTORY_FILE" ]; then
  echo "缺少前端版本历史文件: $APP_RELEASE_HISTORY_FILE"
  exit 1
fi

VERSION_LINE="$(grep '^version:' "$ROOT_DIR/flutter_app/pubspec.yaml" | awk '{print $2}')"
VERSION_NAME="${VERSION_LINE%%+*}"
VERSION_CODE="${VERSION_LINE##*+}"
LAUNCHER_VERSION=""
CLI_VERSION=""

if [ "$BUILD_LAUNCHER" -eq 1 ] || [ "$DEPLOY_LAUNCHER" -eq 1 ]; then
  LAUNCHER_VERSION="$(tr -d '[:space:]' < "$ROOT_DIR/launcher/version.txt")"
fi
if [ "$BUILD_CLI" -eq 1 ] || [ "$DEPLOY_CLI" -eq 1 ]; then
  CLI_VERSION="$(grep '"version"' "$CLI_PKG_JSON" | head -n 1 | sed -E 's/.*"version": "([^"]+)".*/\1/')"
fi

VERSION_JSON_LOCAL="/tmp/chatcodex_version_${VERSION_NAME}_${VERSION_CODE}.json"
WEB_TAR_LOCAL="/tmp/chatcodex_web_${VERSION_NAME}_${VERSION_CODE}.tgz"
ADMIN_WEB_TAR_LOCAL="/tmp/chatcodex_admin_web_${VERSION_NAME}_${VERSION_CODE}.tgz"
WEB_CODEX_TAR_LOCAL="/tmp/chatcodex_web_codex_${VERSION_NAME}_${VERSION_CODE}.tgz"
APK_LOCAL="$ROOT_DIR/flutter_app/build/app/outputs/flutter-apk/app-release.apk"
WINDOWS_INSTALLER_FILENAME="chat-codex-windows-x64-setup.exe"
WINDOWS_INSTALLER_LOCAL="${CHAT_CODEX_WINDOWS_INSTALLER:-$ROOT_DIR/flutter_app/build/windows/installer/ChatCodex-${VERSION_NAME}-windows-x64-setup.exe}"
MACOS_PACKAGE_FILENAME="chat-codex-darwin-arm64.dmg"
MACOS_PACKAGE_LOCAL="${CHAT_CODEX_MACOS_PACKAGE:-$ROOT_DIR/flutter_app/build/macos/installer/ChatCodex-${VERSION_NAME}-darwin-arm64.dmg}"
CLI_DIST_LOCAL="$CLI_ROOT/dist"
CLI_PACKAGE_LOCAL="$CLI_ROOT/dist-three"
CLI_WINDOWS_LOCAL="$CLI_PACKAGE_LOCAL/opencode-windows-x64.zip"
CLI_DARWIN_LOCAL="$CLI_PACKAGE_LOCAL/opencode-darwin-arm64.zip"
CLI_LINUX_LOCAL="$CLI_PACKAGE_LOCAL/opencode-linux-x64.tar.gz"
CLI_MODELS_JSON="${CHAT_CODEX_MODELS_DEV_API_JSON:-$ROOT_DIR/.chatcodex-artifacts/models-dev-api.json}"
LAUNCHER_WINDOWS_LOCAL="$ROOT_DIR/launcher/dist-three/launcher-windows-x64.zip"
LAUNCHER_DARWIN_LOCAL="$ROOT_DIR/launcher/dist-three/launcher-darwin-arm64.zip"
LAUNCHER_GUI_WINDOWS_LOCAL="$ROOT_DIR/launcher/dist-three/chat-codex-launcher-windows-x64.zip"
LAUNCHER_GUI_DARWIN_LOCAL="$ROOT_DIR/launcher/dist-three/chat-codex-launcher-darwin-arm64.zip"
TUNNEL_DARWIN_ARM64_LOCAL="$ROOT_DIR/launcher/dist-three/tunnel/chat-codex-tunnel-darwin-arm64"
TUNNEL_DARWIN_X64_LOCAL="$ROOT_DIR/launcher/dist-three/tunnel/chat-codex-tunnel-darwin-x64"
TUNNEL_LINUX_X64_LOCAL="$ROOT_DIR/launcher/dist-three/tunnel/chat-codex-tunnel-linux-x64"
TUNNEL_LINUX_ARM64_LOCAL="$ROOT_DIR/launcher/dist-three/tunnel/chat-codex-tunnel-linux-arm64"
TUNNEL_WINDOWS_X64_LOCAL="$ROOT_DIR/launcher/dist-three/tunnel/chat-codex-tunnel-windows-x64.exe"
TUNNEL_WINDOWS_ARM64_LOCAL="$ROOT_DIR/launcher/dist-three/tunnel/chat-codex-tunnel-windows-arm64.exe"
RUNTIME_UV_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/uv"
UV_WINDOWS_LOCAL="$RUNTIME_UV_DIR/uv-x86_64-pc-windows-msvc.zip"
UV_DARWIN_LOCAL="$RUNTIME_UV_DIR/uv-aarch64-apple-darwin.tar.gz"
UV_LINUX_LOCAL="$RUNTIME_UV_DIR/uv-x86_64-unknown-linux-gnu.tar.gz"
RUNTIME_NODE_VERSION="${CHAT_CODEX_RUNTIME_NODE_VERSION:-v22.16.0}"
RUNTIME_NODE_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/node"
NODE_WINDOWS_LOCAL="$RUNTIME_NODE_DIR/node-${RUNTIME_NODE_VERSION}-win-x64.zip"
NODE_DARWIN_LOCAL="$RUNTIME_NODE_DIR/node-${RUNTIME_NODE_VERSION}-darwin-arm64.tar.gz"
NODE_LINUX_LOCAL="$RUNTIME_NODE_DIR/node-${RUNTIME_NODE_VERSION}-linux-x64.tar.gz"
RUNTIME_GO_VERSION="${CHAT_CODEX_RUNTIME_GO_VERSION:-1.25.4}"
RUNTIME_GO_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/go"
GO_WINDOWS_LOCAL="$RUNTIME_GO_DIR/go${RUNTIME_GO_VERSION}.windows-amd64.zip"
GO_DARWIN_ARM64_LOCAL="$RUNTIME_GO_DIR/go${RUNTIME_GO_VERSION}.darwin-arm64.tar.gz"
GO_DARWIN_AMD64_LOCAL="$RUNTIME_GO_DIR/go${RUNTIME_GO_VERSION}.darwin-amd64.tar.gz"
GO_LINUX_AMD64_LOCAL="$RUNTIME_GO_DIR/go${RUNTIME_GO_VERSION}.linux-amd64.tar.gz"
RUNTIME_JAVA_VERSION="${CHAT_CODEX_RUNTIME_JAVA_VERSION:-21}"
RUNTIME_JAVA_TEMURIN_VERSION="${CHAT_CODEX_RUNTIME_JAVA_TEMURIN_VERSION:-21.0.11_10}"
RUNTIME_JAVA_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/java"
JAVA_WINDOWS_FILENAME="OpenJDK21U-jdk_x64_windows_hotspot_${RUNTIME_JAVA_TEMURIN_VERSION}.zip"
JAVA_DARWIN_FILENAME="OpenJDK21U-jdk_aarch64_mac_hotspot_${RUNTIME_JAVA_TEMURIN_VERSION}.tar.gz"
JAVA_LINUX_FILENAME="OpenJDK21U-jdk_x64_linux_hotspot_${RUNTIME_JAVA_TEMURIN_VERSION}.tar.gz"
JAVA_WINDOWS_SHA256="${CHAT_CODEX_RUNTIME_JAVA_WINDOWS_SHA256:-d3625e7cadf23787ea540229544b6e2ab494b3b54da1801879e583e1dfee0a64}"
JAVA_DARWIN_SHA256="${CHAT_CODEX_RUNTIME_JAVA_DARWIN_SHA256:-6ebcf221c9b41507b14c098e93c6ead6440b8d9bd154f8ec666c4c73abbdb201}"
JAVA_LINUX_SHA256="${CHAT_CODEX_RUNTIME_JAVA_LINUX_SHA256:-4b2220e232a97997b436ca6ab15cbf70171ecff52958a46159dfa5a8c44ca4de}"
JAVA_WINDOWS_LOCAL="$RUNTIME_JAVA_DIR/$JAVA_WINDOWS_FILENAME"
JAVA_DARWIN_LOCAL="$RUNTIME_JAVA_DIR/$JAVA_DARWIN_FILENAME"
JAVA_LINUX_LOCAL="$RUNTIME_JAVA_DIR/$JAVA_LINUX_FILENAME"
RUNTIME_GHIDRA_VERSION="${CHAT_CODEX_RUNTIME_GHIDRA_VERSION:-12.1.2}"
GHIDRA_FILENAME="ghidra_${RUNTIME_GHIDRA_VERSION}_PUBLIC_20260605.zip"
GHIDRA_SHA256="${CHAT_CODEX_RUNTIME_GHIDRA_SHA256:-b62e81a0390618466c019c60d8c2f796ced2509c4c1aea4a37644a77272cf99d}"
RUNTIME_GHIDRA_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/ghidra"
GHIDRA_LOCAL="$RUNTIME_GHIDRA_DIR/$GHIDRA_FILENAME"
RUNTIME_IDA_SOURCE_DIR="${CHAT_CODEX_RUNTIME_IDA_SOURCE_DIR:-$HOME/Downloads/百度网盘}"
IDA_WINDOWS_FILENAME="bgspa-ida92-win.zip"
IDA_DARWIN_AMD64_FILENAME="bgspa-ida92-x64mac.zip"
IDA_DARWIN_ARM64_FILENAME="bgspa-ida92-armmac.zip"
IDA_WINDOWS_SHA256="4c67ce1bf4befa22478287085505927f2639efd3decf43d720dfd15daf5b20ec"
IDA_DARWIN_AMD64_SHA256="3b3c63012bc99908bc6681ed16a39da7192c22543536f2b281ccfedaa855fb08"
IDA_DARWIN_ARM64_SHA256="2ed18b10bebad66c105df6880bd19297ffd11fe0b26116c8763675ad9adc4c94"
IDA_WINDOWS_LOCAL="$RUNTIME_IDA_SOURCE_DIR/$IDA_WINDOWS_FILENAME"
IDA_DARWIN_AMD64_LOCAL="$RUNTIME_IDA_SOURCE_DIR/$IDA_DARWIN_AMD64_FILENAME"
IDA_DARWIN_ARM64_LOCAL="$RUNTIME_IDA_SOURCE_DIR/$IDA_DARWIN_ARM64_FILENAME"
RUNTIME_MCP_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/mcp"
RUNTIME_TOOL_DIR="$ROOT_DIR/.chatcodex-artifacts/runtime/tool"
JADX_TOOL_FILENAME="jadx-1.5.5.zip"
JADX_TOOL_SHA256="38a5766d3c8170c41566b4b13ea0ede2430e3008421af4927235c2880234d51a"
JADX_TOOL_LOCAL="$RUNTIME_TOOL_DIR/$JADX_TOOL_FILENAME"
APKTOOL_TOOL_FILENAME="apktool_3.0.2.jar"
APKTOOL_TOOL_SHA256="eee4669a704a14e0623407e6701b0b91887e61e1e4049cb7a82833e14ae8b5fd"
APKTOOL_TOOL_LOCAL="$RUNTIME_TOOL_DIR/$APKTOOL_TOOL_FILENAME"
MCP_JADX_LOCAL="$RUNTIME_MCP_DIR/jadx-mcp-server.tar.gz"
MCP_APKTOOL_LOCAL="$RUNTIME_MCP_DIR/apktool-mcp-server.tar.gz"
MCP_FRIDA_LOCAL="$RUNTIME_MCP_DIR/frida-analykit.tar.gz"
MCP_IDA_PRO_LOCAL="$RUNTIME_MCP_DIR/ida-pro-mcp.tar.gz"
MCP_IDA_PRO_WHEEL_LOCAL="$RUNTIME_MCP_DIR/ida_pro_mcp-2.0.0-py3-none-any.whl"
MCP_IDAPRO_WHEEL_LOCAL="$RUNTIME_MCP_DIR/idapro-0.0.10-py3-none-any.whl"
MCP_TOMLI_W_WHEEL_LOCAL="$RUNTIME_MCP_DIR/tomli_w-1.2.0-py3-none-any.whl"
MCP_MIRA_LOCAL="$RUNTIME_MCP_DIR/ios-vwww-droid-mira.tar.gz"
MCP_WIREMCP_LOCAL="$RUNTIME_MCP_DIR/wiremcp.tar.gz"
MCP_BINARY_LOCAL="$RUNTIME_MCP_DIR/binary-mcp.tar.gz"
MCP_JSREVERSER_LOCAL="$RUNTIME_MCP_DIR/web-noone-jsreverser-mcp.tar.gz"
MCP_TRIAGE_LOCAL="$RUNTIME_MCP_DIR/windows-eversinc33-triagemcp.tar.gz"
MCP_VOLATILITY_LOCAL="$RUNTIME_MCP_DIR/windows-gaffx-volatility-mcp.tar.gz"
MCP_X64DBG_PY_LOCAL="$RUNTIME_MCP_DIR/windows-wasdubya-x64dbgmcp.tar.gz"
CLI_VERSION_JSON_LOCAL="/tmp/chatcodex_cli_version_${CLI_VERSION}.json"
LAUNCHER_VERSION_JSON_LOCAL="/tmp/chatcodex_launcher_version_${LAUNCHER_VERSION}.json"

log_section() {
  echo
  echo "=== $1 ==="
}

remote_ssh() {
  local ssh_opts=(
    -o ServerAliveInterval=30
    -o ServerAliveCountMax=4
    -o ConnectTimeout=30
    -o ControlMaster=auto
    -o ControlPersist=10m
    -o ControlPath="$SSH_CONTROL_PATH"
  )
  local attempt=1
  local status=0
  while true; do
    if [ -n "$SSH_KEY" ]; then
      ssh "${ssh_opts[@]}" -i "$SSH_KEY" "$SERVER" "$@" && return 0
    else
      ssh "${ssh_opts[@]}" "$SERVER" "$@" && return 0
    fi
    status=$?
    if [ "$attempt" -ge "$SSH_ATTEMPTS" ]; then
      return "$status"
    fi
    echo "ssh failed, retry $attempt/$SSH_ATTEMPTS in $((attempt * 3))s: exit=$status" >&2
    sleep $((attempt * 3))
    attempt=$((attempt + 1))
  done
}

remote_scp() {
  local src="$1"
  local dst="$2"
  local scp_opts=(
    -o ServerAliveInterval=30
    -o ServerAliveCountMax=4
    -o ConnectTimeout=30
    -o ControlMaster=auto
    -o ControlPersist=10m
    -o ControlPath="$SSH_CONTROL_PATH"
  )
  local attempt=1
  local status=0
  while true; do
    if [ -n "$SSH_KEY" ]; then
      scp "${scp_opts[@]}" -i "$SSH_KEY" "$src" "$SERVER:$dst" && return 0
    else
      scp "${scp_opts[@]}" "$src" "$SERVER:$dst" && return 0
    fi
    status=$?
    if [ "$attempt" -ge "$SSH_ATTEMPTS" ]; then
      return "$status"
    fi
    echo "scp failed, retry $attempt/$SSH_ATTEMPTS in $((attempt * 3))s: exit=$status" >&2
    sleep $((attempt * 3))
    attempt=$((attempt + 1))
  done
}

close_ssh_control_master() {
  if [ -S "$SSH_CONTROL_PATH" ]; then
    if [ -n "$SSH_KEY" ]; then
      ssh -S "$SSH_CONTROL_PATH" -O exit -i "$SSH_KEY" "$SERVER" >/dev/null 2>&1 || true
    else
      ssh -S "$SSH_CONTROL_PATH" -O exit "$SERVER" >/dev/null 2>&1 || true
    fi
  fi
}

trap close_ssh_control_master EXIT

upload_file() {
  local src="$1"
  local dst="$2"
  local tmp="${dst}.tmp.$$"
  local local_size
  local remote_size

  remote_ssh "rm -f '$tmp'" || true
  remote_scp "$src" "$tmp"
  local_size="$(stat -f '%z' "$src" 2>/dev/null || stat -c '%s' "$src")"
  remote_size="$(remote_ssh "stat -c '%s' '$tmp'")"
  if [ "$local_size" != "$remote_size" ]; then
    echo "上传文件大小不一致: $src -> $dst local=$local_size remote=$remote_size" >&2
    remote_ssh "rm -f '$tmp'" || true
    exit 1
  fi
  remote_ssh "mv -f '$tmp' '$dst'"
  echo "uploaded $src -> $dst ($local_size bytes)"
}

file_sha256() {
  shasum -a 256 "$1" | awk '{print $1}'
}

require_file() {
  local path="$1"
  local label="${2:-产物}"
  if [ ! -s "$path" ]; then
    echo "缺少${label}: $path" >&2
    exit 1
  fi
}

apk_version_line() {
  local apk="$1"
  local package_line
  local apk_version_name
  local apk_version_code

  package_line="$(aapt dump badging "$apk" | sed -n '1p')"
  apk_version_name="$(printf '%s\n' "$package_line" | sed -n "s/.*versionName='\([^']*\)'.*/\1/p")"
  apk_version_code="$(printf '%s\n' "$package_line" | sed -n "s/.*versionCode='\([^']*\)'.*/\1/p")"
  if [ -z "$apk_version_name" ] || [ -z "$apk_version_code" ]; then
    echo "无法读取 Android APK 版本: $package_line" >&2
    return 1
  fi
  echo "$apk_version_name+$apk_version_code"
}

remote_file_sha256() {
  local path="$1"
  remote_ssh "if [ -f '$path' ]; then sha256sum '$path' | cut -d ' ' -f 1; fi" 2>/dev/null || true
}

remote_file_size() {
  local path="$1"
  remote_ssh "if [ -f '$path' ]; then stat -c '%s' '$path'; fi" 2>/dev/null || true
}

upload_file_if_changed() {
  local src="$1"
  local dst="$2"
  local local_hash
  local remote_hash
  local_hash="$(file_sha256 "$src")"
  remote_hash="$(remote_file_sha256 "$dst")"
  if [ -n "$remote_hash" ] && [ "$local_hash" = "$remote_hash" ]; then
    echo "skip same-sha256 $src -> $dst ($local_hash)"
    return
  fi
  upload_file "$src" "$dst"
}

download_if_missing() {
  local url="$1"
  local dst="$2"
  local expected_sha256="${3:-}"
  local max_time="${4:-3600}"
  local tmp="${dst}.download.$$"
  local actual_sha256
  if [ -s "$dst" ]; then
    if [ -z "$expected_sha256" ] || [ "$(file_sha256 "$dst")" = "$expected_sha256" ]; then
      return
    fi
    echo "本地制品校验失败，重新下载: $dst" >&2
  fi
  mkdir -p "$(dirname "$dst")"
  echo "download $url -> $dst"
  rm -f "$tmp"
  if ! curl -L --fail --connect-timeout 15 --max-time "$max_time" --retry 3 --retry-all-errors -o "$tmp" "$url"; then
    rm -f "$tmp"
    return 1
  fi
  if [ -n "$expected_sha256" ]; then
    actual_sha256="$(file_sha256 "$tmp")"
    if [ "$actual_sha256" != "$expected_sha256" ]; then
      rm -f "$tmp"
      echo "下载 SHA-256 验证失败: expected=$expected_sha256 actual=$actual_sha256 url=$url" >&2
      return 1
    fi
  fi
  mv -f "$tmp" "$dst"
}

verify_remote_file_sha256() {
  local path="$1"
  local expected="$2"
  local actual
  actual="$(remote_file_sha256 "$path")"
  if [ "$actual" != "$expected" ]; then
    echo "远程制品 SHA-256 验证失败: expected=$expected actual=$actual path=$path" >&2
    exit 1
  fi
  echo "remote-sha256-ok sha256=$actual path=$path"
}

verify_download_headers() {
  local url="$1"
  local max_time="${2:-30}"
  verify_download_range "$url" "$max_time"
}

verify_download_range() {
  local url="$1"
  local max_time="${2:-180}"
  local tmp
  local headers
  local status
  local size
  local content_range
  local range_end
  local total_size
  local expected_size
  tmp="$(mktemp)"
  headers="$(mktemp)"
  status="$(curl --http1.1 -L -sS --connect-timeout 10 --max-time "$max_time" \
    -H 'Range: bytes=0-1023' -D "$headers" -o "$tmp" -w '%{http_code}' "$url")"
  size="$(stat -f '%z' "$tmp" 2>/dev/null || stat -c '%s' "$tmp")"
  content_range="$(tr -d '\r' < "$headers" | sed -nE 's/^[Cc]ontent-[Rr]ange:[[:space:]]*bytes[[:space:]]+0-([0-9]+)\/([0-9]+)[[:space:]]*$/\1 \2/p' | tail -1)"
  range_end="${content_range%% *}"
  total_size="${content_range##* }"
  expected_size=1024
  if [[ "$total_size" =~ ^[0-9]+$ ]] && [ "$total_size" -lt "$expected_size" ]; then
    expected_size="$total_size"
  fi
  if [ "$status" != "206" ] || ! [[ "$range_end" =~ ^[0-9]+$ ]] || [ "$size" != "$expected_size" ] || [ "$range_end" -ne $((expected_size - 1)) ]; then
    echo "HTTP Range 验证失败: status=$status bytes=$size url=$url" >&2
    sed -n '1,20p' "$headers" >&2
    rm -f "$tmp" "$headers"
    exit 1
  fi
  echo "range-ok status=$status bytes=$size url=$url"
  rm -f "$tmp" "$headers"
}

verify_download_sha256() {
  local url="$1"
  local expected="$2"
  local max_time="${3:-300}"
  local tmp
  local actual
  tmp="$(mktemp)"
  curl --http1.1 -L --fail -sS --connect-timeout 10 --max-time "$max_time" -o "$tmp" "$url"
  actual="$(file_sha256 "$tmp")"
  rm -f "$tmp"
  if [ "$actual" != "$expected" ]; then
    echo "下载 SHA-256 验证失败: expected=$expected actual=$actual url=$url" >&2
    exit 1
  fi
  echo "sha256-ok sha256=$actual url=$url"
}

ensure_uv_artifacts() {
  local base="${CHAT_CODEX_UV_MIRROR:-$PUBLIC_BASE/api/runtime/uv/download?artifact=}"
  download_if_missing "${base}uv-x86_64-pc-windows-msvc.zip" "$UV_WINDOWS_LOCAL"
  download_if_missing "${base}uv-aarch64-apple-darwin.tar.gz" "$UV_DARWIN_LOCAL"
  download_if_missing "${base}uv-x86_64-unknown-linux-gnu.tar.gz" "$UV_LINUX_LOCAL"
}

ensure_node_artifacts() {
	local base="${CHAT_CODEX_NODE_MIRROR:-https://cdn.npmmirror.com/binaries/node/$RUNTIME_NODE_VERSION}"
	download_if_missing "$base/node-${RUNTIME_NODE_VERSION}-win-x64.zip" "$NODE_WINDOWS_LOCAL"
	download_if_missing "$base/node-${RUNTIME_NODE_VERSION}-darwin-arm64.tar.gz" "$NODE_DARWIN_LOCAL"
	download_if_missing "$base/node-${RUNTIME_NODE_VERSION}-linux-x64.tar.gz" "$NODE_LINUX_LOCAL"
}

ensure_go_artifacts() {
  local base="${CHAT_CODEX_GO_MIRROR:-https://mirrors.aliyun.com/golang}"
  download_if_missing "$base/go${RUNTIME_GO_VERSION}.windows-amd64.zip" "$GO_WINDOWS_LOCAL"
  download_if_missing "$base/go${RUNTIME_GO_VERSION}.darwin-arm64.tar.gz" "$GO_DARWIN_ARM64_LOCAL"
  download_if_missing "$base/go${RUNTIME_GO_VERSION}.darwin-amd64.tar.gz" "$GO_DARWIN_AMD64_LOCAL"
  download_if_missing "$base/go${RUNTIME_GO_VERSION}.linux-amd64.tar.gz" "$GO_LINUX_AMD64_LOCAL"
}

ensure_java_artifacts() {
  local base="${CHAT_CODEX_JAVA_MIRROR:-}"
  if [ -z "$base" ]; then
    base="https://github.com/adoptium/temurin21-binaries/releases/download/jdk-${RUNTIME_JAVA_TEMURIN_VERSION/_/+}/"
    download_if_missing "${base}$JAVA_WINDOWS_FILENAME" "$JAVA_WINDOWS_LOCAL" "$JAVA_WINDOWS_SHA256"
    download_if_missing "${base}$JAVA_DARWIN_FILENAME" "$JAVA_DARWIN_LOCAL" "$JAVA_DARWIN_SHA256"
    download_if_missing "${base}$JAVA_LINUX_FILENAME" "$JAVA_LINUX_LOCAL" "$JAVA_LINUX_SHA256"
    return
  fi
  if [[ "$base" == *"artifact=" ]]; then
    download_if_missing "${base}$JAVA_WINDOWS_FILENAME" "$JAVA_WINDOWS_LOCAL" "$JAVA_WINDOWS_SHA256"
    download_if_missing "${base}$JAVA_DARWIN_FILENAME" "$JAVA_DARWIN_LOCAL" "$JAVA_DARWIN_SHA256"
    download_if_missing "${base}$JAVA_LINUX_FILENAME" "$JAVA_LINUX_LOCAL" "$JAVA_LINUX_SHA256"
    return
  fi
  download_if_missing "$base/x64/windows/$JAVA_WINDOWS_FILENAME" "$JAVA_WINDOWS_LOCAL" "$JAVA_WINDOWS_SHA256"
  download_if_missing "$base/aarch64/mac/$JAVA_DARWIN_FILENAME" "$JAVA_DARWIN_LOCAL" "$JAVA_DARWIN_SHA256"
  download_if_missing "$base/x64/linux/$JAVA_LINUX_FILENAME" "$JAVA_LINUX_LOCAL" "$JAVA_LINUX_SHA256"
}

ensure_ghidra_artifact() {
  local url="${CHAT_CODEX_GHIDRA_URL:-https://github.com/NationalSecurityAgency/ghidra/releases/download/Ghidra_${RUNTIME_GHIDRA_VERSION}_build/$GHIDRA_FILENAME}"
  if [ -s "$GHIDRA_LOCAL" ] && [ "$(file_sha256 "$GHIDRA_LOCAL")" = "$GHIDRA_SHA256" ]; then
    return
  fi
  rm -f "$GHIDRA_LOCAL"
  mkdir -p "$(dirname "$GHIDRA_LOCAL")"
  echo "download $url -> $GHIDRA_LOCAL"
  curl -L --fail --connect-timeout 15 --max-time 3600 --retry 3 -o "$GHIDRA_LOCAL" "$url"
  if [ "$(file_sha256 "$GHIDRA_LOCAL")" != "$GHIDRA_SHA256" ]; then
    echo "Ghidra SHA-256 校验失败: $GHIDRA_LOCAL" >&2
    exit 1
  fi
}

ensure_ida_artifacts() {
  local path
  local expected
  for path in "$IDA_WINDOWS_LOCAL" "$IDA_DARWIN_AMD64_LOCAL" "$IDA_DARWIN_ARM64_LOCAL"; do
    require_file "$path" "IDA 9.2 安装包"
    case "$(basename "$path")" in
      "$IDA_WINDOWS_FILENAME") expected="$IDA_WINDOWS_SHA256" ;;
      "$IDA_DARWIN_AMD64_FILENAME") expected="$IDA_DARWIN_AMD64_SHA256" ;;
      "$IDA_DARWIN_ARM64_FILENAME") expected="$IDA_DARWIN_ARM64_SHA256" ;;
      *) echo "未知 IDA 安装包: $path" >&2; exit 1 ;;
    esac
    if [ "$(file_sha256 "$path")" != "$expected" ]; then
      echo "IDA 安装包 SHA-256 校验失败: $path" >&2
      exit 1
    fi
  done
}

ensure_android_tool_artifacts() {
  local jadx_url="${CHAT_CODEX_JADX_URL:-https://github.com/skylot/jadx/releases/download/v1.5.5/$JADX_TOOL_FILENAME}"
  local apktool_url="${CHAT_CODEX_APKTOOL_URL:-https://github.com/iBotPeaches/Apktool/releases/download/v3.0.2/$APKTOOL_TOOL_FILENAME}"
  download_if_missing "$jadx_url" "$JADX_TOOL_LOCAL" "$JADX_TOOL_SHA256"
  download_if_missing "$apktool_url" "$APKTOOL_TOOL_LOCAL" "$APKTOOL_TOOL_SHA256"
}

archive_local_mcp_source() {
  local src="$1"
  local dst="$2"
  local label="$3"
  if [ ! -d "$src" ]; then
    echo "缺少 MCP 源码目录: $src" >&2
    exit 1
  fi
  mkdir -p "$(dirname "$dst")"
  echo "package $label -> $dst"
  COPYFILE_DISABLE=1 bsdtar --no-xattrs --no-mac-metadata \
    --exclude .git \
    --exclude .venv \
    --exclude __pycache__ \
    --exclude '*/__pycache__' \
    --exclude '.pytest_cache' \
    --exclude '.mypy_cache' \
    -C "$src" -czf "$dst" .
}

download_existing_mcp_artifact() {
  local artifact="$1"
  local dst="$2"
  local url="$PUBLIC_BASE/api/runtime/mcp/download/$artifact"
  mkdir -p "$(dirname "$dst")"
  echo "download existing hosted MCP artifact $url -> $dst"
  curl -L --fail --connect-timeout 10 --max-time 240 --retry 2 -o "$dst" "$url"
}

ensure_mcp_artifact_from_local_or_hosted() {
  local label="$1"
  local src="$2"
  local dst="$3"
  local artifact="$4"
  if [ -s "$dst" ]; then
    return
  fi
  if [ -d "$src" ] && [ -n "$(find "$src" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    archive_local_mcp_source "$src" "$dst" "$label"
    return
  fi
  if download_existing_mcp_artifact "$artifact" "$dst"; then
    return
  fi
  echo "缺少 MCP 包 $artifact。请先放置 $dst，或提供本地源码目录 $src，或确认旧服务器已托管该包。" >&2
  exit 1
}

ensure_mcp_artifacts() {
  local mcp_root="${CHAT_CODEX_LOCAL_MCP_ROOT:-$HOME/.my-opencode-launcher/mcps}"
  ensure_mcp_artifact_from_local_or_hosted "jadx-mcp-server" "$mcp_root/android-jadx-mcp-server" "$MCP_JADX_LOCAL" "jadx-mcp-server.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "apktool-mcp-server" "$mcp_root/android-apktool-mcp-server" "$MCP_APKTOOL_LOCAL" "apktool-mcp-server.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "frida-analykit" "$mcp_root/android-frida-analykit" "$MCP_FRIDA_LOCAL" "frida-analykit.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "ida-pro-mcp" "$mcp_root/ida-idalib-mcp" "$MCP_IDA_PRO_LOCAL" "ida-pro-mcp.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "Mira" "$mcp_root/ios-vwww-droid-mira" "$MCP_MIRA_LOCAL" "ios-vwww-droid-mira.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "WireMCP" "$mcp_root/web-0xkoda-wiremcp" "$MCP_WIREMCP_LOCAL" "wiremcp.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "binary-mcp" "$mcp_root/windows-sarks0-binary-mcp" "$MCP_BINARY_LOCAL" "binary-mcp.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "JSReverser-MCP" "$mcp_root/web-noone-jsreverser-mcp" "$MCP_JSREVERSER_LOCAL" "web-noone-jsreverser-mcp.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "TriageMCP" "$mcp_root/windows-eversinc33-triagemcp" "$MCP_TRIAGE_LOCAL" "windows-eversinc33-triagemcp.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "volatility-mcp" "$mcp_root/windows-gaffx-volatility-mcp" "$MCP_VOLATILITY_LOCAL" "windows-gaffx-volatility-mcp.tar.gz"
  ensure_mcp_artifact_from_local_or_hosted "x64dbgMCP" "$mcp_root/windows-wasdubya-x64dbgmcp" "$MCP_X64DBG_PY_LOCAL" "windows-wasdubya-x64dbgmcp.tar.gz"
}

write_frontend_version_json() {
  python3 - "$VERSION_NAME" "$VERSION_CODE" "$PUBLIC_BASE" "$APP_CHANGELOG_FILE" "$APP_RELEASE_HISTORY_FILE" "$APK_SHA256" "$WINDOWS_INSTALLER_SHA256" "$MACOS_PACKAGE_SHA256" "$VERSION_JSON_LOCAL" <<'PY'
import json
import pathlib
import sys

version, code, base, notes_file, history_file, apk_sha, windows_sha, macos_sha, output = sys.argv[1:]
notes = pathlib.Path(notes_file).read_text(encoding="utf-8").strip()
try:
    history = json.loads(pathlib.Path(history_file).read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError):
    history = []
if isinstance(history, dict):
    history = history.get("releases", [])
if not isinstance(history, list):
    history = []
current = {
    "version": version,
    "version_code": int(code),
    "released_at": "",
    "items": [line[2:].strip() if line.startswith("- ") else line for line in notes.splitlines() if line.strip()],
}
releases = [current] + [item for item in history if isinstance(item, dict) and not (
    str(item.get("version", "")).strip() == version and int(item.get("version_code", 0) or 0) == int(code)
)]
pathlib.Path(history_file).write_text(
    json.dumps(releases, ensure_ascii=False, indent=2) + "\n",
    encoding="utf-8",
)
payload = {
    "version": version,
    "version_code": int(code),
    "download_url": f"{base}/api/app/download?v={code}",
    "changelog": notes,
    "releases": releases,
    "downloads": [item for item in [
        {
            "platform": "android",
            "filename": f"chat-codex-{version}.apk",
            "url": f"{base}/api/app/download?v={code}",
            "sha256": apk_sha,
            "version_code": int(code),
        },
        {
            "platform": "windows-x64",
            "filename": "chat-codex-windows-x64-setup.exe",
            "url": f"{base}/api/app/download?artifact=chat-codex-windows-x64-setup.exe&v={code}",
            "sha256": windows_sha,
            "version_code": int(code),
        },
        {
            "platform": "darwin-arm64",
            "filename": "chat-codex-darwin-arm64.dmg",
            "url": f"{base}/api/app/download?artifact=chat-codex-darwin-arm64.dmg&v={code}",
            "sha256": macos_sha,
            "version_code": int(code),
        },
    ] if item["sha256"]],
}
pathlib.Path(output).write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
PY
}

build_macos_package() {
  if [ "$(uname -s)" != "Darwin" ]; then
    echo "macOS 安装镜像必须在 macOS 构建机生成" >&2
    exit 1
  fi
  local app_path="$ROOT_DIR/flutter_app/build/macos/Build/Products/Release/Chat Codex.app"
  local executable="$app_path/Contents/MacOS/Chat Codex"
  local staging_dir="$ROOT_DIR/flutter_app/build/macos/dmg-staging"
  cd "$ROOT_DIR/flutter_app"
  dart run flutter_launcher_icons
  flutter build macos --release --dart-define=CHAT_CODEX_DEFAULT_BASE_URL="$PUBLIC_BASE"
  require_file "$executable" "macOS App"
  if ! lipo -archs "$executable" | tr ' ' '\n' | grep -qx arm64; then
    echo "macOS App 不包含 arm64 架构" >&2
    exit 1
  fi
  rm -rf "$staging_dir"
  mkdir -p "$staging_dir" "$(dirname "$MACOS_PACKAGE_LOCAL")"
  cp -R "$app_path" "$staging_dir/"
  ln -s /Applications "$staging_dir/Applications"
  rm -f "$MACOS_PACKAGE_LOCAL"
  hdiutil create -volname "Chat Codex" -srcfolder "$staging_dir" -ov -format UDZO "$MACOS_PACKAGE_LOCAL"
  rm -rf "$staging_dir"
  require_file "$MACOS_PACKAGE_LOCAL" "macOS DMG"
}

write_cli_version_json() {
  python3 - "$CLI_VERSION" "$PUBLIC_BASE" "$CLI_CHANGELOG_FILE" "$CLI_WINDOWS_SHA256" "$CLI_DARWIN_SHA256" "$CLI_LINUX_SHA256" "$CLI_VERSION_JSON_LOCAL" <<'PY'
import json
import pathlib
import sys

version, base, notes_file, windows_sha, darwin_sha, linux_sha, output = sys.argv[1:]
notes = pathlib.Path(notes_file).read_text(encoding="utf-8").strip()
payload = {
    "version": version,
    "channel": "latest",
    "changelog": notes,
    "downloads": [
        {
            "platform": "windows-x64",
            "filename": "opencode-windows-x64.zip",
            "url": f"{base}/api/app/download?artifact=opencode-windows-x64.zip",
            "sha256": windows_sha,
        },
        {
            "platform": "darwin-arm64",
            "filename": "opencode-darwin-arm64.zip",
            "url": f"{base}/api/app/download?artifact=opencode-darwin-arm64.zip",
            "sha256": darwin_sha,
        },
        {
            "platform": "linux-x64",
            "filename": "opencode-linux-x64.tar.gz",
            "url": f"{base}/api/app/download?artifact=opencode-linux-x64.tar.gz",
            "sha256": linux_sha,
        },
    ],
}
pathlib.Path(output).write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
PY
}

package_cli_artifacts() {
  rm -rf "$CLI_PACKAGE_LOCAL"
  mkdir -p "$CLI_PACKAGE_LOCAL"
  (
    cd "$CLI_DIST_LOCAL/opencode-windows-x64/bin"
    zip -q -r "$CLI_WINDOWS_LOCAL" .
  )
  (
    cd "$CLI_DIST_LOCAL/opencode-darwin-arm64/bin"
    zip -q -r "$CLI_DARWIN_LOCAL" .
  )
  (
    cd "$CLI_DIST_LOCAL/opencode-linux-x64/bin"
    COPYFILE_DISABLE=1 tar -czf "$CLI_LINUX_LOCAL" .
  )
}

sign_cli_macos_binary() {
  local binary="$CLI_DIST_LOCAL/opencode-darwin-arm64/bin/opencode"
  if [ "$(uname -s)" != "Darwin" ]; then
    echo "跳过 macOS CLI 签名: 当前构建机不是 macOS"
    return
  fi
  if ! command -v codesign >/dev/null 2>&1; then
    echo "缺少 codesign，不能生成可启动的 macOS CLI 包" >&2
    exit 1
  fi
  codesign --force --deep --sign - "$binary"
  codesign --verify --verbose=2 "$binary"
}

write_launcher_version_json() {
  python3 - "$LAUNCHER_VERSION" "$PUBLIC_BASE" "$LAUNCHER_WINDOWS_SHA256" "$LAUNCHER_DARWIN_SHA256" "$LAUNCHER_GUI_WINDOWS_SHA256" "$LAUNCHER_GUI_DARWIN_SHA256" "$TUNNEL_DARWIN_ARM64_SHA256" "$TUNNEL_DARWIN_X64_SHA256" "$TUNNEL_LINUX_X64_SHA256" "$TUNNEL_LINUX_ARM64_SHA256" "$TUNNEL_WINDOWS_X64_SHA256" "$TUNNEL_WINDOWS_ARM64_SHA256" "$LAUNCHER_VERSION_JSON_LOCAL" <<'PY'
import json
import pathlib
import sys

(
    version, base, windows_sha, darwin_sha, gui_windows_sha, gui_darwin_sha,
    tunnel_darwin_arm64_sha, tunnel_darwin_x64_sha, tunnel_linux_x64_sha,
    tunnel_linux_arm64_sha, tunnel_windows_x64_sha, tunnel_windows_arm64_sha,
    output,
) = sys.argv[1:]
payload = {
    "version": version,
    "channel": "latest",
    # 用 chr(10) 拼换行，避免在多层引号里写转义序列出错。
    "changelog": chr(10).join([
        "- 修复手填的模型上下文窗口被运行时上报值覆盖，设置后界面仍显示旧值的问题",
        "- 上传中断后重选同一文件不再整份重传，只补传缺失分块",
        "- 自动清理中断上传残留的临时目录",
    ]),
    "downloads": [
        {
            "platform": "windows-x64",
            "filename": "launcher-windows-x64.zip",
            "url": f"{base}/api/launcher/download?artifact=launcher-windows-x64.zip",
            "sha256": windows_sha,
        },
        {
            "platform": "darwin-arm64",
            "filename": "launcher-darwin-arm64.zip",
            "url": f"{base}/api/launcher/download?artifact=launcher-darwin-arm64.zip",
            "sha256": darwin_sha,
        },
        {
            "platform": "windows-x64",
            "filename": "chat-codex-launcher-windows-x64.zip",
            "url": f"{base}/api/launcher/download?artifact=chat-codex-launcher-windows-x64.zip",
            "sha256": gui_windows_sha,
        },
        {
            "platform": "darwin-arm64",
            "filename": "chat-codex-launcher-darwin-arm64.zip",
            "url": f"{base}/api/launcher/download?artifact=chat-codex-launcher-darwin-arm64.zip",
            "sha256": gui_darwin_sha,
        },
        *[
            {
                "platform": platform,
                "filename": filename,
                "url": f"{base}/api/launcher/download?artifact={filename}",
                "sha256": sha256,
            }
            for platform, filename, sha256 in [
                ("tunnel-darwin-arm64", "chat-codex-tunnel-darwin-arm64", tunnel_darwin_arm64_sha),
                ("tunnel-darwin-x64", "chat-codex-tunnel-darwin-x64", tunnel_darwin_x64_sha),
                ("tunnel-linux-x64", "chat-codex-tunnel-linux-x64", tunnel_linux_x64_sha),
                ("tunnel-linux-arm64", "chat-codex-tunnel-linux-arm64", tunnel_linux_arm64_sha),
                ("tunnel-windows-x64", "chat-codex-tunnel-windows-x64.exe", tunnel_windows_x64_sha),
                ("tunnel-windows-arm64", "chat-codex-tunnel-windows-arm64.exe", tunnel_windows_arm64_sha),
            ]
        ],
    ],
}
pathlib.Path(output).write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
PY
}

log_section "版本信息"
echo "mode=$MODE"
echo "server=$SERVER"
echo "public_base=$PUBLIC_BASE"
echo "backend_port=$BACKEND_PORT"
if [ -n "$SSH_KEY" ]; then
  echo "ssh_key=$SSH_KEY"
fi
echo "default_relay_url=$DEFAULT_RELAY_URL"
echo "version_name=$VERSION_NAME"
echo "version_code=$VERSION_CODE"
if [ -n "$CLI_VERSION" ]; then
  echo "cli_version=$CLI_VERSION"
fi
if [ -n "$LAUNCHER_VERSION" ]; then
  echo "launcher_version=$LAUNCHER_VERSION"
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry_run=1"
  exit 0
fi

if [ "$BUILD_BACKEND" -eq 1 ]; then
  log_section "构建后端"
  cd "$ROOT_DIR/backend"
  GOOS=linux GOARCH=amd64 go build -o "$BINARY" ./cmd/server/main.go
  ls -lh "$BINARY"
fi

if [ "$BUILD_FRONTEND" -eq 1 ]; then
  log_section "构建 Flutter Web"
  cd "$ROOT_DIR/flutter_app"
  # --pwa-strategy=none：不注册 Service Worker。控制台不需要离线能力，而 SW 会把
  # 旧版页面壳缓存下来，发布新版本后老用户会一直拿到新旧混合的文件（白屏 / 启动报错）。
  flutter build web --pwa-strategy=none --base-href / --dart-define=CHAT_CODEX_DEFAULT_BASE_URL="$PUBLIC_BASE"
  COPYFILE_DISABLE=1 bsdtar --no-xattrs --no-mac-metadata -C build/web -czf "$WEB_TAR_LOCAL" .
  if [ "$BUILD_MOBILE_APP" -eq 1 ]; then
    rm -f "$APK_LOCAL"
    flutter build apk --release --dart-define=CHAT_CODEX_DEFAULT_BASE_URL="$PUBLIC_BASE"
    require_file "$APK_LOCAL" "Android APK"
    APK_VERSION_LINE="$(apk_version_line "$APK_LOCAL")"
    if [ "$APK_VERSION_LINE" != "$VERSION_NAME+$VERSION_CODE" ]; then
      echo "Android APK 版本不匹配: expected=$VERSION_NAME+$VERSION_CODE actual=$APK_VERSION_LINE" >&2
      exit 1
    fi
    if [ "$ANDROID_ONLY" -eq 0 ]; then
      build_macos_package
    fi
  fi
  flutter build web --pwa-strategy=none --base-href /codex/ --dart-define=CHAT_CODEX_DEFAULT_BASE_URL="$PUBLIC_BASE"
  COPYFILE_DISABLE=1 bsdtar --no-xattrs --no-mac-metadata -C build/web -czf "$WEB_CODEX_TAR_LOCAL" .
  ls -lh "$WEB_TAR_LOCAL"
  if [ "$BUILD_MOBILE_APP" -eq 1 ]; then
    ls -lh "$APK_LOCAL" "$MACOS_PACKAGE_LOCAL"
  fi
  ls -lh "$WEB_CODEX_TAR_LOCAL"
fi

if [ "$BUILD_MOBILE_APP" -eq 1 ] && [ "$BUILD_FRONTEND" -eq 0 ]; then
  log_section "构建 Flutter APK"
  cd "$ROOT_DIR/flutter_app"
  rm -f "$APK_LOCAL"
  flutter build apk --release --dart-define=CHAT_CODEX_DEFAULT_BASE_URL="$PUBLIC_BASE"
  require_file "$APK_LOCAL" "Android APK"
  APK_VERSION_LINE="$(apk_version_line "$APK_LOCAL")"
  if [ "$APK_VERSION_LINE" != "$VERSION_NAME+$VERSION_CODE" ]; then
    echo "Android APK 版本不匹配: expected=$VERSION_NAME+$VERSION_CODE actual=$APK_VERSION_LINE" >&2
    exit 1
  fi
  if [ "$ANDROID_ONLY" -eq 0 ]; then
    build_macos_package
    ls -lh "$APK_LOCAL" "$MACOS_PACKAGE_LOCAL"
  else
    ls -lh "$APK_LOCAL"
  fi
fi

if [ "$BUILD_ADMIN_WEB" -eq 1 ]; then
  log_section "打包独立管理端 Web"
  COPYFILE_DISABLE=1 bsdtar --no-xattrs --no-mac-metadata -C "$ROOT_DIR/admin_web" -czf "$ADMIN_WEB_TAR_LOCAL" .
  ls -lh "$ADMIN_WEB_TAR_LOCAL"
fi

if [ "$BUILD_CLI" -eq 1 ]; then
  log_section "构建 CLI"
  cd "$CLI_ROOT"
  if [ -f "$CLI_MODELS_JSON" ]; then
    MODELS_DEV_API_JSON="$CLI_MODELS_JSON" OPENCODE_TARGETS=linux-x64,darwin-arm64,windows-x64 OPENCODE_VERSION="$CLI_VERSION" OPENCODE_CHANNEL=latest OPENCODE_DEFAULT_RELAY_URL="$DEFAULT_RELAY_URL" bun run script/build.ts --skip-install
  else
    OPENCODE_TARGETS=linux-x64,darwin-arm64,windows-x64 OPENCODE_VERSION="$CLI_VERSION" OPENCODE_CHANNEL=latest OPENCODE_DEFAULT_RELAY_URL="$DEFAULT_RELAY_URL" bun run script/build.ts --skip-install
  fi
  sign_cli_macos_binary
  package_cli_artifacts
  CLI_WINDOWS_SHA256="$(shasum -a 256 "$CLI_WINDOWS_LOCAL" | awk '{print $1}')"
  CLI_DARWIN_SHA256="$(shasum -a 256 "$CLI_DARWIN_LOCAL" | awk '{print $1}')"
  CLI_LINUX_SHA256="$(shasum -a 256 "$CLI_LINUX_LOCAL" | awk '{print $1}')"
  write_cli_version_json
  ls -lh "$CLI_WINDOWS_LOCAL" "$CLI_DARWIN_LOCAL" "$CLI_LINUX_LOCAL"
fi

if [ "$BUILD_LAUNCHER" -eq 1 ]; then
  log_section "构建 Launcher"
  cd "$ROOT_DIR/launcher"
  LAUNCHER_DEFAULT_PUBLIC_BASE="$PUBLIC_BASE" bash script/build-three.sh
  LAUNCHER_WINDOWS_SHA256="$(shasum -a 256 "$LAUNCHER_WINDOWS_LOCAL" | awk '{print $1}')"
  LAUNCHER_DARWIN_SHA256="$(shasum -a 256 "$LAUNCHER_DARWIN_LOCAL" | awk '{print $1}')"
  LAUNCHER_GUI_WINDOWS_SHA256="$(shasum -a 256 "$LAUNCHER_GUI_WINDOWS_LOCAL" | awk '{print $1}')"
  LAUNCHER_GUI_DARWIN_SHA256="$(shasum -a 256 "$LAUNCHER_GUI_DARWIN_LOCAL" | awk '{print $1}')"
  TUNNEL_DARWIN_ARM64_SHA256="$(shasum -a 256 "$TUNNEL_DARWIN_ARM64_LOCAL" | awk '{print $1}')"
  TUNNEL_DARWIN_X64_SHA256="$(shasum -a 256 "$TUNNEL_DARWIN_X64_LOCAL" | awk '{print $1}')"
  TUNNEL_LINUX_X64_SHA256="$(shasum -a 256 "$TUNNEL_LINUX_X64_LOCAL" | awk '{print $1}')"
  TUNNEL_LINUX_ARM64_SHA256="$(shasum -a 256 "$TUNNEL_LINUX_ARM64_LOCAL" | awk '{print $1}')"
  TUNNEL_WINDOWS_X64_SHA256="$(shasum -a 256 "$TUNNEL_WINDOWS_X64_LOCAL" | awk '{print $1}')"
  TUNNEL_WINDOWS_ARM64_SHA256="$(shasum -a 256 "$TUNNEL_WINDOWS_ARM64_LOCAL" | awk '{print $1}')"
  write_launcher_version_json
  ls -lh "$LAUNCHER_WINDOWS_LOCAL" "$LAUNCHER_DARWIN_LOCAL" "$LAUNCHER_GUI_WINDOWS_LOCAL" "$LAUNCHER_GUI_DARWIN_LOCAL" "$ROOT_DIR/launcher/dist-three/tunnel"/*
fi

if [ "$DEPLOY_BACKEND" -eq 1 ]; then
  require_file "$ROOT_DIR/backend/$BINARY" "后端二进制"
fi

if [ "$DEPLOY_FRONTEND" -eq 1 ]; then
  require_file "$WEB_TAR_LOCAL" "前端 Web 包"
  require_file "$WEB_CODEX_TAR_LOCAL" "Codex 前端 Web 包"
fi

if [ "$DEPLOY_ADMIN_WEB" -eq 1 ]; then
  require_file "$ADMIN_WEB_TAR_LOCAL" "独立管理端 Web 包"
fi

if [ "$DEPLOY_MOBILE_APP" -eq 1 ]; then
  require_file "$APK_LOCAL" "Android APK"
  if [ "$ANDROID_ONLY" -eq 0 ]; then
    require_file "$WINDOWS_INSTALLER_LOCAL" "Windows EXE 安装包"
    require_file "$MACOS_PACKAGE_LOCAL" "macOS DMG 安装包"
  fi
  APK_SHA256="$(file_sha256 "$APK_LOCAL")"
  if [ "$ANDROID_ONLY" -eq 0 ]; then
    WINDOWS_INSTALLER_SHA256="$(file_sha256 "$WINDOWS_INSTALLER_LOCAL")"
    MACOS_PACKAGE_SHA256="$(file_sha256 "$MACOS_PACKAGE_LOCAL")"
  else
    WINDOWS_INSTALLER_SHA256=""
    MACOS_PACKAGE_SHA256=""
  fi
fi

if [ "$DEPLOY_CLI" -eq 1 ]; then
  require_file "$CLI_WINDOWS_LOCAL" "CLI Windows 包"
  require_file "$CLI_DARWIN_LOCAL" "CLI macOS 包"
  require_file "$CLI_LINUX_LOCAL" "CLI Linux 包"
  CLI_WINDOWS_SHA256="$(shasum -a 256 "$CLI_WINDOWS_LOCAL" | awk '{print $1}')"
  CLI_DARWIN_SHA256="$(shasum -a 256 "$CLI_DARWIN_LOCAL" | awk '{print $1}')"
  CLI_LINUX_SHA256="$(shasum -a 256 "$CLI_LINUX_LOCAL" | awk '{print $1}')"
  write_cli_version_json
  require_file "$CLI_VERSION_JSON_LOCAL" "CLI 版本元数据"
fi

if [ "$DEPLOY_LAUNCHER" -eq 1 ]; then
  require_file "$LAUNCHER_WINDOWS_LOCAL" "Launcher Windows 包"
  require_file "$LAUNCHER_DARWIN_LOCAL" "Launcher macOS 包"
  require_file "$LAUNCHER_GUI_WINDOWS_LOCAL" "Launcher GUI Windows 包"
  require_file "$LAUNCHER_GUI_DARWIN_LOCAL" "Launcher GUI macOS 包"
  require_file "$TUNNEL_DARWIN_ARM64_LOCAL" "Tunnel macOS arm64"
  require_file "$TUNNEL_DARWIN_X64_LOCAL" "Tunnel macOS x64"
  require_file "$TUNNEL_LINUX_X64_LOCAL" "Tunnel Linux x64"
  require_file "$TUNNEL_LINUX_ARM64_LOCAL" "Tunnel Linux arm64"
  require_file "$TUNNEL_WINDOWS_X64_LOCAL" "Tunnel Windows x64"
  require_file "$TUNNEL_WINDOWS_ARM64_LOCAL" "Tunnel Windows arm64"
  LAUNCHER_WINDOWS_SHA256="$(shasum -a 256 "$LAUNCHER_WINDOWS_LOCAL" | awk '{print $1}')"
  LAUNCHER_DARWIN_SHA256="$(shasum -a 256 "$LAUNCHER_DARWIN_LOCAL" | awk '{print $1}')"
  LAUNCHER_GUI_WINDOWS_SHA256="$(shasum -a 256 "$LAUNCHER_GUI_WINDOWS_LOCAL" | awk '{print $1}')"
  LAUNCHER_GUI_DARWIN_SHA256="$(shasum -a 256 "$LAUNCHER_GUI_DARWIN_LOCAL" | awk '{print $1}')"
  TUNNEL_DARWIN_ARM64_SHA256="$(shasum -a 256 "$TUNNEL_DARWIN_ARM64_LOCAL" | awk '{print $1}')"
  TUNNEL_DARWIN_X64_SHA256="$(shasum -a 256 "$TUNNEL_DARWIN_X64_LOCAL" | awk '{print $1}')"
  TUNNEL_LINUX_X64_SHA256="$(shasum -a 256 "$TUNNEL_LINUX_X64_LOCAL" | awk '{print $1}')"
  TUNNEL_LINUX_ARM64_SHA256="$(shasum -a 256 "$TUNNEL_LINUX_ARM64_LOCAL" | awk '{print $1}')"
  TUNNEL_WINDOWS_X64_SHA256="$(shasum -a 256 "$TUNNEL_WINDOWS_X64_LOCAL" | awk '{print $1}')"
  TUNNEL_WINDOWS_ARM64_SHA256="$(shasum -a 256 "$TUNNEL_WINDOWS_ARM64_LOCAL" | awk '{print $1}')"
  write_launcher_version_json
  require_file "$LAUNCHER_VERSION_JSON_LOCAL" "Launcher 版本元数据"
fi

if [ "$DEPLOY_RUNTIME" -eq 1 ]; then
  log_section "准备托管运行时"
  ensure_uv_artifacts
  ensure_node_artifacts
  ensure_go_artifacts
  ensure_java_artifacts
  ensure_ghidra_artifact
  ensure_ida_artifacts
  ensure_android_tool_artifacts
  ensure_mcp_artifacts
  ls -lh "$UV_WINDOWS_LOCAL" "$UV_DARWIN_LOCAL" "$UV_LINUX_LOCAL"
  ls -lh "$NODE_WINDOWS_LOCAL" "$NODE_DARWIN_LOCAL" "$NODE_LINUX_LOCAL"
  ls -lh "$GO_WINDOWS_LOCAL" "$GO_DARWIN_ARM64_LOCAL" "$GO_DARWIN_AMD64_LOCAL" "$GO_LINUX_AMD64_LOCAL"
  ls -lh "$JAVA_WINDOWS_LOCAL" "$JAVA_DARWIN_LOCAL" "$JAVA_LINUX_LOCAL"
  ls -lh "$GHIDRA_LOCAL"
  ls -lh "$IDA_WINDOWS_LOCAL" "$IDA_DARWIN_AMD64_LOCAL" "$IDA_DARWIN_ARM64_LOCAL"
  ls -lh "$JADX_TOOL_LOCAL" "$APKTOOL_TOOL_LOCAL"
  ls -lh "$MCP_JADX_LOCAL" "$MCP_APKTOOL_LOCAL" "$MCP_FRIDA_LOCAL" "$MCP_IDA_PRO_LOCAL"
fi

if [ "$DEPLOY_MOBILE_APP" -eq 1 ]; then
  log_section "生成客户端版本元数据"
  write_frontend_version_json
  require_file "$VERSION_JSON_LOCAL" "客户端版本元数据"
  cat "$VERSION_JSON_LOCAL"
  echo
fi

if [ "$DEPLOY_MOBILE_APP" -eq 1 ] && [ "$DEPLOY_FRONTEND" -eq 0 ]; then
  if [ "$ANDROID_ONLY" -eq 1 ]; then
    log_section "上传 Android APK 与版本元数据"
  else
    log_section "上传 Android APK、Windows EXE、macOS DMG 与版本元数据"
  fi
  upload_file "$APK_LOCAL" "/tmp/app-release.apk"
  if [ "$ANDROID_ONLY" -eq 0 ]; then
    upload_file "$WINDOWS_INSTALLER_LOCAL" "/tmp/$WINDOWS_INSTALLER_FILENAME"
    upload_file "$MACOS_PACKAGE_LOCAL" "/tmp/$MACOS_PACKAGE_FILENAME"
  fi
  if [ "$ANDROID_ONLY" -eq 1 ]; then
    log_section "合并已有桌面端下载条目"
    python3 - "$VERSION_JSON_LOCAL" "$PUBLIC_BASE" <<'PY' || true
import json, pathlib, sys, urllib.request
output, base = sys.argv[1], sys.argv[2].rstrip("/")
new = json.loads(pathlib.Path(output).read_text(encoding="utf-8"))
prev = {}
try:
    with urllib.request.urlopen(base + "/api/app/version", timeout=8) as resp:
        prev = json.loads(resp.read().decode("utf-8"))
except Exception:
    prev = {}
prev_code = int(prev.get("version_code") or 0)
merged = [item for item in new.get("downloads") or [] if item.get("platform") == "android"]
seen = {"android"}
for item in prev.get("downloads") or []:
    platform = str(item.get("platform") or "")
    if not platform or platform in seen:
        continue
    seen.add(platform)
    kept = dict(item)
    if int(kept.get("version_code") or 0) <= 0 and prev_code > 0:
        kept["version_code"] = prev_code
    merged.append(kept)
new["downloads"] = merged
pathlib.Path(output).write_text(json.dumps(new, ensure_ascii=False), encoding="utf-8")
PY
  fi
  upload_file "$VERSION_JSON_LOCAL" "/tmp/version.json"
  if [ "$ANDROID_ONLY" -eq 1 ]; then
    remote_ssh "
      mkdir -p $APK_DIR &&
      install -m 644 /tmp/app-release.apk $APK_DIR/app-release.apk &&
      install -m 644 /tmp/version.json $APK_DIR/version.json &&
      ls -lh $APK_DIR/app-release.apk $APK_DIR/version.json &&
      echo '--- app version.json ---' &&
      cat $APK_DIR/version.json
    "
  else
    remote_ssh "
      mkdir -p $APK_DIR &&
      install -m 644 /tmp/app-release.apk $APK_DIR/app-release.apk &&
      install -m 644 /tmp/$WINDOWS_INSTALLER_FILENAME $APK_DIR/$WINDOWS_INSTALLER_FILENAME &&
      install -m 644 /tmp/$MACOS_PACKAGE_FILENAME $APK_DIR/$MACOS_PACKAGE_FILENAME &&
      install -m 644 /tmp/version.json $APK_DIR/version.json &&
      ls -lh $APK_DIR &&
      echo '--- app version.json ---' &&
      cat $APK_DIR/version.json
    "
  fi
fi

if [ "$DEPLOY_BACKEND" -eq 1 ]; then
  log_section "上传并部署后端"
  upload_file "$ROOT_DIR/backend/$BINARY" "/tmp/${BINARY}.new"
  remote_ssh "
    set -e
    mkdir -p $BACKEND_DIR
    install -d -m 700 /var/lib/chat-codex
    if [ -f $BACKEND_DIR/$BINARY ]; then
      cp -p $BACKEND_DIR/$BINARY $BACKEND_DIR/$BINARY.rollback
    fi
    install -m 755 /tmp/${BINARY}.new $BACKEND_DIR/$BINARY
    mkdir -p /etc/systemd/system/$SERVICE.service.d &&
    cat > /etc/systemd/system/$SERVICE.service.d/port.conf <<EOF
[Service]
Environment=ADDR=:$BACKEND_PORT
Environment=ARCHIVE_HIGH_FREQ_EVENTS=0
EOF
    systemctl daemon-reload
    if ! systemctl restart $SERVICE; then
      echo 'new backend failed to start; rolling back' >&2
      if [ -f $BACKEND_DIR/$BINARY.rollback ]; then
        install -m 755 $BACKEND_DIR/$BINARY.rollback $BACKEND_DIR/$BINARY
        systemctl restart $SERVICE || true
      fi
      exit 1
    fi
    ready=0
    for attempt in \$(seq 1 30); do
      if curl -fsS --max-time 2 http://127.0.0.1:$BACKEND_PORT/readyz >/dev/null; then
        ready=1
        break
      fi
      sleep 1
    done
    if [ "\$ready" -ne 1 ]; then
      echo 'new backend failed readiness; rolling back' >&2
      if [ -f $BACKEND_DIR/$BINARY.rollback ]; then
        install -m 755 $BACKEND_DIR/$BINARY.rollback $BACKEND_DIR/$BINARY
        systemctl restart $SERVICE
      fi
      systemctl status $SERVICE --no-pager | sed -n '1,20p'
      exit 1
    fi
    rm -f $BACKEND_DIR/$BINARY.rollback
    systemctl status $SERVICE --no-pager | sed -n '1,12p'
  "
  remote_ssh "
    if [ -f $DOMAIN_NGINX_CONF ]; then
      cp $DOMAIN_NGINX_CONF $DOMAIN_NGINX_CONF.bak-codex-backend-port-\$(date +%Y%m%d%H%M%S) &&
      python3 - <<'PY' &&
from pathlib import Path
import re
path = Path('$DOMAIN_NGINX_CONF')
text = path.read_text(encoding='utf-8')
for suffix in ('/api/', '/ws/', '/admin/', '/healthz', '/readyz'):
    text = re.sub(
        rf'proxy_pass http://127\.0\.0\.1:\d+{re.escape(suffix)}',
        f'proxy_pass http://127.0.0.1:$BACKEND_PORT{suffix}',
        text,
    )
bootstrap_block = '''# chat-codex: temporary SSH bootstrap
location ^~ /codex/b/ {
    proxy_pass http://127.0.0.1:$BACKEND_PORT/b/;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    access_log off;
    proxy_no_cache 1;
    proxy_cache_bypass 1;
    proxy_buffering off;
}

'''
bootstrap_pattern = re.compile(
    r'(?ms)^[ \t]*# chat-codex: temporary SSH bootstrap[ \t]*\n'
    r'[ \t]*location \^~ /codex/b/ \{.*?^[ \t]*\}[ \t]*\n?'
)
text = bootstrap_pattern.sub('', text)
sse_block = '''# chat-codex: notification SSE
location ^~ /codex/api/overlay/events {
    proxy_pass http://127.0.0.1:$BACKEND_PORT/api/overlay/events;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    proxy_set_header Connection \"\";
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
}

'''
sse_pattern = re.compile(
    r'(?ms)^[ \t]*# chat-codex: notification SSE[ \t]*\n'
    r'[ \t]*location \^~ /codex/api/overlay/events \{.*?^[ \t]*\}[ \t]*\n?'
)
text = sse_pattern.sub('', text)
markers = ('# chat-codex: API 代理\n', 'location ^~ /codex/ {')
marker = next((candidate for candidate in markers if candidate in text), '')
if not marker:
    raise SystemExit('未找到 /codex/b/ nginx 插入点')
text = text.replace(marker, sse_block + bootstrap_block + marker, 1)
path.write_text(text, encoding='utf-8')
PY
      nginx -t &&
      nginx -s reload
    else
      echo '缺少域名 Nginx 配置: $DOMAIN_NGINX_CONF' >&2
      exit 1
    fi
  "
fi

if [ "$DEPLOY_FRONTEND" -eq 1 ]; then
  log_section "上传并部署前端"
  upload_file "$WEB_TAR_LOCAL" "/tmp/chatcodex_web.tgz"
  upload_file "$WEB_CODEX_TAR_LOCAL" "/tmp/chatcodex_web_codex.tgz"
  remote_ssh "
    mkdir -p $WEB_DIR &&
    tar -xzf /tmp/chatcodex_web.tgz -C $WEB_DIR
  "
  if [ "$DEPLOY_MOBILE_APP" -eq 1 ]; then
    upload_file "$APK_LOCAL" "/tmp/app-release.apk"
    upload_file "$WINDOWS_INSTALLER_LOCAL" "/tmp/$WINDOWS_INSTALLER_FILENAME"
    upload_file "$MACOS_PACKAGE_LOCAL" "/tmp/$MACOS_PACKAGE_FILENAME"
    upload_file "$VERSION_JSON_LOCAL" "/tmp/version.json"
    remote_ssh "
    mkdir -p $APK_DIR &&
    install -m 644 /tmp/app-release.apk $APK_DIR/app-release.apk &&
    install -m 644 /tmp/$WINDOWS_INSTALLER_FILENAME $APK_DIR/$WINDOWS_INSTALLER_FILENAME &&
    install -m 644 /tmp/$MACOS_PACKAGE_FILENAME $APK_DIR/$MACOS_PACKAGE_FILENAME &&
    install -m 644 /tmp/version.json $APK_DIR/version.json &&
    ls -lh $APK_DIR &&
    echo '--- app version.json ---' &&
    cat $APK_DIR/version.json
  "
  fi
  remote_ssh "
      mkdir -p $CODEX_WEB_DIR &&
      tar -xzf /tmp/chatcodex_web_codex.tgz -C $CODEX_WEB_DIR &&
      if [ -f $DOMAIN_NGINX_CONF ]; then
        cp $DOMAIN_NGINX_CONF $DOMAIN_NGINX_CONF.bak-codex-\$(date +%Y%m%d%H%M%S) &&
        python3 - <<'PY' &&
import re
from pathlib import Path
path = Path('$DOMAIN_NGINX_CONF')
text = path.read_text(encoding='utf-8')
old = 'location ^~ /codex/ {\\n    alias /opt/chat-codex/web/;\\n    try_files \$uri \$uri/ /codex/index.html;\\n}'
new = 'location ^~ /codex/ {\\n    alias /opt/chat-codex/web-codex/;\\n    try_files \$uri \$uri/ /codex/index.html;\\n}'
if old in text:
    text = text.replace(old, new)
elif 'alias /opt/chat-codex/web-codex/;' not in text:
    raise SystemExit('未找到 /codex/ 静态资源 location')
# WebAssembly 编译要求 application/wasm，默认 mime.types 里没有这条；
# 缺了它 Chromium 内核（Edge 等）会直接拒绝编译 canvaskit.wasm 而白屏。
if 'application/wasm' not in text:
    pattern = re.compile(r'(location \^~ /codex/ \{[^}]*?)(\\n\s*\})', re.S)
    text, count = pattern.subn(
        lambda m: m.group(1)
        + '\\n    include /etc/nginx/mime.types;'
        + '\\n    types { application/wasm wasm; }'
        + m.group(2),
        text,
        count=1,
    )
    if count != 1:
        raise SystemExit('未找到可注入 wasm 类型的 /codex/ location')
path.write_text(text, encoding='utf-8')
PY
        nginx -t &&
        nginx -s reload
      fi &&
      echo '--- codex web index base ---' &&
      grep -n '<base' $CODEX_WEB_DIR/index.html
    "
fi

if [ "$DEPLOY_ADMIN_WEB" -eq 1 ]; then
  log_section "上传并部署独立管理端 Web"
  upload_file "$ADMIN_WEB_TAR_LOCAL" "/tmp/chatcodex_admin_web.tgz"
  remote_ssh "
    rm -rf $ADMIN_WEB_DIR &&
    mkdir -p $ADMIN_WEB_DIR &&
    tar -xzf /tmp/chatcodex_admin_web.tgz -C $ADMIN_WEB_DIR &&
    find $ADMIN_WEB_DIR -maxdepth 1 -type f -print
  "
  remote_ssh "
      if [ -f $IP_NGINX_CONF ]; then
        cp $IP_NGINX_CONF $IP_NGINX_CONF.bak-admin-\$(date +%Y%m%d%H%M%S) &&
        python3 - <<'PY' &&
from pathlib import Path
path = Path('$IP_NGINX_CONF')
text = path.read_text(encoding='utf-8')
block = '''
    location = /admin { return 301 /admin/; }

    location ^~ /admin/ {
        proxy_pass http://127.0.0.1:$BACKEND_PORT/admin/;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    }
'''
if 'location ^~ /admin/' not in text:
    marker = '    # API 和 WebSocket 代理到后端\\n'
    if marker not in text:
        raise SystemExit('未找到旧服 /admin/ nginx 插入点')
    path.write_text(text.replace(marker, block + '\\n' + marker), encoding='utf-8')
PY
        nginx -t &&
        nginx -s reload
      fi
    "
  remote_ssh "
      if [ -f $DOMAIN_NGINX_CONF ]; then
        cp $DOMAIN_NGINX_CONF $DOMAIN_NGINX_CONF.bak-codex-admin-\$(date +%Y%m%d%H%M%S) &&
        python3 - <<'PY' &&
from pathlib import Path
path = Path('$DOMAIN_NGINX_CONF')
text = path.read_text(encoding='utf-8')
block = '''
# chat-codex: 管理端
location = /codex-admin { return 301 /codex-admin/; }

location ^~ /codex-admin/ {
    proxy_pass http://127.0.0.1:$BACKEND_PORT/admin/;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    proxy_no_cache 1;
    proxy_cache_bypass 1;
}

'''
if 'location ^~ /codex-admin/' not in text:
    marker = '# chat-codex: API 代理\\n'
    if marker not in text:
        raise SystemExit('未找到旧服域名 /codex-admin/ nginx 插入点')
    path.write_text(text.replace(marker, block + marker), encoding='utf-8')
PY
        nginx -t &&
        nginx -s reload
      fi
    "
fi

if [ "$DEPLOY_CLI" -eq 1 ]; then
  log_section "上传 CLI"
  upload_file "$CLI_VERSION_JSON_LOCAL" "/tmp/cli-version.json"
  upload_file "$CLI_WINDOWS_LOCAL" "/tmp/opencode-windows-x64.zip"
  upload_file "$CLI_DARWIN_LOCAL" "/tmp/opencode-darwin-arm64.zip"
  upload_file "$CLI_LINUX_LOCAL" "/tmp/opencode-linux-x64.tar.gz"
  remote_ssh "
    mkdir -p $CLI_DIR &&
    install -m 644 /tmp/cli-version.json $CLI_DIR/version.json &&
    install -m 644 /tmp/opencode-windows-x64.zip $CLI_DIR/opencode-windows-x64.zip &&
    install -m 644 /tmp/opencode-darwin-arm64.zip $CLI_DIR/opencode-darwin-arm64.zip &&
    install -m 644 /tmp/opencode-linux-x64.tar.gz $CLI_DIR/opencode-linux-x64.tar.gz &&
    ls -lh $CLI_DIR &&
    echo '--- cli version.json ---' &&
    cat $CLI_DIR/version.json
  "
fi

if [ "$DEPLOY_LAUNCHER" -eq 1 ]; then
  log_section "上传 Launcher"
  remote_ssh "mkdir -p $LAUNCHER_DIR"
  upload_file "$LAUNCHER_VERSION_JSON_LOCAL" "/tmp/launcher-version.json"
  upload_file "$LAUNCHER_WINDOWS_LOCAL" "/tmp/launcher-windows-x64.zip"
  upload_file "$LAUNCHER_DARWIN_LOCAL" "/tmp/launcher-darwin-arm64.zip"
  upload_file "$LAUNCHER_GUI_WINDOWS_LOCAL" "/tmp/chat-codex-launcher-windows-x64.zip"
  upload_file "$LAUNCHER_GUI_DARWIN_LOCAL" "/tmp/chat-codex-launcher-darwin-arm64.zip"
  upload_file "$TUNNEL_DARWIN_ARM64_LOCAL" "/tmp/chat-codex-tunnel-darwin-arm64"
  upload_file "$TUNNEL_DARWIN_X64_LOCAL" "/tmp/chat-codex-tunnel-darwin-x64"
  upload_file "$TUNNEL_LINUX_X64_LOCAL" "/tmp/chat-codex-tunnel-linux-x64"
  upload_file "$TUNNEL_LINUX_ARM64_LOCAL" "/tmp/chat-codex-tunnel-linux-arm64"
  upload_file "$TUNNEL_WINDOWS_X64_LOCAL" "/tmp/chat-codex-tunnel-windows-x64.exe"
  upload_file "$TUNNEL_WINDOWS_ARM64_LOCAL" "/tmp/chat-codex-tunnel-windows-arm64.exe"
  remote_ssh "
    mkdir -p $LAUNCHER_DIR &&
    install -m 644 /tmp/launcher-version.json $LAUNCHER_DIR/version.json &&
    install -m 644 /tmp/launcher-windows-x64.zip $LAUNCHER_DIR/launcher-windows-x64.zip &&
    install -m 644 /tmp/launcher-darwin-arm64.zip $LAUNCHER_DIR/launcher-darwin-arm64.zip &&
    install -m 644 /tmp/chat-codex-launcher-windows-x64.zip $LAUNCHER_DIR/chat-codex-launcher-windows-x64.zip &&
    install -m 644 /tmp/chat-codex-launcher-darwin-arm64.zip $LAUNCHER_DIR/chat-codex-launcher-darwin-arm64.zip &&
    install -m 644 /tmp/chat-codex-tunnel-darwin-arm64 $LAUNCHER_DIR/chat-codex-tunnel-darwin-arm64 &&
    install -m 644 /tmp/chat-codex-tunnel-darwin-x64 $LAUNCHER_DIR/chat-codex-tunnel-darwin-x64 &&
    install -m 644 /tmp/chat-codex-tunnel-linux-x64 $LAUNCHER_DIR/chat-codex-tunnel-linux-x64 &&
    install -m 644 /tmp/chat-codex-tunnel-linux-arm64 $LAUNCHER_DIR/chat-codex-tunnel-linux-arm64 &&
    install -m 644 /tmp/chat-codex-tunnel-windows-x64.exe $LAUNCHER_DIR/chat-codex-tunnel-windows-x64.exe &&
    install -m 644 /tmp/chat-codex-tunnel-windows-arm64.exe $LAUNCHER_DIR/chat-codex-tunnel-windows-arm64.exe &&
    rm -f $LAUNCHER_DIR/launcher-linux-x64.tar.gz &&
    ls -lh $LAUNCHER_DIR &&
    echo '--- launcher version.json ---' &&
    cat $LAUNCHER_DIR/version.json
  "
fi

if [ "$DEPLOY_RUNTIME" -eq 1 ]; then
  log_section "上传托管运行时"
  remote_ssh "mkdir -p $RUNTIME_DIR/uv $RUNTIME_DIR/node $RUNTIME_DIR/go $RUNTIME_DIR/java $RUNTIME_DIR/ghidra $RUNTIME_DIR/ida $RUNTIME_DIR/tool $RUNTIME_DIR/mcp"
  upload_file_if_changed "$UV_WINDOWS_LOCAL" "$RUNTIME_DIR/uv/uv-x86_64-pc-windows-msvc.zip"
  upload_file_if_changed "$UV_DARWIN_LOCAL" "$RUNTIME_DIR/uv/uv-aarch64-apple-darwin.tar.gz"
  upload_file_if_changed "$UV_LINUX_LOCAL" "$RUNTIME_DIR/uv/uv-x86_64-unknown-linux-gnu.tar.gz"
  upload_file_if_changed "$NODE_WINDOWS_LOCAL" "$RUNTIME_DIR/node/node-${RUNTIME_NODE_VERSION}-win-x64.zip"
  upload_file_if_changed "$NODE_DARWIN_LOCAL" "$RUNTIME_DIR/node/node-${RUNTIME_NODE_VERSION}-darwin-arm64.tar.gz"
  upload_file_if_changed "$NODE_LINUX_LOCAL" "$RUNTIME_DIR/node/node-${RUNTIME_NODE_VERSION}-linux-x64.tar.gz"
  upload_file_if_changed "$GO_WINDOWS_LOCAL" "$RUNTIME_DIR/go/go${RUNTIME_GO_VERSION}.windows-amd64.zip"
  upload_file_if_changed "$GO_DARWIN_ARM64_LOCAL" "$RUNTIME_DIR/go/go${RUNTIME_GO_VERSION}.darwin-arm64.tar.gz"
  upload_file_if_changed "$GO_DARWIN_AMD64_LOCAL" "$RUNTIME_DIR/go/go${RUNTIME_GO_VERSION}.darwin-amd64.tar.gz"
  upload_file_if_changed "$GO_LINUX_AMD64_LOCAL" "$RUNTIME_DIR/go/go${RUNTIME_GO_VERSION}.linux-amd64.tar.gz"
  upload_file_if_changed "$JAVA_WINDOWS_LOCAL" "$RUNTIME_DIR/java/$JAVA_WINDOWS_FILENAME"
  upload_file_if_changed "$JAVA_DARWIN_LOCAL" "$RUNTIME_DIR/java/$JAVA_DARWIN_FILENAME"
  upload_file_if_changed "$JAVA_LINUX_LOCAL" "$RUNTIME_DIR/java/$JAVA_LINUX_FILENAME"
  verify_remote_file_sha256 "$RUNTIME_DIR/java/$JAVA_WINDOWS_FILENAME" "$JAVA_WINDOWS_SHA256"
  verify_remote_file_sha256 "$RUNTIME_DIR/java/$JAVA_DARWIN_FILENAME" "$JAVA_DARWIN_SHA256"
  verify_remote_file_sha256 "$RUNTIME_DIR/java/$JAVA_LINUX_FILENAME" "$JAVA_LINUX_SHA256"
  upload_file_if_changed "$GHIDRA_LOCAL" "$RUNTIME_DIR/ghidra/$GHIDRA_FILENAME"
  upload_file_if_changed "$IDA_WINDOWS_LOCAL" "$RUNTIME_DIR/ida/$IDA_WINDOWS_FILENAME"
  upload_file_if_changed "$IDA_DARWIN_AMD64_LOCAL" "$RUNTIME_DIR/ida/$IDA_DARWIN_AMD64_FILENAME"
  upload_file_if_changed "$IDA_DARWIN_ARM64_LOCAL" "$RUNTIME_DIR/ida/$IDA_DARWIN_ARM64_FILENAME"
  upload_file_if_changed "$JADX_TOOL_LOCAL" "$RUNTIME_DIR/tool/$JADX_TOOL_FILENAME"
  upload_file_if_changed "$APKTOOL_TOOL_LOCAL" "$RUNTIME_DIR/tool/$APKTOOL_TOOL_FILENAME"
  verify_remote_file_sha256 "$RUNTIME_DIR/tool/$JADX_TOOL_FILENAME" "$JADX_TOOL_SHA256"
  verify_remote_file_sha256 "$RUNTIME_DIR/tool/$APKTOOL_TOOL_FILENAME" "$APKTOOL_TOOL_SHA256"
  upload_file_if_changed "$MCP_JADX_LOCAL" "$RUNTIME_DIR/mcp/jadx-mcp-server.tar.gz"
  upload_file_if_changed "$MCP_APKTOOL_LOCAL" "$RUNTIME_DIR/mcp/apktool-mcp-server.tar.gz"
  upload_file_if_changed "$MCP_FRIDA_LOCAL" "$RUNTIME_DIR/mcp/frida-analykit.tar.gz"
  upload_file_if_changed "$MCP_IDA_PRO_LOCAL" "$RUNTIME_DIR/mcp/ida-pro-mcp.tar.gz"
  upload_file_if_changed "$MCP_IDA_PRO_WHEEL_LOCAL" "$RUNTIME_DIR/mcp/ida_pro_mcp-2.0.0-py3-none-any.whl"
  upload_file_if_changed "$MCP_IDAPRO_WHEEL_LOCAL" "$RUNTIME_DIR/mcp/idapro-0.0.10-py3-none-any.whl"
  upload_file_if_changed "$MCP_TOMLI_W_WHEEL_LOCAL" "$RUNTIME_DIR/mcp/tomli_w-1.2.0-py3-none-any.whl"
  upload_file_if_changed "$MCP_MIRA_LOCAL" "$RUNTIME_DIR/mcp/ios-vwww-droid-mira.tar.gz"
  upload_file_if_changed "$MCP_WIREMCP_LOCAL" "$RUNTIME_DIR/mcp/wiremcp.tar.gz"
  upload_file_if_changed "$MCP_BINARY_LOCAL" "$RUNTIME_DIR/mcp/binary-mcp.tar.gz"
  upload_file_if_changed "$MCP_JSREVERSER_LOCAL" "$RUNTIME_DIR/mcp/web-noone-jsreverser-mcp.tar.gz"
  upload_file_if_changed "$MCP_TRIAGE_LOCAL" "$RUNTIME_DIR/mcp/windows-eversinc33-triagemcp.tar.gz"
  upload_file_if_changed "$MCP_VOLATILITY_LOCAL" "$RUNTIME_DIR/mcp/windows-gaffx-volatility-mcp.tar.gz"
  upload_file_if_changed "$MCP_X64DBG_PY_LOCAL" "$RUNTIME_DIR/mcp/windows-wasdubya-x64dbgmcp.tar.gz"
  remote_ssh "
    ls -lh $RUNTIME_DIR/uv &&
    ls -lh $RUNTIME_DIR/node &&
    ls -lh $RUNTIME_DIR/go &&
    ls -lh $RUNTIME_DIR/java &&
    ls -lh $RUNTIME_DIR/ghidra &&
    ls -lh $RUNTIME_DIR/ida &&
    ls -lh $RUNTIME_DIR/tool &&
    ls -lh $RUNTIME_DIR/mcp
  "
fi

log_section "线上验证"
curl --fail -sS --max-time 10 "$PUBLIC_BASE/healthz"
echo

if [ "$VERIFY_FRONTEND" -eq 1 ]; then
  curl -I -s --max-time 10 "$PUBLIC_BASE/" | sed -n '1,12p'
fi

if [ "$VERIFY_MOBILE_APP" -eq 1 ]; then
  echo
  curl -s --max-time 10 "$PUBLIC_BASE/api/app/version"
  echo
  verify_download_range "$PUBLIC_BASE/api/app/download?v=$VERSION_CODE" 60
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/app/download?v=$VERSION_CODE" "$APK_SHA256" 300
  if [ "$ANDROID_ONLY" -eq 0 ]; then
    echo
    verify_download_range "$PUBLIC_BASE/api/app/download?artifact=$WINDOWS_INSTALLER_FILENAME&v=$VERSION_CODE" 60
    echo
    verify_download_range "$PUBLIC_BASE/api/app/download?artifact=$MACOS_PACKAGE_FILENAME&v=$VERSION_CODE" 60
    echo
    verify_download_sha256 "$PUBLIC_BASE/api/app/download?artifact=$WINDOWS_INSTALLER_FILENAME&v=$VERSION_CODE" "$WINDOWS_INSTALLER_SHA256" 300
    echo
    verify_download_sha256 "$PUBLIC_BASE/api/app/download?artifact=$MACOS_PACKAGE_FILENAME&v=$VERSION_CODE" "$MACOS_PACKAGE_SHA256" 300
  fi
fi

if [ "$VERIFY_ADMIN_WEB" -eq 1 ]; then
  echo
  remote_ssh "test -f $ADMIN_WEB_DIR/index.html && sed -n '1,8p' $ADMIN_WEB_DIR/index.html"
fi

if [ "$VERIFY_CLI" -eq 1 ]; then
  echo
  curl --fail -sS --max-time 10 "$PUBLIC_BASE/api/cli/version"
  echo
  verify_download_range "$PUBLIC_BASE/api/app/download?artifact=opencode-windows-x64.zip" 60
  echo
  verify_download_range "$PUBLIC_BASE/api/app/download?artifact=opencode-darwin-arm64.zip" 60
  echo
  verify_download_range "$PUBLIC_BASE/api/app/download?artifact=opencode-linux-x64.tar.gz" 60
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/app/download?artifact=opencode-windows-x64.zip" "$CLI_WINDOWS_SHA256" 300
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/app/download?artifact=opencode-darwin-arm64.zip" "$CLI_DARWIN_SHA256" 300
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/app/download?artifact=opencode-linux-x64.tar.gz" "$CLI_LINUX_SHA256" 300
  echo
  remote_ssh "cat $CLI_DIR/version.json"
fi

if [ "$VERIFY_LAUNCHER" -eq 1 ]; then
  echo
  curl -s --max-time 10 "$PUBLIC_BASE/api/launcher/version"
  echo
  curl -s --max-time 10 "$PUBLIC_BASE/api/launcher/downloads"
  echo
  verify_download_range "$PUBLIC_BASE/api/launcher/download?artifact=launcher-windows-x64.zip" 60
  echo
  verify_download_range "$PUBLIC_BASE/api/launcher/download?artifact=launcher-darwin-arm64.zip" 60
  echo
  verify_download_range "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-launcher-windows-x64.zip" 60
  echo
  verify_download_range "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-launcher-darwin-arm64.zip" 60
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=launcher-windows-x64.zip" "$LAUNCHER_WINDOWS_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=launcher-darwin-arm64.zip" "$LAUNCHER_DARWIN_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-launcher-windows-x64.zip" "$LAUNCHER_GUI_WINDOWS_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-launcher-darwin-arm64.zip" "$LAUNCHER_GUI_DARWIN_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-tunnel-darwin-arm64" "$TUNNEL_DARWIN_ARM64_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-tunnel-darwin-x64" "$TUNNEL_DARWIN_X64_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-tunnel-linux-x64" "$TUNNEL_LINUX_X64_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-tunnel-linux-arm64" "$TUNNEL_LINUX_ARM64_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-tunnel-windows-x64.exe" "$TUNNEL_WINDOWS_X64_SHA256" 180
  echo
  verify_download_sha256 "$PUBLIC_BASE/api/launcher/download?artifact=chat-codex-tunnel-windows-arm64.exe" "$TUNNEL_WINDOWS_ARM64_SHA256" 180
  echo
  remote_ssh "cat $LAUNCHER_DIR/version.json"
fi

if [ "$VERIFY_RUNTIME" -eq 1 ]; then
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/uv/download?artifact=uv-aarch64-apple-darwin.tar.gz" 30
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/node/download?artifact=node-${RUNTIME_NODE_VERSION}-darwin-arm64.tar.gz" 30
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/go/download?artifact=go${RUNTIME_GO_VERSION}.darwin-arm64.tar.gz" 60
  echo
  verify_download_range "$PUBLIC_BASE/api/runtime/java/download?artifact=$JAVA_DARWIN_FILENAME" 240
  echo
  verify_download_range "$PUBLIC_BASE/api/runtime/ida/download?artifact=$IDA_DARWIN_ARM64_FILENAME" 240
  echo
  verify_download_range "$PUBLIC_BASE/api/runtime/tool/download?artifact=$JADX_TOOL_FILENAME" 120
  echo
  verify_download_range "$PUBLIC_BASE/api/runtime/tool/download?artifact=$APKTOOL_TOOL_FILENAME" 60
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/jadx-mcp-server.tar.gz" 30
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/apktool-mcp-server.tar.gz" 30
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/frida-analykit.tar.gz" 30
  echo
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/ida-pro-mcp.tar.gz" 30
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/ida_pro_mcp-2.0.0-py3-none-any.whl" 30
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/idapro-0.0.10-py3-none-any.whl" 30
  verify_download_headers "$PUBLIC_BASE/api/runtime/mcp/download/tomli_w-1.2.0-py3-none-any.whl" 30
fi

echo
echo "部署完成: $PUBLIC_BASE"
