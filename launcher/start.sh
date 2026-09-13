#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

export LAUNCHER_PUBLIC_BASE="${LAUNCHER_PUBLIC_BASE:-${LAUNCHER_DEFAULT_PUBLIC_BASE:-https://www.xyapi.top/codex}}"
export LAUNCHER_RELAY_URL="${LAUNCHER_RELAY_URL:-}"
export OPENCODE_RELAY_OPERATOR_KEY="${OPENCODE_RELAY_OPERATOR_KEY:-}"
export LAUNCHER_MACHINE_ID="${LAUNCHER_MACHINE_ID:-}"

if [ -z "$OPENCODE_RELAY_OPERATOR_KEY" ]; then
  echo "缺少 OPENCODE_RELAY_OPERATOR_KEY"
  echo "示例: OPENCODE_RELAY_OPERATOR_KEY=opk_xxx bash launcher/start.sh"
  exit 1
fi

cd "$ROOT_DIR"
go run ./cmd/launcher
