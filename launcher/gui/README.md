# 码控 GUI

这是 launcher 的 Wails GUI 版本。它复用现有 Go launcher supervisor 能力，用户可以在桌面窗口中登录账号、写入 operator key、启动本地 launcher、查看 agent 状态并执行基础管理操作。

## 当前能力
- 账号密码登录 `https://www.xyapi.top/codex/api/auth/login`
- 自动保存后端返回的 `operator_key`
- 启动本地 launcher supervisor
- 查看设备状态、launcher 版本、自启动状态和 agent 列表
- 创建 agent、启动/停止、重启、删除 agent
- 检查或触发 launcher 自更新

## 本机构建

```bash
cd launcher
./script/build-gui-mac.sh
```

构建产物：

```text
launcher/gui/build/bin/码控.app
```

## 发布注意
- Wails GUI 依赖目标系统 WebView，不再走 `CGO_ENABLED=0` 的纯 Go 三端交叉编译路线。
- macOS、Windows、Linux GUI 包建议分别在对应系统真实构建和测试。
- 当前 headless launcher 仍由 `script/build-three.sh` 构建，线上自更新暂时不受 GUI 技术验证影响。
