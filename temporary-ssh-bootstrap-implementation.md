# 临时 SSH Bootstrap 实施清单

- [x] 扩展 `tunnelSession/tunnelManager`，增加 bootstrap token 哈希、查找和统一清理。
- [x] 增加 Launcher 发布元数据读取与严格 tunnel asset 校验。
- [x] 增加 Unix/Windows bootstrap 模板、用户名验证和短命令格式化。
- [x] bootstrap 改为建立后台 SSH master，并生成可复用的 Agent wrapper（`exec/status/close`）。
- [x] 增加 `/b/{token}/{platform}` 路由、无缓存响应头和错误映射。
- [x] 扩展创建 tunnel 请求/响应；新版通过 `bootstrap_version: 1` 隔离旧字段，旧版兼容分支待自更新迁移完成后删除。
- [x] 更新 Launcher GUI 只使用服务端短命令，并清理本地拼接超长命令的旧实现。
- [x] 更新 Nginx 部署配置，关闭 `/codex/b/` 日志、缓存和缓冲。
- [x] 补齐 manager、HTTP、模板、GUI 和并发回归测试。
- [x] 运行 Go、GUI、tunnel 与发布构建验证。
- [x] 递增版本，只发布后端与 Launcher，并完成线上端到端验证。
