#!/bin/bash
# chat-codex 后端部署脚本
# 用法: ./deploy.sh

set -e

SERVER="root@114.66.33.149"
REMOTE_DIR="/opt/chat-codex/backend"
BINARY="chat-codex-server"
SERVICE="chat-codex-backend"

echo "=== [1/4] 交叉编译后端 ==="
cd "$(dirname "$0")/backend"
GOOS=linux GOARCH=amd64 go build -o "$BINARY" ./cmd/server/main.go
echo "编译完成: $(ls -lh $BINARY | awk '{print $5, $9}')"

echo ""
echo "=== [2/4] 上传二进制到服务器 ==="
scp "$BINARY" "$SERVER:$REMOTE_DIR/$BINARY"
echo "上传完成"

echo ""
echo "=== [3/4] 重启服务 ==="
ssh "$SERVER" "
  chmod +x $REMOTE_DIR/$BINARY
  systemctl restart $SERVICE
  sleep 2
  systemctl status $SERVICE --no-pager | head -10
"

echo ""
echo "=== [4/4] 接口验证 ==="
sleep 1
RESULT=$(curl -s --max-time 5 http://114.66.33.149:8888/api/auth/login \
  -X POST -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123456"}')
if echo "$RESULT" | grep -q "access_token"; then
  echo "接口验证成功"
  echo "服务地址: http://114.66.33.149:8888"
else
  echo "接口验证失败，请检查日志:"
  echo "  ssh $SERVER journalctl -u $SERVICE -n 20"
  exit 1
fi
