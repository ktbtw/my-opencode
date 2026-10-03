# Release 1.6.119

## Versions

- Flutter app: `1.6.119+266`
- Backend: 本次无独立用户版本号（按 Git commit / 发布日标识）
- OpenCode Agent / Launcher / CLI: 本次不发布

## Changes

### 客户端（1.6.118 → 1.6.119）

- 交付图片支持左右滑动切换：同一轮交付的多张图片放进同一个横向分页，点开后可直接滑动查看
  上一张 / 下一张，底部显示当前是第几张，不再需要退出后重新点击。
- 应用内生成的图片同样支持左右滑动查看，打开时从点击的那张开始。
- 问答弹窗改为横向分页：多个问题左右滑动切换，顶部定位点可点击直接跳题，已答问题打勾标记；
  每页内部可上下滚动，选项较多时不再把弹窗撑长。
- 修复问答弹窗单选选中态显示异常的问题：选中圆点由 6px 图标字形改为直接绘制，渲染更清晰。
- 修复切换 Agent 时已手动收起的状态通知被再次自动展开的问题，收起后保持收起。

## Verification

- Flutter：`flutter analyze --no-pub` 无新增问题（8 项均为改动前既存）；
  `flutter test --no-pub` 588 项全部通过。
- 新增回归：图片画廊分页与左右滑动 6 项、问答弹窗横向分页与提交门禁 4 项、通知收起 2 项。
- 构建校验：`deploy.sh` 用 `aapt dump badging` 确认 APK 为 `1.6.119+266`。
- 线上验证：
  - `/healthz`、`/readyz` 均返回 200。
  - `/api/app/version` 返回 `1.6.119+266`，changelog 6 条，与 release-notes 一致。
  - APK 下载返回 200，远端 `sha256=255a93aa074b571cfaa2fb39a4456fda4c05b34bfe9dec2d496ac4a3850a36f7`
    与本地构建产物一致。
  - Web 根路径与 `/codex/` 均返回 200，nginx 配置校验通过。

## 发布范围说明

- 已发布：Android APK、Windows EXE、macOS DMG、Flutter Web（`/` 与 `/codex/`）。
- Windows EXE 在 Windows 构建机（`windows-lan-yuzhihao`，内网 IP 已变更为 192.168.10.110）上构建，
  构建目录用完即删。
- macOS DMG 在 macOS 本机构建（`flutter build macos --release`，产物含 arm64），
  经 `hdiutil` 打包为 UDZO，挂载校验 `CFBundleShortVersionString = 1.6.119`。
- 最终以 `--mobile-app-only` 一次性上传三端，脚本校验版本与 SHA256 后发布。

三端 SHA256：

| 平台 | 文件 | SHA256 |
| --- | --- | --- |
| android | `chat-codex-1.6.119.apk` | `255a93aa074b571cfaa2fb39a4456fda4c05b34bfe9dec2d496ac4a3850a36f7` |
| windows-x64 | `chat-codex-windows-x64-setup.exe` | `4610e7df7e713fa7356d1145b48fdc8b27693ddc1f933fdad2c7900f745517f3` |
| darwin-arm64 | `chat-codex-darwin-arm64.dmg` | `037b1ced853ae7d0b38a2e69d117a78a290de292ac66dba05cc0c29a0a240e03` |

## Rollback

- 客户端：发布目录保留上一版 APK（`app-release.apk.bak-<版本>`）与 `version.json`。
- Web：发布前保留 `/opt/chat-codex/web` 与 `/opt/chat-codex/web-codex`。
