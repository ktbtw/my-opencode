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

- 已发布：Android APK、Flutter Web（`/` 与 `/codex/`）。
- Windows EXE 未重新构建：内网构建机 `windows-lan-yuzhihao`（192.168.10.114）当前不可达，
  线上 windows-x64 条目按 `--android-only` 的预期行为保留为上一版（1.6.118 / v=265）。
- macOS DMG 未重新构建，同样保留上一版条目。

## Rollback

- 客户端：发布目录保留上一版 APK（`app-release.apk.bak-<版本>`）与 `version.json`。
- Web：发布前保留 `/opt/chat-codex/web` 与 `/opt/chat-codex/web-codex`。
