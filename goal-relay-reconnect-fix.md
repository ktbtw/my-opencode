# Goal Relay Reconnect Fix

## Goal
修复 Goal 长任务在 Relay 重连后丢失进度和终态事件、继而被后端误判超时的问题。

## Tasks
- [x] 将任务事件发送切换到 `state.ws`，断线时缓存，重连后按序补发。
- [x] 重连后重新声明活动任务和 Goal 状态。
- [x] 让有效 Goal 心跳参与独立的长任务活跃租约。
- [x] 为重连后的进度、终态和 Goal 心跳补回归测试。
- [x] 运行 OpenCode Relay 与 Go 后端定向测试。
- [x] 构建、发布并部署修复版本。

## Done When
- [x] 运行中的 Goal 经 WebSocket 断开重连后仍能上报进度和最终状态。
- [x] 持续收到 Goal 心跳的任务不会被通用 30 分钟清理器终止。

## Release
- Backend: deployed and active on `www.xyapi.top`.
- OpenCode: `1.15.55`, published for Windows x64, macOS arm64, and Linux x64.
- Windows verification: Launcher downloaded `1.15.55`; the active Agent process runs from the `1.15.55` directory.
