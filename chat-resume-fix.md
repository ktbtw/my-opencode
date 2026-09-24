# 对话恢复与任务停滞排查

截至 2026-09-19，本次改动只在工作区，尚未打包发布。

- [x] 核对 Flutter task SSE、队列 SSE 与 OpenCode relay WebSocket 链路。
- [x] 将恢复展示状态与执行提示分离，恢复的活动消息不再重放首次发送四阶段卡片。
- [x] 统一停止按钮的活动任务判定；单纯加载历史不显示停止按钮。
- [x] 复现并修复仅有心跳时 SSE 取消等待的问题，覆盖原生 socket、API 鉴权层、聊天仓库。
- [x] 任务恢复不再等待队列恢复；恢复异步结果增加会话版本检查。
- [x] 查询 admin 的两个 Agent 的真实任务状态及本机日志。
- [x] 最终回归：Flutter 406 个测试全部通过；静态检查无 error/warning，保留原有 5 条 info；git diff --check 通过。

## 已复现的客户端问题

旧原生 SSE、API 和仓库通过 async* / await for 层层包装。业务流只收到心跳时，取消订阅等待后续业务帧，旧 socket 保持打开。`reconcileAfterResume` 又先 await 队列重建，而队列重建 await 旧订阅取消，导致任务状态同步被阻塞。

用本地 HttpServer 只发送 `: ping`，取消在 500ms 内未返回。修复后原生/API/task/queue 的取消回归均能及时结束。改为显式 StreamController 转发取消，原生连接 60 秒未收到任何字节则断开；心跳算有效字节，不因模型暂时没有输出就断线。保留 401 刷新和任务游标重连。

恢复 UI 使用 `StreamRecovery` 字段，不再复用会被快照覆盖的 `statusHint`。`syncing` 展示“正在恢复任务状态”，同步后进入 `following`，持续显示当前执行状态且隐藏首次发送四阶段标签。停止按钮由活动任务/streaming 回复决定，取消仍发送到同一 task_id。

## 真实任务证据（北京时间）

| 项目 | task_id | API 当前状态 | 证据 |
| --- | --- | --- | --- |
| 造梦西游4 | task_1789736205866003038 | failed | 9/18 22:31:56 发起超时取消，22:32:11 转失败；30 分钟无响应，取消后 15 秒未完成 |
| 造梦西游6 | task_1789737484971466321 | failed | 9/18 22:31:46 发起超时取消，22:32:01 转失败；同类超时 |
| 造梦西游6 | task_1789748096279825428 | cancelled | 9/19 00:42:30 发起停止，00:42:34 任务状态为 cancelled |

来源：生产 `GET /api/tasks/{id}`、完整分页 `/event-pages`，以及本机 `~/.my-opencode-launcher/agents/{agent_id}/stdout.log`。两个 agent_id 分别为 `agent_7ab10665`、`agent_d9e0ed2d`。

最后一条造梦西游6日志：`prompt_started` 22,395ms，首次 reasoning 39,992ms，首次正文 941,839ms（15 分 42 秒），`prompt_completed` 1,652,075ms 且 has_error=true。造梦西游4前一条 `task_1789732395963625672` 记录 `task_failed_sent: Upstream service temporarily unavailable`。说明运行端/上游确有长等待；这些日志还不足以把所有长等待统一归为 launcher 或同一个模型故障。两条超时任务在 backend 已有失败终态，App 应同步展示失败，不再等待新正文。

本次未重启用户的 Agent，未重跑已失败的模型任务，也未修改供应商配置。上游服务稳定性与设备端长任务停滞仍需按具体任务继续定位，客户端修复不会让已失败任务自动完成。

## 连接模型与设计判断

- OpenCode relay：每个运行中的 Agent 进程一条到 backend 的 WebSocket；本地任务事件通过 GlobalBus 转发，不是每个进程到 Flutter 一条 SSE。
- launcher：另有设备管理 WebSocket，负责进程和设备控制。
- Flutter：每次 `watchTaskEvents` 订阅建立 task SSE；会话还有一条 queue SSE。子代理树也会单独订阅任务，所以连接数不保证“每个 task 恰好一条”。
- chatProvider 当前不使用 autoDispose，切换页面会保留旧 Agent 的状态及订阅。因此连接数可能随访问过的活动对话增加。后续可按可见页面或引用计数管理订阅，并复用同 task 的业务流；应同时处理共享游标与历史补齐，避免只合并连接而丢失事件。
- 目前按任务 SSE 的总体设计可用，已确认的缺陷是取消与恢复耦合、恢复状态与执行提示混用。无需为本次问题重构整个 WebSocket/SSE 拓扑。

## 验证范围

新增真实 HTTP 测试覆盖心跳期间取消、无字节超时、心跳保活、HTTP 错误、API/task/queue 全链路取消、401 刷新、Last-Event-ID 重连；状态和 widget 测试覆盖冷启动、队列阻塞时终态同步、恢复卡片与停止按钮。发布后仍需在手机实测锁屏/后台挂起与冷启动，确认系统网络生命周期下的表现。

最终验证还包含设备分页布局测试的依赖隔离：布局测试改用空的设备指标流，避免真实 SSE 失败重试干扰布局断言。backend API/store 测试已通过，本次未修改 backend 或 launcher/OpenCode 源码。


后续更新（2026-09-19）：已实现订阅共享、详情连接按页面/前后台释放及高频输出批处理，当前架构和 421 项回归测试结果见 `subscription-performance.md`。本文的原始连接分析保留作问题证据，当前实现以该后续说明为准。
