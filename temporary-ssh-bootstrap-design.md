# 临时 SSH Bootstrap 设计

## 目标

将 Launcher 当前生成的超长 SSH 命令替换为 macOS/Linux 与 Windows 两条短命令。完整 bootstrap 脚本由后端按请求动态渲染，并与现有 tunnel session 使用同一生命周期。

## 理解与约束

- macOS/Linux 与 Windows 继续提供两条独立命令。
- 临时状态只保存在后端内存，不写数据库或磁盘。
- bootstrap 有效期与 tunnel ticket 一致，默认 5 分钟。
- 有效期内允许重复获取脚本；首次 client WebSocket claim 后立即失效。
- session 取消、Launcher 离线、超时或服务重启时同步失效。
- tunnel ticket 继续保持单次使用，每台设备最多 4 个活动 tunnel。
- 当前按单实例部署；未来多实例时 tunnel 与 bootstrap 状态整体迁移到 Redis。

## 选定方案

`tunnelManager` 是 bootstrap 状态的唯一所有者。每个 `tunnelSession` 增加 bootstrap token 哈希；服务端只在 bootstrap 存活期间于内存保留渲染 ticket 所需的 client token，不保存原始 bootstrap token、完整短链接或脚本文本。

创建 tunnel 时同时生成 client token、device token 与 bootstrap token。新版 Launcher 请求携带 `bootstrap_version: 1`，创建响应只返回 Unix/Windows bootstrap 路径，不再暴露原始 client token。未携带协议版本的旧 Launcher 暂时获得 `token/client_path` 兼容字段；该分支仅保留到 Windows 自更新迁移完成。新版 Launcher 只根据路径生成并展示两条短命令。

公共脚本端点：

- `GET /b/{bootstrapToken}/unix`
- `GET /b/{bootstrapToken}/windows`

端点读取当前 Launcher 发布元数据，严格校验 tunnel helper 的 URL、SHA-256 和可用状态，再按平台渲染脚本。获取脚本不消费 token；建立 client tunnel 时消费。

## 数据流

1. Launcher 创建 tunnel，并提交当前 SSH 用户名。
2. 后端验证设备、SSH 状态与用户名，创建 tunnel session 和 bootstrap token。
3. 后端向目标 Launcher 派发 device tunnel token。
4. 后端返回旧版字段和新版 Unix/Windows 短命令。
5. 诊断端执行短命令，从 `/b/` 路由获取完整脚本。
6. 脚本下载并校验对应平台的 tunnel helper，然后启动一个后台 SSH master；脚本同时生成本地 `chat-codex-ssh-<tunnel_id>` wrapper。
7. Agent 后续使用 wrapper 的 `exec`、`status`、`close` 子命令复用同一个 master，不再重新执行 bootstrap URL。
8. client WebSocket claim 成功后 bootstrap token 被清理；第二个 client 仍受单次 ticket 限制。

## 安全与可靠性

- bootstrap token 使用 256-bit 随机值，内存中仅保存 SHA-256。
- 用户名限制长度并拒绝控制字符，脚本使用平台专用转义。
- 响应设置 `Cache-Control: no-store, private`、`Pragma: no-cache` 和 `Referrer-Policy: no-referrer`。
- `/codex/b/` 的 Nginx location 关闭 access log、代理缓存和响应缓冲。
- 后端、Launcher 与 tunnel 日志不记录 bootstrap token、ticket、完整命令或脚本文本。
- 发布元数据或产物异常返回 `503`，session 保留到过期，允许重试。
- bootstrap 不存在、已使用、平台错误或过期统一返回 `404`。
- Unix 短命令使用 `bash -o pipefail`；Windows 设置 `$ErrorActionPreference='Stop'`。
- helper 下载使用临时文件、SHA-256 校验和退出清理。
- master 使用 SSH keepalive，Agent 命令不分配 PTY，避免 `curl | bash` 的 EOF 让远端交互会话提前退出。
- wrapper 与 ControlPath 写入用户私有目录；`close` 会关闭 master 并删除本地 wrapper，断线时 Agent 通过 `status` 获取失败状态并重新申请 bootstrap。

## 测试

- tunnel manager：创建、哈希存储、并发读取、claim/取消/过期清理。
- HTTP：平台限制、响应头、404/503、重复获取、无敏感值泄露。
- 模板：中文、空格、引号、控制字符和六个平台产物。
- 兼容：旧请求临时保留原字段；`bootstrap_version: 1` 的新请求只返回短命令路径，不返回原始 client token。
- Launcher GUI：只生成两条短命令，服务端缺少 bootstrap 路径时明确报错。
- 集成：Unix 脚本真实执行；Windows PowerShell 内容和 SSH 参数解析。
- 发布：版本 API、Range、SHA-256、重复领取、真实连接和连接后失效。

## 决策记录

- 选择 tunnelManager 统一所有权，未选择独立 bootstrap manager，避免双重清理状态。
- 未选择客户端上传脚本，避免形成任意脚本托管入口。
- 选择内存状态，保持与现有 tunnel 实现一致。
- 选择 client WebSocket claim 时消费，避免脚本下载中断导致短链接提前失效。
- 旧响应字段采用显式协议分流，Windows 自更新迁移完成后删除 `bootstrap_version < 1` 分支。
- 后端保留旧响应字段以允许已安装的 Launcher 滚动升级；新版 Launcher 不再包含本地拼接超长命令的旧实现。
