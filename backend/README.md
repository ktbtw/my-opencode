# relay-server

第一版后端控制中心最小实现。

当前已包含：

- `POST /api/tasks`
- `GET /api/tasks/:taskId`
- `GET /api/tasks/:taskId/events`
- `POST /api/tasks/:taskId/cancel`
- `GET /ws/device`
- `GET /healthz`

## 启动

```bash
go run ./cmd/server
```

默认监听：

```text
:8080
```

## 当前实现范围

- 内存存储
- 单设备在线
- 设备通过 `WebSocket` 主动连接
- 服务端通过 HTTP 创建任务
- 设备回传任务事件

## 后续待补

- PostgreSQL
- JWT 鉴权
- 审批流
- 多设备调度
- 与 `my-opencode` 的 `serve` 真正打通
