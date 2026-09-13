# Android 后台通知可靠性改造记录

## 目标

后台任务完成通知由 Android 原生前台 Service 维护唯一 SSE 连接。Flutter 不再在前后台生命周期中维护第二条通知 SSE，只通过原生 EventChannel 接收实时变更，并通过 REST 做状态补偿。

## 已确认的现状

- `GlobalOverlayService` 已在原生线程连接 `/api/overlay/events`，并负责 Token 刷新、通知投递和通知游标。
- Flutter `AppNotificationSyncService` 也会连接同一 SSE，形成重复连接和两套游标。
- 原生 Service 的 SSE 重连循环没有绑定后台通知开关，切换设置时可能继续重连。
- 首次连接没有有效游标时，服务端返回最新状态游标，不回放既有通知；身份作用域切换时可能跳过未读任务。
- 通知投递失败队列主要在内存中，Service 被系统回收后恢复能力有限。
- 服务端已有 20 秒 heartbeat、通知变更表和游标增量接口，可作为补偿基础。

## 设计决策

1. 不使用透明悬浮窗或其他窗口锚点。
2. Android 前台 Service 是后台通知的唯一 SSE 所有者。
3. Flutter 只接收原生推送事件，前台恢复时用 REST 增量同步。
4. SSE 采用单实例连接状态机：启动、连接、重连、认证刷新、停止均可观测且幂等。
5. 通知事件先落本地持久化队列，系统通知成功后推进游标。
6. Service 重启、网络恢复、Token 更新和通知权限恢复都触发补偿连接。
7. 服务端保留事件持久化和 SSE heartbeat；部署层为 SSE 路由显式关闭缓冲并设置长读取超时。

## 分阶段实施

### 阶段 1：基线与记录

- 建立本记录文件。
- 固定单 SSE 所有权和 Flutter EventChannel 方向。

### 阶段 2：原生连接状态机

- 抽离或封装通知 SSE 客户端。
- 增加单实例保护、开关感知、网络和认证重连。
- 修复游标初始化和 Service 重启恢复。

### 阶段 3：原生事件桥接

- 新增独立 `notification_events` EventChannel。
- 原生事件发送给 Flutter 控制器。
- 关闭 Flutter 通知 SSE 和后台保活逻辑。

### 阶段 4：可靠投递与服务端配置

- 持久化待投递通知队列。
- 校准通知权限、频道、Nginx/代理 SSE 参数。
- 增加诊断状态和日志字段。

### 阶段 5：验证

- Flutter 单元测试、Android 编译检查、后端测试。
- 覆盖冷启动、切后台、进程重启、断网、Token 过期、通知权限关闭、身份切换和连续事件压力场景。

## 进度

- [x] 阶段 1：记录基线和方案
- [x] 阶段 2：原生连接状态机
- [x] 阶段 3：原生事件桥接
- [x] 阶段 4：可靠投递与服务端配置
- [x] 阶段 5：验证（目标范围通过）

## 阶段记录

### 阶段 1：基线与记录

- 新增本文件，固定单一原生 SSE 所有权和移除透明悬浮窗的决策。

### 阶段 2：原生连接状态机

- 为原生连接循环增加单实例保护 `streamLoopRunning`。
- 关闭后台通知时主动断开连接，并让重连循环感知开关。
- 增加 `last_stream_error`、`stream_loop_running` 等诊断状态。
- 首次连接始终发送游标 `0`，避免服务端把未初始化客户端直接定位到最新版本。
- 注册 Android 默认网络回调，网络恢复时立即触发幂等重连。

### 阶段 3：原生事件桥接

- 新增 `chat_codex/notification_events` EventChannel。
- 原生收到 `notification.updated` 后转发给 Flutter 控制器。
- Flutter 通知同步服务删除 SSE、重连和 fallback polling，仅保留 REST 增量同步。

### 阶段 4：可靠投递与服务端配置

- 待投递通知写入 `FlutterSharedPreferences`，Service 重启后自动恢复。
- 后台通知开关启用和关闭都会发送原生命令，避免关闭状态只停留在 Flutter。
- 关闭状态下不投递内存队列；无可交互悬浮层时同步结束通知 Service，避免关闭后继续常驻。
- Nginx 部署脚本为 `/codex/api/overlay/events` 增加 HTTP/1.1、关闭缓冲和 1 小时读写超时。
- Android Kotlin 编译已通过；后端 API、overlay、store 测试已通过。
- Flutter 通知测试曾因旧 `pollInterval` 构造参数不兼容失败，已恢复该参数的源码兼容性并重新通过目标测试。

### 阶段 5：验证结果

- `flutter test test/core/notifications/app_notification_sync_test.dart test/core/notifications/app_notification_controller_test.dart test/core/services/global_overlay_service_test.dart`：25 个测试通过。
- `go test ./...`：后端全量测试通过。
- `./gradlew :app:compileDebugKotlin`：通过。
- `flutter analyze --no-pub`：无本次改动引入的错误；仅保留项目原有的 Web `dart:html` 弃用提示。
- `bash -n deploy.sh`、`git diff --check`：通过。
- Flutter 全量测试仍有既有聊天队列测试失败，日志显示 `path_provider` 的 `MissingPluginException` 和测试超时；该失败不涉及后台通知模块，需单独完善测试插件桩。
