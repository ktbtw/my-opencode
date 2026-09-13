# Agent 权威对话历史重构审查报告

> 审查对象：`chat-codex` 当前 Agent、后端 relay、MySQL archive、Flutter 对话链路
>
> 审查目的：确认将“完整对话历史”从云端 MySQL `task_events` 迁移到 Agent 电脑本地后，哪些模块保留、重构、废弃或删除，并给出可回滚的迁移顺序。
>
> 审查结论：建议迁移，但不建议一次性删除 `task_events`。应先让 Agent 本地 SQLite 成为历史权威源，后端变成鉴权、路由、实时转发和任务索引层，MySQL 进入兼容回退与短期缓存阶段，验证完成后再停止高频写入和清理旧表。

## 1. 结论摘要

目标架构如下：

```text
Agent 本地 SQLite
  session / message / part / todo / event / event_sequence
          |
          | history request + realtime relay
          v
后端 broker / API
  鉴权、Agent 路由、任务索引、实时 SSE、短期兼容回退
          |
          +--> Flutter 设备 A
          +--> Flutter 设备 B
          +--> 其他 Flutter 设备
```

这样同一个 Agent 连接多个 Flutter 设备时，所有设备读取同一份 Agent 历史，不会因为每台设备分别记录事件而产生多个副本。

当前实现不是“Flutter 已经完整本地存储”：

- Flutter `cached_tasks` 是任务列表和 payload 的启动缓存。
- Flutter `session_sync_state` 是 revision 游标缓存。
- Flutter `task_events` 收到 SSE 后会写入，但没有读取历史的接口，实际是只写不读的去重/审计缓存。
- Flutter 历史恢复仍由 `ChatRepository.watchTaskSnapshotEvents()` 请求后端 `/api/tasks/{taskID}/event-pages`。
- 后端收到 Agent 的 `task.*` relay 事件后，同时写入 Memory 和 MySQL `task_events`。

因此，重构重点不是把 Flutter 的“完整历史”搬走，而是把后端当前承担的“完整事件归档”职责，改成由 Agent 本地会话存储承担。

## 2. 当前实现审计

### 2.1 当前数据流

```text
Agent 本地
  session/message/part/todo/event/event_sequence
        |
        | relay.ts 生成派生事件
        | task.started / task.delta / task.progress / task.tool_updated
        | task.plan_updated / task.subagent_* / task.compaction_*
        | task.completed / task.failed / goal 事件等
        v
后端 WebSocket broker
        |
        +--> Memory.AddEvent()
        |      进程内事件订阅和 SSE 实时分发
        |
        +--> MySQLArchive.AppendEventWithID()
               INSERT INTO task_events
        |
        v
Flutter SSE / event-pages
        |
        +--> 当前 ChatNotifier 更新界面
        +--> Flutter SQLite task_events（只写，不参与历史恢复）
```

### 2.2 Agent 本地已经具备的能力

Agent 数据库已有完整会话实体：

- `session`：会话、项目、父子会话、标题、模型、token 和时间信息。
- `message`：用户消息、助手消息等消息级信息。
- `part`：正文、reasoning、工具调用、工具结果等消息片段。
- `todo`：计划/待办状态。
- `event`：有序同步事件。
- `event_sequence`：每个 aggregate 的稳定递增序号。

关键实现：

- `my-opencode2/packages/opencode/src/session/session.sql.ts`
- `my-opencode2/packages/opencode/src/sync/event.sql.ts`
- `my-opencode2/packages/opencode/src/sync/README.md`
- `my-opencode2/packages/opencode/src/server/routes/instance/httpapi/handlers/session.ts`
- `my-opencode2/packages/opencode/src/server/routes/instance/httpapi/handlers/sync.ts`

`sync/README.md` 明确采用“一台设备写入，多台设备同步”的事件源设计，和本次目标一致。Agent 还已有 session message 查询、sync history/replay 和本地数据库重启恢复能力，所以不需要重新设计一套 Flutter 侧事件数据库来充当权威源。

### 2.3 Agent relay 当前职责

文件：`my-opencode2/packages/opencode/src/server/relay.ts`

relay 当前做两件事：

1. 执行 Agent 任务并使用本地 session 服务持久化消息、part、工具状态和子会话。
2. 把面向 Flutter 的派生状态事件通过 WebSocket 发给后端。

典型事件包括：

- 文本和 reasoning 增量：`task.delta`
- 进度：`task.progress`
- 工具卡片：`task.tool_updated`
- 计划：`task.plan_updated`
- 生命周期：`task.started`、`task.completed`、`task.failed`、`task.cancelled`
- 子代理：`task.subagent_state`、`task.subagent_result`
- 压缩：`task.compaction_started`、`task.compaction_completed`
- goal、审批、问答、重试和其他运行状态事件

relay 的 envelope 已有可复用的 `request_id`、`sent_at`、`type`、`payload` 字段，可以在此基础上增加历史查询请求/响应，不需要另建连接协议。

### 2.4 后端当前职责

后端当前在 `backend/internal/app/app.go` 中解析 relay 消息，并对各类 `task.*` 事件调用 `store.AddEvent()`。`Memory.AddEvent()` 会先更新进程内事件流；若 archive 实现了 `AppendEventWithID`，随后调用 `MySQLArchive.AppendEventWithID()` 写入 MySQL。

关键位置：

- `backend/internal/store/store.go`
- `backend/internal/store/mysql_archive.go`
- `backend/internal/app/app.go`
- `backend/internal/api/api.go`
- `backend/internal/model/model.go`

MySQL 当前不仅保存事件正文，还建立了多个 `task_events` 索引，并被以下能力读取：

- `/api/tasks/{taskID}/events` 的 SSE 历史补发和断线追赶。
- `/api/tasks/{taskID}/event-pages` 的分页历史恢复。
- 终态事件和任务恢复逻辑。
- 计划历史清理、旧计划清理和项目记忆的有界事件读取。

因此，`task_events` 不是单一调用点，直接删表会影响历史加载、断线重连、任务终态恢复和后台清理任务。

### 2.5 Flutter 当前职责

关键文件：

- `flutter_app/lib/features/chat/data/local_chat_store.dart`
- `flutter_app/lib/features/chat/data/local_chat_store_sqlite.dart`
- `flutter_app/lib/features/chat/data/chat_repository.dart`
- `flutter_app/lib/features/chat/presentation/chat_provider.dart`

Flutter SQLite 建表内容：

- `cached_tasks`：按 identity、agent、project、session 缓存任务摘要和 payload。
- `task_events`：按 identity、task、event_key 去重保存收到的事件 payload。
- `session_sync_state`：保存 session revision。

当前 `LocalChatStore` 只暴露 `recordEvent()`，没有 `readEvents()`、分页读取、游标重放或事件投影接口。`ChatNotifier._persistTaskEvent()` 在处理 SSE 时写入事件，但 `ChatRepository.watchTaskSnapshotEvents()` 仍请求后端 `event-pages` 来重建聊天界面。

这说明 Flutter `task_events` 当前并不是 UI 的权威数据源，删除它不会直接使 UI 失去历史；真正需要改的是历史读取 API 的来源和响应 DTO。

## 3. 重构后的职责边界

### 3.1 Agent：历史权威源

Agent 应负责：

- 持久化完整 session、message、part、todo 和同步事件。
- 为指定 `agent_id + machine_id + project_id + session_id` 提供有序历史查询。
- 提供稳定 cursor/sequence，支持分页和断线重放。
- 返回子会话、工具调用、计划、reasoning、审批、问答和压缩状态所需的原始数据。
- 在 Agent 重启后仍能读取历史。

Agent 不应只返回当前内存中的拼接文本。完整历史必须从本地数据库读取，避免 relay 进程重启后丢失正文或工具状态。

### 3.2 后端：broker、鉴权和任务索引

后端应负责：

- 验证 Flutter 用户对 task/session 的访问权限。
- 根据稳定映射找到正确的 Agent 连接。
- 将 Flutter 历史请求转为 Agent request，并以 `request_id` 关联响应。
- 转发 Agent 的历史响应和实时事件。
- 保存轻量任务索引和终态状态到 `tasks`，供列表、权限、通知和路由使用。
- 在 Agent 在线时优先获取 Agent 历史，在兼容期对旧任务回退 MySQL。

后端不再把每一条高频 delta/progress/tool 更新作为永久云端历史的默认写入目标。

### 3.3 Flutter：展示和短期 UI 状态

Flutter 应负责：

- 通过后端获取 Agent 历史 DTO。
- 将统一 DTO 投影成当前 `model.Event` 或直接投影为 Chat 状态。
- 接收实时事件并更新当前界面。
- 保存可选的启动缓存、筛选状态和 revision，不把本地缓存当作权威历史。

Flutter 不需要保存完整事件日志。一个 Agent 对多个设备提供服务时，删除设备上的事件副本可以避免分叉和无限增长。

## 4. 模块处理清单

### 4.1 必须重构

| 模块 | 当前问题 | 重构内容 |
| --- | --- | --- |
| Agent relay | 只有任务事件上行，没有通用历史读取请求 | 增加 `session.history.request/response` 或等价的 `task.history.request/response`，携带 request_id、session_id、cursor、limit、task_id 映射 |
| Agent history adapter | 本地 session/message/part 结构与 Flutter `model.Event` 不同 | 新增稳定 DTO 和转换层，明确 message、part、event、todo、child session 的顺序和类型 |
| 后端 broker | 当前只处理实时 relay，缺少请求-响应等待机制 | 增加按 Agent/machine/project/session 路由、超时、断线、取消和 request_id 去重 |
| 后端历史 API | `/event-pages` 直接读 MySQL | 新增 Agent history API；在线 Agent 优先，旧任务或 Agent 离线时兼容回退 MySQL |
| Flutter ChatRepository | 历史来源固定为 MySQL event-pages | 改为调用新的 Agent-history API，保留旧接口 fallback，响应转换保持现有 UI 模型兼容 |
| Flutter 历史恢复 | 依赖云端派生事件来重建完整聊天 | 优先消费 Agent 原始历史，再将实时 relay 事件合并，使用 Agent sequence 去重 |
| 映射模型 | task_id 和 session_id 不是天然一一对应 | 固化 `task_id`、`session_id`、`agent_id`、`machine_id`、`project_id`、`project_root`、`operator_id` 的映射和权限校验 |
| 测试 | 当前测试主要覆盖 MySQL event-pages 和实时 SSE | 增加 Agent 在线、重启、多设备、断线、分页、fallback、顺序和投影一致性测试 |

### 4.2 可以在兼容期后废弃或删除

以下项目是最终可删除项，但不应在第一阶段同时删除：

1. Flutter SQLite `task_events` 表。
2. `LocalChatStore.recordEvent()` 接口及其 SQLite 实现。
3. `ChatNotifier._persistTaskEvent()` 及对应的 SSE 永久落盘调用。
4. Flutter 事件表相关索引、迁移和测试。
5. 后端对高频 `delta`、`progress`、`tool_updated` 等事件的永久 MySQL 插入路径。
6. 以 MySQL `task_events` 作为完整历史权威源的 `/event-pages` 主路径。
7. 只为上述高频事件服务的 MySQL 索引和清理任务；删除前要确认没有项目记忆、终态恢复或旧任务回退依赖。

### 4.3 必须保留

下列能力和本次迁移不是一回事，不应因为删除 Flutter 事件表而误删：

- Agent 本地 `session`、`message`、`part`、`todo`、`event`、`event_sequence`。
- Agent SyncEvent、sequence 和 replay 机制。
- relay 实时事件发送能力。
- 后端 `tasks` 任务索引、权限和状态机。
- 后端 broker、WebSocket、SSE 和心跳。
- 任务创建、取消、恢复、审批、问答和终态处理。
- Flutter 当前 SSE UI 更新逻辑。
- `cached_tasks` 和 `session_sync_state` 的启动缓存能力（至少在第一阶段）。
- MySQL 旧历史读取和 fallback，直到迁移验收完成。

### 4.4 暂不删除的 Flutter 本地表

`cached_tasks` 和 `session_sync_state` 可以继续保留，但文档和代码必须明确：

- 它们是性能缓存，不是权威源。
- 清空 Flutter SQLite 不得导致历史丢失。
- 缓存失效时必须重新向后端/Agent 请求。
- 多设备之间不通过这些表同步权威事件。

如果后续确认完全不需要启动缓存，再单独发起 Flutter 本地数据库精简变更，不要和历史源迁移绑在同一批上线。

## 5. 建议的历史协议

### 5.1 请求

建议使用明确的 request/response，而不是把历史伪装成大量实时事件：

```json
{
  "type": "session.history.request",
  "request_id": "REQ_ID",
  "sent_at": "2026-09-07T00:00:00Z",
  "payload": {
    "task_id": "TASK_ID",
    "session_id": "SESSION_ID",
    "agent_id": "AGENT_ID",
    "machine_id": "MACHINE_ID",
    "project_id": "PROJECT_ID",
    "cursor": "CURSOR_OR_EMPTY",
    "limit": 200,
    "include_children": true
  }
}
```

### 5.2 响应

响应应包含原始历史和可转换的稳定排序信息：

```json
{
  "type": "session.history.response",
  "request_id": "REQ_ID",
  "payload": {
    "task_id": "TASK_ID",
    "session_id": "SESSION_ID",
    "items": [],
    "next_cursor": "NEXT_CURSOR_OR_EMPTY",
    "has_more": false,
    "source_sequence_start": 1,
    "source_sequence_end": 200,
    "complete": true,
    "error_code": ""
  }
}
```

`items` 可以是统一 history DTO，也可以暂时返回 Agent 原始 `message/part/event`，但必须固定版本。至少要覆盖：

- user/assistant/system 消息和 message id。
- text 与 reasoning 的分离。
- tool call、tool result、工具状态和 tool call id。
- plan/todo 的版本、顺序和最终状态。
- subagent parent/child session、node id、状态和结果。
- compaction 开始/结束和压缩后的消息关系。
- approval、question、retry、error、cancelled、completed。
- created_at、updated_at、sequence、cursor 和幂等键。

### 5.3 合并规则

- 历史快照和实时事件必须使用 Agent sequence 或稳定 `(sequence, id)` 排序，不应使用 Flutter 本地时间作为主序。
- Flutter 收到重复事件时按 Agent event id/sequence 去重。
- 后端不应重新生成会覆盖 Agent sequence 的随机顺序。
- 历史响应和实时订阅的建立顺序应为“先订阅实时，再读取历史”，避免查询期间丢事件。
- 同一个 request_id 只允许完成一次；超时后迟到响应应被丢弃或记录为过期响应。

## 6. 关键风险和降级行为

### 6.1 Agent 离线

Agent 离线时后端难以凭空恢复新历史。建议分层降级：

1. 若旧任务仍有 MySQL 历史，读取兼容快照并标识来源为 legacy archive。
2. 若只有 `tasks` 索引，展示任务状态、最终结果或“历史暂不可用”状态。
3. Agent 恢复上线后，重新请求完整历史并校正 Flutter 当前 UI。

### 6.2 Agent 本地历史被删除或归档

需要返回明确错误码，例如 `history_not_found`、`history_archived`、`history_cursor_expired`，Flutter 显示可区分的状态。不应把空数组误当成“用户从未发送过消息”。

### 6.3 旧任务没有 Agent 对应关系

旧任务可能只存在于 MySQL `task_events`，必须保留 fallback。迁移前应统计：有多少 task 有有效 `session_id`、有多少 Agent 仍保留对应 session、多少任务只能从 MySQL 恢复。

### 6.4 多设备并发读取

多个 Flutter 设备可以同时读取，但写入仍由 Agent 控制。设备端不应修改 sequence，也不应以本地事件副本反向覆盖 Agent 历史。所有设备应看到相同的 Agent cursor 和消息顺序。

### 6.5 高并发 delta

高频 delta 是 `task_events` 膨胀和 MySQL buffer/index 压力的主要候选来源之一，但在实际改动前仍需通过数据库指标确认。应区分：

- MySQL buffer pool、索引页和连接内存。
- 后端 Memory 事件缓存和订阅者数量。
- Flutter SQLite 文件增长。
- Agent 本地 SQLite 文件增长。

不应只删除一张表来推断所有内存问题已经解决。

## 7. 推荐迁移顺序

### 阶段 0：基线和盘点

- 记录 MySQL `task_events` 行数、表大小、索引大小、每天新增行数和事件类型分布。
- 记录后端进程 RSS、Memory 任务事件数量、SSE 连接数和平均历史请求大小。
- 记录 Flutter 本地 SQLite 文件大小以及 `task_events` 是否实际被读取。
- 统计任务和 Agent/session 的映射完整度。

### 阶段 1：Agent 历史接口

- 定义 history DTO、版本和 cursor。
- Agent 增加 session history request/response。
- 从本地 `message/part/event` 读取，统一子会话、计划和工具数据。
- 加入分页上限、超时、请求幂等和权限前置校验。

### 阶段 2：后端代理和双路径读取

- 增加后端历史查询 endpoint。
- 在线 Agent 优先，MySQL `event-pages` fallback。
- 保留旧 SSE 行为，先确保实时显示不变。
- 记录每次读取的 source：`agent_local`、`mysql_legacy`、`task_index_only`。

### 阶段 3：Flutter 切换读取源

- `ChatRepository` 优先请求 Agent history。
- 旧任务和 Agent 离线继续使用旧分页接口。
- Flutter UI 使用现有模型投影，避免同时改动展示组件。
- `task_events` 暂时继续写入但不再参与权威恢复，便于观测和回滚。

### 阶段 4：验证多设备和异常场景

- 两台 Flutter 设备同时读取同一 Agent/session。
- Agent 重启后读取完整历史。
- 清空 Flutter SQLite 后恢复历史。
- SSE 断开、重连、重复事件和分页游标过期。
- goal 多轮、reasoning/text、工具、计划、子代理、压缩、审批和问答。
- Agent 离线、旧任务 fallback 和历史被删除。

### 阶段 5：停止高频云端永久写入

- 先停止新 `delta`、`progress`、高频工具中间态的 MySQL 永久插入，保留低频任务索引和终态。
- 观察至少一个完整发布周期。
- 保留可配置开关，出现历史读取问题时恢复写入。

### 阶段 6：清理旧数据和 Flutter 事件表

- 按时间、任务状态和事件类型分批清理旧 `task_events`，每批可回滚或有备份。
- 确认项目记忆、计划清理、终态恢复和旧任务 fallback 不再依赖后再删索引。
- 最后删除 Flutter `task_events` 表、`recordEvent()`、`_persistTaskEvent()` 和相关测试。

## 8. 验收清单

以下全部通过后，才可以把 Agent 宣布为唯一历史权威源：

- [ ] Agent 在线时，Flutter 历史 source 为 `agent_local`。
- [ ] Agent 重启后历史仍完整可读。
- [ ] 同一 Agent 连接两个 Flutter 设备，两端消息、工具、计划和子代理顺序一致。
- [ ] 删除 Flutter 本地 SQLite 后，历史仍能重新加载。
- [ ] 历史分页不会重复、跳过或乱序。
- [ ] 历史读取期间到达的实时事件不会丢失。
- [ ] 断线重连不会重复正文，也不会丢失终态事件。
- [ ] text/reasoning 分离保持现有 UI 行为。
- [ ] 工具开始、更新、完成和错误状态正确。
- [ ] plan history 去重、顺序和最终状态正确。
- [ ] subagent tree 的 parent、child、node 状态正确。
- [ ] compaction、goal、approval、question、retry 和 cancel 可恢复。
- [ ] Agent 离线时有明确降级状态，不把空历史当成正常空会话。
- [ ] 旧任务仍能从 MySQL fallback 展示。
- [ ] MySQL `task_events` 新增速率显著下降，表和索引增长受控。
- [ ] 后端 RSS、MySQL buffer/index 内存和 Flutter 文件增长均有对比数据。

## 9. 最终删除清单（验收后执行）

### Flutter

- `task_events` SQLite 表。
- `LocalChatStore.recordEvent()`。
- `_SqliteLocalChatStore.recordEvent()`。
- `ChatNotifier._persistTaskEvent()`。
- SSE 事件永久写入调用。
- 事件表相关迁移、索引和单元测试。

### 后端

- 高频 relay 事件到 `MySQLArchive.AppendEventWithID()` 的默认永久写入。
- `/event-pages` 作为新任务主路径的依赖。
- 仅服务旧高频事件归档的索引和清理代码。

### 不应删除

- Agent 本地会话和 sync 数据库。
- Agent relay 实时发送。
- 后端任务索引、权限、broker、SSE 和终态状态机。
- MySQL 兼容读取，直到所有旧任务和离线场景迁移完成。
- Flutter `cached_tasks`、`session_sync_state`，除非另行完成缓存精简设计。

## 10. 审查结论

当前设计的主要问题不是 Flutter 少了一个本地日志读取接口，而是同一份对话同时被三层以不同格式保存：Agent 本地原始会话、后端 Memory/MySQL 派生事件、Flutter 本地只写事件缓存。它们的生命周期、顺序和清理策略不同，导致 `task_events` 持续膨胀，也让多设备一致性依赖云端派生事件。

建议采用“Agent 权威历史 + 后端 relay/broker + Flutter 展示缓存”的分层方式。先新增 Agent history 通道并保留 MySQL fallback，再切换 Flutter 读取，最后停止高频归档和清理旧表。这样可以达到“原始对话只保存在 Agent 电脑、多个 Flutter 设备共享同一历史”的目标，同时保留足够的回滚和旧任务兼容能力。
