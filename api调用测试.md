# OpenCode API 调用测试

## 1. 直接验证供应商 OpenAI 兼容接口

```bash
curl -sS https://api.xcode.best/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <YOUR_API_KEY>' \
  -d '{"model":"gpt-5.4-mini","messages":[{"role":"user","content":"请只回复: ok"}]}'
```

预期结果：

- 返回 `chat.completion`
- `choices[0].message.content` 为 `ok`

## 2. 验证后端 relay-server 健康检查

在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下执行：

```bash
go run ./cmd/server
```

新开终端执行：

```bash
curl -s http://127.0.0.1:8080/healthz
```

预期结果：

- 返回 `{"status":"ok"}`

## 3. 验证设备连接与 `parts` 任务下发最小链路

在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下，先启动服务：

```bash
go run ./cmd/server
```

再启动一个最小设备客户端：

```bash
node -e '
const ws = new WebSocket("ws://127.0.0.1:8080/ws/device");
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: "device.hello",
    request_id: "req_hello_1",
    sent_at: new Date().toISOString(),
    payload: {
      device_id: "macbook-main",
      hostname: "MacBook-Pro",
      version: "0.1.0",
      projects: [{ project_id: "chat-codex", root: "/Users/yuminghao/Downloads/chat-codex" }]
    }
  }));
};
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data.toString());
  if (msg.type !== "task.run") return;
  const base = { request_id: msg.request_id, sent_at: new Date().toISOString() };
  ws.send(JSON.stringify({ type: "task.started", ...base, payload: { task_id: msg.payload.task_id, session_id: "sess_demo_1" } }));
  const text = (msg.payload.parts || []).filter((x) => x.type === "text").map((x) => x.text).join("\\n");
  ws.send(JSON.stringify({ type: "task.delta", ...base, payload: { task_id: msg.payload.task_id, content: "正在处理: " + text } }));
  setTimeout(() => {
    ws.send(JSON.stringify({ type: "task.completed", ...base, payload: { task_id: msg.payload.task_id, session_id: "sess_demo_1", result: "已完成任务: " + text } }));
  }, 200);
};
'
```

然后创建任务：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "device_id":"macbook-main",
    "project_id":"chat-codex",
    "parts":[
      {"type":"text","text":"继续实现设备连接模块"},
      {"type":"file","mime":"image/png","filename":"demo.png","url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7Z0XQAAAAASUVORK5CYII="}
    ]
  }'
```

记录返回的 `task_id` 后，查询状态：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
```

查询事件流：

```bash
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

预期结果：

- 创建任务后返回 `status=dispatched`
- 创建任务返回结果中包含 `parts`
- 查询任务最终返回 `status=completed`
- 查询任务结果中包含 `session_id`
- 事件流顺序包含 `dispatched`
- 事件流顺序包含 `started`
- 事件流顺序包含 `delta`
- 事件流顺序包含 `completed`

补充说明：

- 当前后端 `parts` 已对齐 `opencode` 输入结构
- 可继续透传 `agent`、`subtask`、`file.source`

## 4. 验证改造后的 OpenCode serve relay 最小链路

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

再在目录 `/Users/yuminghao/Downloads/chat-codex/my-opencode/packages/opencode` 下启动改造后的 `serve`：

```bash
export OPENCODE_CONFIG_CONTENT='{
  "enabled_providers": ["xcodebest"],
  "provider": {
    "xcodebest": {
      "name": "xcodebest",
      "api": "https://api.xcode.best/v1",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "apiKey": "<YOUR_API_KEY>",
        "baseURL": "https://api.xcode.best/v1"
      },
      "models": {
        "gpt-5.4-mini": {
          "name": "gpt-5.4-mini",
          "id": "gpt-5.4-mini",
          "tool_call": true,
          "modalities": {
            "input": ["text"],
            "output": ["text"]
          }
        }
      }
    }
  },
  "model": "xcodebest/gpt-5.4-mini",
  "small_model": "xcodebest/gpt-5.4-mini"
}'
export OPENCODE_RELAY_URL=http://127.0.0.1:8080
export OPENCODE_RELAY_DEVICE_ID=macbook-main
export OPENCODE_RELAY_PROJECT_ID=chat-codex
export OPENCODE_RELAY_PROJECT_ROOT=/Users/yuminghao/Downloads/chat-codex
export OPENCODE_SERVER_PASSWORD=
export OPENCODE_DISABLE_DEFAULT_PLUGINS=1
export OPENCODE_DISABLE_MODELS_FETCH=1
export OPENCODE_DISABLE_AUTOUPDATE=1
export OPENCODE_DISABLE_PROJECT_CONFIG=1
export OPENCODE_DISABLE_CLAUDE_CODE=1
export BUN_INSTALL_CACHE_DIR=/opt/homebrew/lib/node_cache
bun run src/index.ts serve --hostname 127.0.0.1 --port 4096
```

然后创建一个仅文本任务：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "device_id":"macbook-main",
    "project_id":"chat-codex",
    "parts":[
      {"type":"text","text":"请只回复: ok"}
    ]
  }'
```

记录返回的 `task_id` 后，查询结果：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

预期结果：

- 任务返回 `status=completed`
- 结果中包含合法的 `session_id`
- 结果 `result` 中包含 `ok`
- 事件流顺序包含 `dispatched`
- 事件流顺序包含 `started`
- 事件流顺序包含至少一个 `delta`
- 事件流顺序包含 `completed`

本次实测结果：

- 2026-04-04 已验证供应商接口返回 `ok`
- 2026-04-04 已验证 `backend` 健康检查通过
- 2026-04-04 已验证 `serve relay` 端到端链路通过
