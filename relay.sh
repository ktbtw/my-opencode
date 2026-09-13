#!/bin/bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$ROOT_DIR/my-opencode"
PID_FILE="$ROOT_DIR/.relay.pid"
LOG_FILE="$ROOT_DIR/.relay.log"

HOSTNAME_VALUE="$(hostname)"

RELAY_URL="${OPENCODE_RELAY_URL:-wss://www.xyapi.top/codex/ws/device}"
RELAY_OPERATOR_KEY="${OPENCODE_RELAY_OPERATOR_KEY:-opk_7479b72c5aa3262e3c63ed0416264842a72258a550833ad1}"
RELAY_AGENT_ID="${OPENCODE_RELAY_AGENT_ID:-${HOSTNAME_VALUE}:my-opencode}"
RELAY_MACHINE_ID="${OPENCODE_RELAY_MACHINE_ID:-${HOSTNAME_VALUE}}"
RELAY_PROJECT_ID="${OPENCODE_RELAY_PROJECT_ID:-my-opencode}"
RELAY_PROJECT_ROOT="${OPENCODE_RELAY_PROJECT_ROOT:-$PROJECT_DIR}"
RELAY_PERMISSION_MODE="${OPENCODE_RELAY_PERMISSION_MODE:-auto-approve}"
RELAY_TASK_TIMEOUT_MS="${OPENCODE_RELAY_TASK_TIMEOUT_MS:-2100000}"

API_BASE="${CHAT_CODEX_API_BASE:-https://www.xyapi.top/codex}"
ADMIN_USER="${CHAT_CODEX_ADMIN_USER:-admin}"
ADMIN_PASS="${CHAT_CODEX_ADMIN_PASS:-admin123456}"

usage() {
  cat <<EOF
用法: ./relay.sh <start|stop|restart|status|logs>

默认配置:
  OPENCODE_RELAY_URL=$RELAY_URL
  OPENCODE_RELAY_AGENT_ID=$RELAY_AGENT_ID
  OPENCODE_RELAY_MACHINE_ID=$RELAY_MACHINE_ID
  OPENCODE_RELAY_PROJECT_ID=$RELAY_PROJECT_ID
  OPENCODE_RELAY_PROJECT_ROOT=$RELAY_PROJECT_ROOT
  OPENCODE_RELAY_PERMISSION_MODE=$RELAY_PERMISSION_MODE
  OPENCODE_RELAY_TASK_TIMEOUT_MS=$RELAY_TASK_TIMEOUT_MS
EOF
}

running_pid() {
  if [[ -f "$PID_FILE" ]]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      echo "$pid"
      return 0
    fi
  fi
  return 1
}

stop_relay() {
  local pid=""
  if pid="$(running_pid)"; then
    echo "停止 relay 进程: $pid"
    kill "$pid" 2>/dev/null || true
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
  pkill -f "bun run serve-relay.ts" 2>/dev/null || true
  rm -f "$PID_FILE"
}

start_relay() {
  if running_pid >/dev/null 2>&1; then
    echo "relay 已在运行: $(running_pid)"
    return 0
  fi

  mkdir -p "$(dirname "$LOG_FILE")"
  cd "$PROJECT_DIR"

  export OPENCODE_RELAY_URL="$RELAY_URL"
  export OPENCODE_RELAY_OPERATOR_KEY="$RELAY_OPERATOR_KEY"
  export OPENCODE_RELAY_AGENT_ID="$RELAY_AGENT_ID"
  export OPENCODE_RELAY_MACHINE_ID="$RELAY_MACHINE_ID"
  export OPENCODE_RELAY_PROJECT_ID="$RELAY_PROJECT_ID"
  export OPENCODE_RELAY_PROJECT_ROOT="$RELAY_PROJECT_ROOT"
  export OPENCODE_RELAY_PERMISSION_MODE="$RELAY_PERMISSION_MODE"
  export OPENCODE_RELAY_TASK_TIMEOUT_MS="$RELAY_TASK_TIMEOUT_MS"

  if [[ -z "${https_proxy:-}" && -z "${HTTPS_PROXY:-}" ]] && nc -z 127.0.0.1 7897 >/dev/null 2>&1; then
    export https_proxy="http://127.0.0.1:7897"
    export http_proxy="http://127.0.0.1:7897"
    export all_proxy="socks5://127.0.0.1:7897"
  fi

  nohup sh -c 'exec bun run serve-relay.ts' </dev/null >>"$LOG_FILE" 2>&1 &
  local pid=$!
  echo "$pid" >"$PID_FILE"

  echo "relay 已启动: pid=$pid"
  echo "日志文件: $LOG_FILE"
}

relay_status() {
  local pid=""
  if pid="$(running_pid)"; then
    echo "relay 进程在线: pid=$pid"
  else
    echo "relay 进程未运行"
  fi

  local token
  token="$(curl -sS "$API_BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" | sed -n 's/.*\"access_token\":\"\\([^\"]*\\)\".*/\\1/p')"
  if [[ -z "$token" ]]; then
    echo "后端登录失败，无法查询 agent 状态"
    return 0
  fi

  echo "--- /api/agents ---"
  curl -s "$API_BASE/api/agents" -H "Authorization: Bearer $token"
  echo
}

show_logs() {
  if [[ ! -f "$LOG_FILE" ]]; then
    echo "日志文件不存在: $LOG_FILE"
    return 0
  fi
  tail -n 120 "$LOG_FILE"
}

cmd="${1:-restart}"
case "$cmd" in
  start)
    start_relay
    sleep 2
    relay_status
    ;;
  stop)
    stop_relay
    ;;
  restart)
    stop_relay
    start_relay
    sleep 2
    relay_status
    ;;
  status)
    relay_status
    ;;
  logs)
    show_logs
    ;;
  *)
    usage
    exit 1
    ;;
esac
