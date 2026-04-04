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
      agent_id: "macbook-main:chat-codex",
      machine_id: "macbook-main",
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
    "agent_id":"macbook-main:chat-codex",
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
export OPENCODE_RELAY_AGENT_ID=macbook-main:chat-codex
export OPENCODE_RELAY_MACHINE_ID=macbook-main
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
    "agent_id":"macbook-main:chat-codex",
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

## 4.1 验证任务列表接口与 MySQL 持久化

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

确认 MySQL 建库建表是否完成：

```bash
MYSQL_PWD='ymh20040825' mysql -uroot -e 'SHOW DATABASES LIKE "chat_codex"; USE chat_codex; SHOW TABLES;'
```

再启动一个最小任务设备客户端：

```bash
node -e '
const ws = new WebSocket("ws://127.0.0.1:8080/ws/device");
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: "device.hello",
    request_id: "req_tasks_module_1",
    sent_at: new Date().toISOString(),
    payload: {
      agent_id: "macbook-main:tasks-demo",
      machine_id: "macbook-main",
      hostname: "MacBook-Air",
      version: "0.3.0",
      projects: [{ project_id: "chat-codex", root: "/Users/yuminghao/Downloads/chat-codex" }]
    }
  }));
};
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data.toString());
  if (msg.type !== "task.run") return;
  const base = { request_id: msg.request_id, sent_at: new Date().toISOString() };
  const text = (msg.payload.parts || []).filter((x) => x.type === "text").map((x) => x.text).join("\\n");
  ws.send(JSON.stringify({ type: "task.started", ...base, payload: { task_id: msg.payload.task_id, session_id: "sess_tasks_demo_1" } }));
  ws.send(JSON.stringify({ type: "task.delta", ...base, payload: { task_id: msg.payload.task_id, content: "任务处理中: " + text } }));
  setTimeout(() => {
    ws.send(JSON.stringify({ type: "task.completed", ...base, payload: { task_id: msg.payload.task_id, session_id: "sess_tasks_demo_1", result: "任务完成: " + text } }));
  }, 200);
};
setInterval(() => {
  ws.send(JSON.stringify({
    type: "device.heartbeat",
    request_id: "hb_" + Date.now(),
    sent_at: new Date().toISOString(),
    payload: { agent_id: "macbook-main:tasks-demo", running_task_id: "" }
  }));
}, 5000);
'
```

创建任务：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"macbook-main:tasks-demo",
    "project_id":"chat-codex",
    "parts":[
      {"type":"text","text":"请输出任务模块联调验证"}
    ]
  }'
```

等待任务完成后查询任务详情：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

查询任务列表：

```bash
curl -s 'http://127.0.0.1:8080/api/tasks?agent_id=macbook-main:tasks-demo&status=completed&limit=10'
```

重启后端后再次查询任务列表：

```bash
curl -s 'http://127.0.0.1:8080/api/tasks?agent_id=macbook-main:tasks-demo&status=completed&limit=10'
```

预期结果：

- `chat_codex` 数据库存在
- `tasks`、`task_events` 等表已自动创建
- 任务详情最终返回 `status=completed`
- 任务列表接口可按 `agent_id/status/limit` 过滤
- 重启后端后，任务列表仍能查到刚才的任务记录

本次实测结果：

- 2026-04-04 已验证 `GET /api/tasks` 可返回真实完成任务
- 2026-04-04 已验证任务记录在后端重启后仍可从 MySQL 查回

## 5. 验证后端审批链路

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

再启动一个最小审批设备客户端：

```bash
node -e '
const ws = new WebSocket("ws://127.0.0.1:8080/ws/device");
let currentTask = null;
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: "device.hello",
    request_id: "req_hello_approval_1",
    sent_at: new Date().toISOString(),
    payload: {
      agent_id: "approval-device:chat-codex",
      machine_id: "approval-device",
      hostname: "Approval-Mock",
      version: "0.2.0",
      projects: [{ project_id: "chat-codex", root: "/Users/yuminghao/Downloads/chat-codex" }]
    }
  }));
};
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data.toString());
  if (msg.type === "task.run") {
    currentTask = msg.payload.task_id;
    ws.send(JSON.stringify({
      type: "task.started",
      request_id: "req_started_1",
      sent_at: new Date().toISOString(),
      payload: { task_id: currentTask, session_id: "sess_approval_1" }
    }));
    ws.send(JSON.stringify({
      type: "task.waiting_approval",
      request_id: "req_waiting_1",
      sent_at: new Date().toISOString(),
      payload: {
        task_id: currentTask,
        session_id: "sess_approval_1",
        permission_id: "permission_mock_1",
        permission: "edit",
        patterns: ["/Users/yuminghao/Downloads/chat-codex/approval-check-from-api.txt"],
        metadata: { tool: "write" }
      }
    }));
    return;
  }
  if (msg.type === "task.approval_response") {
    const reply = msg.payload.reply;
    if (reply === "reject") {
      ws.send(JSON.stringify({
        type: "task.failed",
        request_id: "req_failed_1",
        sent_at: new Date().toISOString(),
        payload: { task_id: currentTask, session_id: "sess_approval_1", error: "approval rejected" }
      }));
      return;
    }
    ws.send(JSON.stringify({
      type: "task.approval_applied",
      request_id: "req_applied_1",
      sent_at: new Date().toISOString(),
      payload: { task_id: currentTask, session_id: "sess_approval_1", permission_id: "permission_mock_1", reply }
    }));
    ws.send(JSON.stringify({
      type: "task.completed",
      request_id: "req_completed_1",
      sent_at: new Date().toISOString(),
      payload: { task_id: currentTask, session_id: "sess_approval_1", result: "done" }
    }));
  }
};
setTimeout(() => {}, 120000);
'
```

然后创建审批任务：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"approval-device:chat-codex",
    "project_id":"chat-codex",
    "parts":[{"type":"text","text":"测试审批流"}]
  }'
```

查询任务应进入等待审批：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

提交审批：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>/approval \
  -H 'Content-Type: application/json' \
  -d '{"reply":"once"}'
```

再次查询结果：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

预期结果：

- 第一次查询任务返回 `status=waiting_approval`
- 任务结果中包含 `approval.permission_id`
- 事件流包含 `waiting_approval`
- 审批提交后最终返回 `status=completed`
- 审批后的事件流包含 `approval_requested`
- 审批后的事件流包含 `approval_applied`
- 审批后的事件流包含 `completed`

本次实测结果：

- 2026-04-04 已验证 `waiting_approval -> approval_requested -> approval_applied -> completed`

## 6. 验证自动审批事件链路

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

再启动一个最小自动审批设备客户端：

```bash
node -e '
const ws = new WebSocket("ws://127.0.0.1:8080/ws/device");
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: "device.hello",
    request_id: "req_hello_auto_1",
    sent_at: new Date().toISOString(),
    payload: {
      agent_id: "auto-approval-device:chat-codex",
      machine_id: "auto-approval-device",
      hostname: "Auto-Approval-Mock",
      version: "0.2.0",
      projects: [{ project_id: "chat-codex", root: "/Users/yuminghao/Downloads/chat-codex" }]
    }
  }));
};
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data.toString());
  if (msg.type !== "task.run") return;
  ws.send(JSON.stringify({
    type: "task.started",
    request_id: "req_started_auto_1",
    sent_at: new Date().toISOString(),
    payload: { task_id: msg.payload.task_id, session_id: "sess_auto_1" }
  }));
  ws.send(JSON.stringify({
    type: "task.approval_auto_approved",
    request_id: "req_auto_approved_1",
    sent_at: new Date().toISOString(),
    payload: {
      task_id: msg.payload.task_id,
      session_id: "sess_auto_1",
      permission_id: "permission_auto_1",
      permission: "edit",
      patterns: ["/Users/yuminghao/Downloads/chat-codex/auto-approval-check.txt"]
    }
  }));
  ws.send(JSON.stringify({
    type: "task.completed",
    request_id: "req_completed_auto_1",
    sent_at: new Date().toISOString(),
    payload: { task_id: msg.payload.task_id, session_id: "sess_auto_1", result: "done" }
  }));
};
setTimeout(() => {}, 120000);
'
```

然后创建任务并查询事件流：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"auto-approval-device:chat-codex",
    "project_id":"chat-codex",
    "parts":[{"type":"text","text":"测试自动审批事件"}]
  }'

curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

预期结果：

- 最终返回 `status=completed`
- 事件流包含 `approval_auto_approved`
- 事件流包含 `completed`

本次实测结果：

- 2026-04-04 已验证 `approval_auto_approved -> completed`

## 7. 验证真实 my-opencode serve 的 ask 审批链路

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

再在目录 `/Users/yuminghao/Downloads/chat-codex/my-opencode/packages/opencode` 下启动真实 `serve`：

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
  "agent": {
    "build": {
      "permission": {
        "bash": "ask",
        "edit": "ask",
        "read": "allow",
        "list": "allow",
        "glob": "allow",
        "grep": "allow",
        "todowrite": "allow",
        "task": "allow",
        "question": "allow"
      }
    }
  },
  "model": "xcodebest/gpt-5.4-mini",
  "small_model": "xcodebest/gpt-5.4-mini",
  "logLevel": "DEBUG"
}'
export OPENCODE_RELAY_URL=http://127.0.0.1:8080
export OPENCODE_RELAY_AGENT_ID=macbook-main:chat-codex
export OPENCODE_RELAY_MACHINE_ID=macbook-main
export OPENCODE_RELAY_PROJECT_ID=chat-codex
export OPENCODE_RELAY_PROJECT_ROOT=/Users/yuminghao/Downloads/chat-codex
export OPENCODE_RELAY_PERMISSION_MODE=ask
export OPENCODE_SERVER_PASSWORD=
export OPENCODE_DISABLE_DEFAULT_PLUGINS=1
export OPENCODE_DISABLE_MODELS_FETCH=1
export OPENCODE_DISABLE_AUTOUPDATE=1
export OPENCODE_DISABLE_PROJECT_CONFIG=1
export OPENCODE_DISABLE_CLAUDE_CODE=1
export BUN_INSTALL_CACHE_DIR=/opt/homebrew/lib/node_cache
bun run src/index.ts serve --hostname 127.0.0.1 --port 4096
```

新开终端，先确保测试文件不存在，再创建真实写文件任务：

```bash
rm -f /Users/yuminghao/Downloads/chat-codex/real-ask-approval.txt

curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"macbook-main:chat-codex",
    "project_id":"chat-codex",
    "parts":[
      {
        "type":"text",
        "text":"必须在当前项目根目录创建文件 `real-ask-approval.txt`，内容为 `approved-by-real-ask`，然后只回复 done。不要只描述步骤，必须真正执行。"
      }
    ]
  }'
```

记录返回的 `task_id` 后，先查询任务，确认进入 `waiting_approval`：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
```

审批通过：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>/approval \
  -H 'Content-Type: application/json' \
  -d '{"reply":"once"}'
```

最后检查状态、事件流和文件：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
cat /Users/yuminghao/Downloads/chat-codex/real-ask-approval.txt
```

预期结果：

- 审批前任务状态为 `waiting_approval`
- 事件流包含 `waiting_approval`
- 审批后事件流包含 `approval_requested`
- 审批后事件流包含 `approval_applied`
- 最终任务状态为 `completed`
- 最终 `result` 为 `done`
- 文件 `real-ask-approval.txt` 被真实创建，内容为 `approved-by-real-ask`

本次实测结果：

- 2026-04-04 已验证真实 `serve` 审批链路 `waiting_approval -> approval_requested -> approval_applied -> completed`
- 2026-04-04 已验证文件 `/Users/yuminghao/Downloads/chat-codex/real-ask-approval.txt` 被真实写入，内容为 `approved-by-real-ask`

## 8. 验证多机多 agent 模型下的 agent 注册与按 agent 投递

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

再启动一个最小 agent 客户端：

```bash
node -e '
const ws = new WebSocket("ws://127.0.0.1:8080/ws/device");
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: "device.hello",
    request_id: "req_hello_agent_1",
    sent_at: new Date().toISOString(),
    payload: {
      agent_id: "macbook-main:project-a",
      machine_id: "macbook-main",
      hostname: "MacBook-Pro",
      version: "0.3.0",
      projects: [{ project_id: "project-a", root: "/Users/yuminghao/project-a" }]
    }
  }));
};
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data.toString());
  if (msg.type !== "task.run") return;
  ws.send(JSON.stringify({
    type: "task.started",
    request_id: "req_started_agent_1",
    sent_at: new Date().toISOString(),
    payload: { task_id: msg.payload.task_id, session_id: "sess_agent_1" }
  }));
  ws.send(JSON.stringify({
    type: "task.delta",
    request_id: "req_delta_agent_1",
    sent_at: new Date().toISOString(),
    payload: { task_id: msg.payload.task_id, content: "agent working" }
  }));
  setTimeout(() => {
    ws.send(JSON.stringify({
      type: "task.completed",
      request_id: "req_completed_agent_1",
      sent_at: new Date().toISOString(),
      payload: { task_id: msg.payload.task_id, session_id: "sess_agent_1", result: "done-by-agent" }
    }));
  }, 200);
};
setTimeout(() => {}, 120000);
'
```

查询在线 agent：

```bash
curl -s http://127.0.0.1:8080/api/agents
```

然后按 `agent_id` 创建任务：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"macbook-main:project-a",
    "project_id":"project-a",
    "parts":[{"type":"text","text":"请只回复 done-by-agent"}]
  }'
```

记录返回的 `task_id` 后，查询任务和事件流：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

预期结果：

- `GET /api/agents` 返回在线 `agent`
- agent 列表中包含 `agent_id`、`machine_id`、`projects`
- 创建任务接口必须使用 `agent_id`
- 任务结果中包含 `agent_id`
- 任务结果中包含 `machine_id`
- 任务结果中包含 `project_root`
- 最终状态为 `completed`
- 最终 `result` 为 `done-by-agent`

本次实测结果：

- 2026-04-04 已验证 `GET /api/agents` 返回在线 agent
- 2026-04-04 已验证 `POST /api/tasks` 按 `agent_id` 投递成功

## 9. 验证真实 my-opencode serve 使用 agent 协议注册与投递

先在目录 `/Users/yuminghao/Downloads/chat-codex/backend` 下启动后端：

```bash
go run ./cmd/server
```

再在目录 `/Users/yuminghao/Downloads/chat-codex/my-opencode/packages/opencode` 下启动真实 `serve`：

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
  "agent": {
    "build": {
      "permission": {
        "bash": "ask",
        "edit": "ask",
        "read": "allow",
        "list": "allow",
        "glob": "allow",
        "grep": "allow",
        "todowrite": "allow",
        "task": "allow",
        "question": "allow"
      }
    }
  },
  "model": "xcodebest/gpt-5.4-mini",
  "small_model": "xcodebest/gpt-5.4-mini",
  "logLevel": "DEBUG"
}'
export OPENCODE_RELAY_URL=http://127.0.0.1:8080
export OPENCODE_RELAY_AGENT_ID=macbook-main:chat-codex
export OPENCODE_RELAY_MACHINE_ID=macbook-main
export OPENCODE_RELAY_PROJECT_ID=chat-codex
export OPENCODE_RELAY_PROJECT_ROOT=/Users/yuminghao/Downloads/chat-codex
export OPENCODE_RELAY_PERMISSION_MODE=ask
export OPENCODE_SERVER_PASSWORD=
export OPENCODE_DISABLE_DEFAULT_PLUGINS=1
export OPENCODE_DISABLE_MODELS_FETCH=1
export OPENCODE_DISABLE_AUTOUPDATE=1
export OPENCODE_DISABLE_PROJECT_CONFIG=1
export OPENCODE_DISABLE_CLAUDE_CODE=1
export BUN_INSTALL_CACHE_DIR=/opt/homebrew/lib/node_cache
bun run src/index.ts serve --hostname 127.0.0.1 --port 4096
```

确认在线 agent：

```bash
curl -s http://127.0.0.1:8080/api/agents
```

然后按 `agent_id` 创建一个真实文本任务：

```bash
curl -s http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"macbook-main:chat-codex",
    "project_id":"chat-codex",
    "parts":[{"type":"text","text":"请只回复: ok-agent"}]
  }'
```

记录返回的 `task_id` 后，查询结果和事件流：

```bash
curl -s http://127.0.0.1:8080/api/tasks/<TASK_ID>
curl -sN http://127.0.0.1:8080/api/tasks/<TASK_ID>/events
```

预期结果：

- `GET /api/agents` 返回 `agent_id=macbook-main:chat-codex`
- agent 列表中包含 `machine_id=macbook-main`
- 创建任务返回中包含 `agent_id`
- 创建任务返回中包含 `project_root`
- 最终任务状态为 `completed`
- 最终 `result` 为 `ok-agent`

本次实测结果：

- 2026-04-04 已验证真实 `serve` 用 `agent_id + machine_id` 注册成功
- 2026-04-04 已验证真实文本任务 `task_1775279062624247000` 返回 `result=ok-agent`
