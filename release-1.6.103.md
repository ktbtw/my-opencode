# Release 1.6.103

## Versions

- Flutter app: `1.6.103+250`
- Backend: 本次无独立用户版本号（按 Git commit / 发布日标识）
- Launcher / OpenCode CLI: 本次不发布

## Changes

### 客户端（Flutter Web / Android / Windows / macOS）

- 修复设备页启用/停用 Agent 后不再弹出操作通知的问题：隐藏中的操作完成后未释放抑制标记，导致该 operationId 被永久拉黑（且随重启持久化）。现在终态更新会释放标记，后续同 ID 操作可正常提示。
- 修复停止任务后停止按钮长时间残留、模型测速不可用的问题（配合服务端取消链路修复）。

### 后端

- 取消兜底不再以 `UpdatedAt` 起算：改用「取消请求时刻」（`cancel_requested_at`）。此前子代理事件会持续刷新 `UpdatedAt`，使取消 grace 永不触发，任务永久停留在 `cancelling`。
- 取消期间放行设备上报的 `task.completed`：此前会被 `shouldIgnoreTaskEvent` 丢弃，导致真实完成结果丢失并被误判为失败。
- 取消接口幂等：已处于 `cancelling` 的任务重复取消不再重复追加事件、不再重复下发 `task.cancel`。

## Verification

- 后端 `go test ./internal/...` 全量通过；`go vet ./...` 无问题；`go test -race ./internal/store ./internal/api ./internal/app` 通过。
- 新增回归：`internal/app/cancel_race_test.go`（取消幂等 / 完成结果保留 / grace 不受事件刷新影响）。
- Flutter `flutter analyze` 无新增问题；`flutter test` 515 项全部通过。
- 新增回归：`test/core/notifications/app_notification_controller_test.dart`（终态释放隐藏抑制，同 ID 可再次提示）；已用回退修复的方式确认该用例能复现原问题。
- 生产验证：`/healthz`、`/readyz`、`/api/app/version` 与各端下载校验（APK / EXE / DMG）。

## Rollback

- 后端：`deploy.sh` 启动失败自动回滚，旧二进制保留在服务器备份目录。
- 客户端：上一版 APK / EXE / DMG 与 `version.json` 保留在发布目录。
