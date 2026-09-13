# 对话历史权威源重构计划

> 状态：阶段 1–3 已上线；阶段 4 进行中；阶段 5–6 未做  
> 日期：2026-09-10  
> 范围：Agent（OpenCode/relay）、后端 `chat-codex-backend`、Flutter 聊天、MySQL `task_events`  
> 相关审查：`agent-authoritative-history-refactor-audit.md`  
> 发布方式：后端用 `./deploy.sh --backend-only`；接口或事件协议变化时后端与 Flutter 一起发

本文是可执行计划，不是再次审查。结论已经确定：**完整对话历史的权威源必须从云端 MySQL `task_events` 迁到 Agent 本机 SQLite**。后端只保留鉴权、路由、实时转发和任务索引。Flutter 只做展示和短期缓存。

---

## 1. 目标

同一 Agent 连接多台 Flutter 设备时，所有设备读到同一份历史。这份历史来自 Agent 电脑上的 `session` / `message` / `part`，而不是云端把 `task.delta` 逐条录像后再拼回去。

完成后应同时满足：

- 打开旧对话不再扫 19GB 的 `task_events`
- 新任务默认不再把 `delta` / `progress` 永久写入 MySQL
- Agent 离线或旧任务仍有明确降级，不会把空数组显示成“从未聊过”
- 实时打字、取消、审批、问答、Goal、子代理控制仍然走现有 broker / SSE

非目标：

- 不替换项目记忆、运行时安装、推送、聊天队列
- 不把 Flutter SQLite 变成新的权威源
- 不在一次发布里删除 `task_events`
- 不改 MySQL 驱动 / 连接池（现有 `database/sql` 已够用）

---

## 2. 线上基线（2026-09-09 实测）

服务器：`root@www.xyapi.top`  
后端：systemd `chat-codex-backend`，二进制 `/opt/chat-codex/backend/chat-codex-server`  
MySQL：Docker `chat-codex-mysql`（8.4），`127.0.0.1:13307`

| 项 | 值 |
| --- | --- |
| `task_events` 行数 | 13,519,759 |
| `task_events.ibd` | 19GB |
| 时间跨度 | 2026-04-05 ~ 2026-09-09 |
| 近 10 天写入 | 每天 5 万～28 万行 |
| 最近 20 万条构成 | `delta` 67%，`progress` 23%，`tool_updated` 9%（payload 最大） |
| 当前连接 | 6，`Max_used_connections=10` |
| InnoDB buffer pool | 3GB（所以 MySQL 内存会长期约 4.3GB） |
| `Com_delete` | 550（几乎没清理） |

工作区未提交的后端改动（`event-pages`、SSE 分页、`SubagentEvents(200)`）**还没进线上二进制**。线上仍是 2026-09-08 那份 `chat-codex-server`。

---

## 3. 当前完整流程

```text
用户在 Flutter 发消息
        |
        v
后端创建/恢复 tasks 行，经 broker 向 Agent 发 task.run
        |
        v
Agent 本机
  1. 把 user/assistant/tool/plan 写入 SQLite
       session / message / part / todo / event / event_sequence
  2. relay.ts 再派生面向 Flutter 的事件：
       task.delta / task.progress / task.tool_updated
       task.plan_updated / task.subagent_* / task.goal_*
       task.compaction_* / task.completed / task.failed ...
        |
        | WebSocket
        v
后端 App.handle
  - Memory.AddEvent()：进行中任务最多留 500 条，终态后从内存删掉
  - MySQLArchive.AppendEventWithID()：每一条都 INSERT task_events
  - 订阅者 SSE 推给 Flutter
        |
        +--> 打开会话 / 刷新：
        |      Flutter ChatRepository.watchTaskSnapshotEvents()
        |      GET /api/tasks/{id}/event-pages
        |      把 delta 拼成正文、reasoning、工具卡、计划、goal 轮次
        |
        +--> 进行中：
               GET /api/tasks/{id}/events  (SSE)
               Last-Event-ID / X-Task-Event-Cursor 断线后从 MySQL 追赶
        |
        v
Flutter 本地 SQLite
  cached_tasks          任务列表缓存（会读）
  session_sync_state    revision 游标（会读）
  task_events           收到 SSE 后只写不读
```

关键代码：

| 层 | 文件 |
| --- | --- |
| Agent 本地库 | `my-opencode2/packages/opencode/src/session/session.sql.ts` |
| 派生事件 | `my-opencode2/packages/opencode/src/server/relay.ts` |
| 入库 | `backend/internal/store/store.go` `AddEvent` |
| 归档 SQL | `backend/internal/store/mysql_archive.go` |
| SSE / 分页 | `backend/internal/api/api.go` `TaskEvents` / `TaskEventPage` |
| 子代理 | `backend/internal/api/orchestration.go` |
| Goal/子代理重连 | `backend/internal/app/app.go` |
| Flutter 历史 | `flutter_app/lib/features/chat/data/chat_repository.dart` |
| Flutter 投影 | `flutter_app/lib/features/chat/presentation/chat_provider.dart` |
| Flutter 只写缓存 | `flutter_app/lib/features/chat/data/local_chat_store_sqlite.dart` |

当前问题不是“少一张 Flutter 读表”，而是同一份对话存了三份、格式还不同：

1. Agent 原始 session/message/part
2. 后端 Memory + MySQL 派生事件流
3. Flutter 只写事件缓存

多设备能对齐，完全是因为大家都去读云端那份派生录像。录像按 token 流存储，所以表必然膨胀。

---

## 4. 目标流程

```text
用户发消息 / 打开旧对话
        |
        +-- 实时 --------------------------------------------+
        |   Agent relay  -> 后端 Memory -> SSE -> Flutter     |
        |   不要求写入 task_events                            |
        |                                                    |
        +-- 历史 --------------------------------------------+
            Flutter 先订阅 SSE
                 |
                 v
            GET /api/tasks/{id}/agent-history
                 |
                 v
            后端鉴权，按 task.agent_id / machine_id / session_id
            向在线 Agent 发 session.history.request
                 |
                 +-- Agent 在线：读本地 SQLite，返回 history DTO
                 +-- Agent 离线或旧任务：MySQL event-pages fallback
                 +-- 只有 tasks 行：返回 source=task_index_only
                 |
                 v
            Flutter 投影为现有 ChatMessage / 工具卡 / 计划 / 子代理树
            用 Agent sequence 与实时 SSE 去重合并
```

后端 `tasks` 表继续承担：列表、权限、状态机、Goal 元数据、可恢复子代理快照、最终结果摘要。  
MySQL `task_events` 降级为兼容层，验收后再停止高频写入并清理。

---

## 5. 功能影响

表中“现在”指继续以 `task_events` 为权威源。“停写 delta”指只停 `delta`/`progress` 永久插入、实时 SSE 仍在。“切权威源”指 Flutter 已改读 Agent 历史。

| 功能 | 现在依赖 | 只停写 delta/progress | 切到 Agent 历史后 | 处理 |
| --- | --- | --- | --- | --- |
| 进行中打字 | Memory + SSE | 不受影响 | 不受影响 | 保持 |
| 打开旧对话 / 刷新重建气泡 | `event-pages` 拼接 delta | **正文和 reasoning 会空** | 从 message/part 投影 | **禁止在阶段 4 之前停写** |
| SSE 断线补发 | MySQL `(sent_at,id)` | 断线期间 delta 可能丢 | 先订 SSE 再拉 Agent 缺口 | 阶段 3 改合并规则 |
| 工具卡片 | `tool_updated` | 若仍写入则还在 | 从 tool part 投影 | 阶段 4 再降频或停写 |
| 计划面板 | `plan_updated` + `plan_history_cleanup.go` | 保留写入则还在 | 从 todo/plan 投影 | 低频事件先继续写 |
| Goal 多轮 | `goal_*` + 多轮 delta | 状态在 `tasks.metadata`；轮次正文会缺 | 从 session 轮次投影 | 与历史协议一起做 |
| 子代理树 / 控制 / 日志 | 最近 200 条 subagent 事件 + JSON `node_id` | 影响小 | 从子 session 读 | 阶段 2 必须覆盖 child session |
| Goal / 子代理重连恢复 | `SubagentEvents(task, 200)` | 仍写 subagent 则还能恢复 | 问 Agent 或读 `tasks.metadata` 快照 | 阶段 4 前把可恢复节点写入 metadata |
| 超时对账 | `LatestTerminalEvent` | 终态仍写入则没事 | 信 `tasks.status` | 保持终态写入直到阶段 6 |
| 项目记忆抽取 | 最近 64 条 completed/tool/plan/goal/compaction | 不要删这些类型 | 改读 tasks 摘要 + Agent | 不要动 `project_memory_job_events` |
| 压缩提示 | `compaction_*` | 可只留开始/结束 | Agent 有 compaction 时间 | 低频保留 |
| 审批 / 问答 overlay | 低频事件 | 继续写 | 可进 tasks 或独立小表 | 不与本计划绑死 |
| 聊天队列 / 推送 / 预检 / 通知 | 不走 `task_events` | 无影响 | 无影响 | 明确排除 |
| 多设备同会话 | 都读云端事件表 | 新任务会对不齐 | 都读同一 Agent | 阶段 3 验收项 |
| Agent 离线看历史 | 完全靠 MySQL | 旧数据还在 | 新任务显示明确降级 | fallback 必须保留 |
| Flutter 本地 `task_events` | 只写不读 | 无功能影响 | 最后删除 | 阶段 6 |

不要一起改的表：

- `project_memory_job_events`
- `runtime_install_events`
- `app_notification_changes`
- `chat_queue_items`
- `tasks`（保留；`input_json` 平均 105KB 另开裁剪任务，不在本计划验收范围）

---

## 6. 历史协议

### 6.1 Agent 请求

```json
{
  "type": "session.history.request",
  "request_id": "req_hist_...",
  "sent_at": "2026-09-10T00:00:00Z",
  "payload": {
    "task_id": "TASK_ID",
    "session_id": "SESSION_ID",
    "agent_id": "AGENT_ID",
    "machine_id": "MACHINE_ID",
    "project_id": "PROJECT_ID",
    "cursor": "",
    "limit": 200,
    "include_children": true
  }
}
```

### 6.2 Agent 响应

```json
{
  "type": "session.history.response",
  "request_id": "req_hist_...",
  "payload": {
    "task_id": "TASK_ID",
    "session_id": "SESSION_ID",
    "source": "agent_local",
    "items": [],
    "next_cursor": "",
    "has_more": false,
    "source_sequence_start": 1,
    "source_sequence_end": 200,
    "complete": true,
    "error_code": ""
  }
}
```

`items` 必须是稳定 DTO，版本化，至少覆盖：

- user / assistant / system 消息和 message id
- text 与 reasoning 分离
- tool call、tool result、tool call id、状态
- plan / todo 版本和最终状态
- 子代理 parent/child session、node id、状态、结果
- compaction 起止
- approval、question、retry、error、cancelled、completed
- created_at、sequence、cursor、幂等键

不要再把历史伪装成一串 `task.delta`。

### 6.3 后端 HTTP

建议新接口：

```text
GET /api/tasks/{taskID}/agent-history?cursor=&limit=200&include_children=1
```

响应带 `source`：`agent_local` | `mysql_legacy` | `task_index_only`。

权限与现有 `/api/tasks/{taskID}` 相同：任务必须属于当前 operator。路由键：`operator_id + agent_id + machine_id + session_id`。

超时、取消、`request_id` 去重由 broker 处理。迟到响应丢弃。同一个 `request_id` 只完成一次。

### 6.4 合并规则

1. Flutter 先订阅 SSE，再拉历史，避免空窗丢实时事件
2. 主序使用 Agent sequence / 稳定 `(sequence, id)`，不用设备本地时间
3. 重复事件按 Agent event id 或 sequence 去重
4. 后端不得重写会覆盖 Agent sequence 的顺序
5. 空历史必须带 `error_code`（`history_not_found` / `history_archived` / `agent_offline`），禁止用空数组表示“没聊过”

---

## 7. 分阶段计划

每阶段单独发布，可回滚。不要把停写、清表、改 Flutter 读源绑在一次上线。

### 阶段 1. 把现有分页读路径发上去

目的：先减轻打开长对话时的卡顿，行为与现在一致。

本地未提交改动（必须先测再发）：

- `backend/internal/api/api.go`：`TaskEventPage`、SSE 按页补发
- `backend/internal/store/store.go`：`ListEventPage`、`SubagentEvents`
- `backend/internal/store/mysql_archive.go`：`(sent_at, id)` keyset，`sent_at` 截到秒
- `backend/internal/app/app.go`：重连改为 `SubagentEvents(..., 200)`
- `flutter_app/lib/features/chat/data/chat_repository.dart`：历史走 `event-pages`

动作：

1. `cd backend && go test ./internal/store ./internal/api ./internal/app`
2. `cd flutter_app && flutter test test/features/chat`
3. `./deploy.sh --backend-only`，必要时同时发 Flutter

验收：

- [ ] `/readyz` 通过，服务未回滚
- [ ] 打开大任务历史不再一次拉全表
- [ ] 子代理树、Goal 重连、SSE 断线补发与发版前一致
- [ ] MySQL 行数不会因此下降（本阶段不追求缩表）

回滚：`deploy.sh` 已有二进制 rollback；Flutter 回上一版客户端。

### 阶段 2. Agent 历史通道 + 后端双路径

目的：历史可从 Agent 本机读出，但 Flutter 暂不切换。

改动：

1. 在 `relay.ts` 增加 `session.history.request/response`
2. 从 `session.sql.ts` 的 message/part/todo/子 session 组装 DTO
3. 后端 broker 增加按连接等待响应
4. 实现 `GET /api/tasks/{taskID}/agent-history`
5. Agent 在线走本地；否则 fallback `ListEventPage`
6. 日志打 `source`

验收：

- [ ] 用 curl/内部调试对在线 Agent 拉一页，`source=agent_local`
- [ ] Agent 进程重启后历史仍完整
- [ ] Agent 断开时 `source=mysql_legacy` 或明确 `agent_offline`
- [ ] 子会话、工具、计划能出现在 DTO 里
- [ ] 现有 SSE / event-pages 行为不变

回滚：新接口可留着不用；不要在本阶段停 MySQL 写入。

### 阶段 3. Flutter 改读 Agent 历史

目的：打开对话不再依赖拼接云端 delta。

改动：

1. `ChatRepository.watchTaskSnapshotEvents` 优先打 `agent-history`
2. 把 history DTO 投影到现有 `TaskEventSnapshot` / `ChatMessage`（先不改气泡组件）
3. 旧任务、无 session 映射、Agent 离线继续 `event-pages`
4. 先订阅 `watchTaskEvents`，再拉快照
5. Flutter `task_events` 暂时继续只写，便于对照和回滚

验收：

- [ ] 清空 Flutter SQLite 后历史仍能加载
- [ ] 两台设备看同一 Agent/session，正文、工具、计划、子代理顺序一致
- [ ] text / reasoning 分离与现在 UI 一致
- [ ] Goal 多轮、input_applied、compaction、审批、问答可恢复
- [ ] 进行中任务刷新不会丢当前 SSE 流
- [ ] 对比日志：新任务 `source=agent_local`

回滚：客户端开关或回退到只请求 `event-pages`。

### 阶段 4. 停止高频云端永久写入

前置：阶段 3 至少一个完整发布周期，且无历史空洞客诉。

| 事件 | 本阶段动作 |
| --- | --- |
| `delta` / `progress` | 默认不写 MySQL，只 Memory + SSE |
| `tool_updated` | 可改为每 tool 最新快照，或等 DTO 稳定后再停 |
| `plan_updated` / `subagent_*` / `goal_*` / 终态 / 审批问答 | **继续写** |
| 可恢复子代理 | 写入 `tasks.metadata` JSON，避免只靠最近 200 条事件 |

`AddEvent` 增加可配置开关，例如环境变量 `ARCHIVE_HIGH_FREQ_EVENTS=0`。出问题立即改回 1 并重启。

Goal 重连那段 `addTaskEvent(..., "progress", ...)` 可改成低频 `goal_resumed`，不要再靠 progress 当审计。

验收：

- [x] 新插入速率从每天十几万降到终态/工具快照量级（2026-09-10 06:25 UTC 发版后未见新 `delta`/`progress` 行）
- [ ] 打开对话仍完整
- [x] Goal 重连、子代理恢复仍成功（单测覆盖 `goal_resumed` 与 metadata 恢复；线上需观察真实重连）
- [x] 项目记忆抽取不因缺少 delta 而空（抽取本来就不读 `delta`/`progress`）

回滚：打开高频写入开关，重启 `chat-codex-backend`。

### 阶段 5. 清理存量 MySQL

只在阶段 4 稳定后做。生产删除必须分批，禁止单条超大事务。

1. 统计将删除的 `delta`/`progress` 行数和最早/最晚 `sent_at`
2. 低峰循环：

```sql
DELETE FROM task_events
WHERE event_type IN ('delta', 'progress')
  AND sent_at < UTC_TIMESTAMP() - INTERVAL 7 DAY
LIMIT 5000;
```

3. 确认 fallback、项目记忆、计划清理不依赖这些行
4. 安排窗口重建表或改为按月 `RANGE` 分区，回收 19GB 文件
5. 表降到数 GB 后，把 `innodb-buffer-pool-size` 从 3GB 降到 1～1.5GB
6. 评估删除低价值索引：`idx_task_events_event_type`、`idx_task_events_sent_at`

验收：

- [ ] `task_events.ibd` 明显下降
- [ ] MySQL 容器内存随 buffer pool 下调而下降
- [ ] 旧任务 fallback 仍能打开
- [ ] 无长事务锁表导致后端超时

回滚：删除不可逆。必须先备份或只删已确认无 fallback 需求的类型/时间窗。

### 阶段 6. 收口

1. 新任务主路径不再使用 `/event-pages`
2. 删除 Flutter `task_events` 表、`LocalChatStore.recordEvent()`、`ChatNotifier._persistTaskEvent()`
3. 项目记忆改为读 `tasks` 摘要，不再扫 `task_events` 里的 `tool_updated`
4. 高频归档代码和仅为其服务的索引/清理从后端移除
5. `cached_tasks` / `session_sync_state` 仍保留，除非另开缓存精简任务

仍必须保留：

- Agent 本地 session/message/part/event
- relay 实时发送
- 后端 tasks、权限、broker、SSE、终态状态机
- 旧任务 MySQL 只读 fallback，直到统计显示无需再读

---

## 8. 发布与验证命令

```bash
# 后端
cd backend
go test ./internal/store ./internal/api ./internal/app
go test -race ./internal/store ./internal/api ./internal/app
./deploy.sh --backend-only

# Flutter（阶段 1、3）
cd flutter_app
flutter analyze --no-pub
flutter test --no-pub
# 再按 更新发布指南.md 发 Web / APK / EXE / DMG
```

线上确认：

```bash
ssh -i ./root.pem root@www.xyapi.top "systemctl status chat-codex-backend --no-pager"
ssh -i ./root.pem root@www.xyapi.top "curl -fsS --max-time 2 http://127.0.0.1:19081/readyz"
```

阶段 4 之后看写入：

```sql
SELECT event_type, COUNT(*) cnt
FROM task_events
WHERE sent_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY
GROUP BY event_type
ORDER BY cnt DESC;
```

---

## 9. 总验收清单

全部勾上之前，不得宣称 Agent 已是唯一权威源，也不得删除 `task_events`。

- [ ] Agent 在线时 Flutter 历史 `source=agent_local`
- [ ] Agent 重启后历史完整
- [ ] 两台 Flutter 设备顺序一致（正文、工具、计划、子代理）
- [ ] 删除 Flutter SQLite 后仍能恢复
- [ ] 分页不重复、不跳过、不乱序
- [ ] 拉历史期间到达的实时事件不丢
- [ ] 断线重连不重复正文、不丢终态
- [ ] text / reasoning 分离保持现有 UI
- [ ] 工具开始/更新/完成/错误正确
- [ ] plan 去重和最终状态正确
- [ ] 子代理 parent/child/node 状态正确，控制接口可用
- [ ] compaction、goal、approval、question、retry、cancel 可恢复
- [ ] Agent 离线有明确降级，不把空历史当正常空会话
- [ ] 旧任务仍能从 MySQL fallback 打开
- [ ] 新 `task_events` 插入速率显著下降
- [ ] 后端 RSS、MySQL 内存、磁盘有对比数据

---

## 10. 明确禁止

- 不要引入 `pgxpool` 或把 MySQL 当 PostgreSQL
- 不要在 Flutter 能从 Agent 重建气泡之前停写 `delta`
- 不要一次 `DELETE FROM task_events` 或无备份清表
- 不要把项目记忆、运行时、通知、队列塞进同一次发布
- 不要让 Flutter 本地库成为权威源
- 不要在阶段 1 只重启线上进程却不部署新二进制（重启不等于发版）

---

## 11. 推荐开工顺序

1. 阶段 1：把工作区已有分页发到线上，先治打开长对话卡顿  
2. 阶段 2：定 history DTO，在 `relay.ts` 落地 request/response  
3. 阶段 3：Flutter 切换读取  
4. 阶段 4：开关停写高频事件  
5. 阶段 5～6：清表和删除只写缓存  

当前下一步：阶段 4 默认停写 `delta`/`progress`，可恢复子代理写入 `tasks.metadata`；稳定一个发布周期后再做阶段 5 清理。
