# API 调用测试记录

## 2026-09-06：task_events 索引迁移与线上后端更新

### 代码修复

- `ensureSchema` 新增幂等索引迁移：旧表缺失时补充
  `idx_task_events_task_sent_id (task_id, sent_at, id)` 和
  `idx_task_events_task_id_id (task_id, id)`；已有同名或等价索引时跳过。
- 新建表模板移除冗余的 `idx_task_events_task_id (task_id)`，已有环境不自动删除索引。
- 增加 MySQL 集成迁移回归，覆盖旧表补索引和重复执行不重复变更。
- 未加入自动删除、归档或重建大表逻辑，避免启动阶段产生长锁和历史数据风险。

### 发布与验证

```bash
./deploy.sh --backend-only
curl --fail --silent --show-error --max-time 10 https://www.xyapi.top/codex/healthz
```

结果：后端新进程 `NRestarts=0`，`/healthz` 返回 `{"status":"ok"}`，Nginx 配置检查通过。
线上 `task_events` 查询计划使用 `idx_task_events_task_sent_id`，未出现 `Using filesort`。
部署后 5 分钟日志无 `Out of sort memory`、panic 或 MySQL 错误；额外 60 秒观测健康检查持续正常。

### 数据库保护确认

- `verify-mysql`：保持 `Up 7 weeks (healthy)`，未停止、未重启、未修改配置。
- `chat-codex-mysql`：保持运行，Buffer Pool `3 GiB`、`max_allowed_packet` `128 MiB`。
- 当前仅保留线上已有数据和索引变更；归档保留周期需后续单独评估后再执行。

## 2026-09-05：Flutter 频繁 502 线上诊断

### 公网接口复核

```bash
curl --http1.1 -sS -o /dev/null -w 'health=%{http_code} time=%{time_total}\n' \
  https://www.xyapi.top/codex/healthz
curl --http1.1 -sS -o /dev/null -w 'version=%{http_code} time=%{time_total}\n' \
  https://www.xyapi.top/codex/api/app/version
```

结果：检查时短暂返回 HTTP `200`；服务重启窗口内同一接口返回 HTTP `502`。

### 服务器诊断

```bash
ssh -i /Users/yuminghao/Downloads/chat-codex/root.pem root@220.167.100.153 \
  'systemctl show chat-codex-backend --property=MainPID,NRestarts,Result; \
   journalctl -k --since "2026-09-04 18:00:00" | \
   grep "Out of memory: Killed process.*chat-codex-serv"'
```

结果：`chat-codex-backend` 被 Linux OOM Killer 反复杀死，`NRestarts` 达到 `179`；最近一次后端进程 RSS 约 `5.9 GiB`，服务器无 Swap。Nginx 日志同时记录 `connect() failed (111: Connection refused)`，上游为 `127.0.0.1:19081`，因此 Flutter 收到 502。

### 数据库资源诊断

```bash
docker stats --no-stream
docker exec chat-codex-mysql mysql -uroot -p\"$MYSQL_ROOT_PASSWORD\" -NBe \
  'SHOW GLOBAL VARIABLES'
```

结果：`chat-codex-mysql` 约占 `5.3 GiB`，InnoDB buffer pool 为 `4 GiB`；`task_events` 约 `3721 万`行、约 `13.6 GiB`，当前没有 `(task_id, sent_at, id)` 复合索引。后端日志持续出现 MySQL `Error 1038: Out of sort memory`。

结论：502 的直接原因是后端进程内存耗尽并退出；事件归档表过大、查询排序和后端无界加载/缓存是主要诱因。未执行线上重启、删数据、改表或修改数据库参数。

## 2026-09-05：线上后端状态检查

### 健康检查

```bash
curl --http1.1 --fail -sS --max-time 15 https://www.xyapi.top/codex/healthz
```

结果：HTTP `200`，返回 `{"status":"ok"}`。

### 版本接口

```bash
curl --http1.1 --fail -sS --max-time 15 https://www.xyapi.top/codex/api/app/version
```

结果：线上版本 `1.6.53`、构建号 `200`；Android、Windows、macOS 下载项均为 `available=true`。

### SSH 与固定 IP 连通性

```bash
ssh -o BatchMode=yes -o ConnectTimeout=10 -i /Users/yuminghao/Downloads/chat-codex/root.pem root@www.xyapi.top "systemctl is-active chat-codex-backend"
ssh -o BatchMode=yes -o ConnectTimeout=10 -i /Users/yuminghao/Downloads/chat-codex/root.pem root@114.66.33.149 "systemctl is-active chat-codex-backend"
```

结果：域名 SSH 在 banner 阶段超时；配置中的固定 IP SSH 连接超时，因此本次无法读取 systemd、端口监听、Nginx 和 MySQL 的服务器内部状态。域名当前解析为 `220.167.100.153`，与 `deploy.md` 中的 `114.66.33.149` 不一致。

## 2026-09-03：Git自动备份功能测试

### 单元测试

```bash
cd backend
go test -v ./internal/gitbackup/...
```

**测试用例：**
- ✅ TestAutoBackup_InitNewRepo - 初始化新仓库
- ✅ TestAutoBackup_ExistingRepo - 现有仓库追加提交
- ✅ TestAutoBackup_NoChanges - 无变更跳过
- ✅ TestAutoBackup_InvalidDir - 无效目录错误处理
- ✅ TestAutoBackup_EmptyDir - 空目录参数验证

结果：`PASS ok relay-server/internal/gitbackup 0.789s`

### 编译验证

```bash
cd backend
go build -o /tmp/test-backend-gitbackup ./cmd/server
```

结果：编译成功，无错误。

### 集成点验证

功能已集成到 `api.CreateTask` (api.go:1392)，在获取projectRoot后自动执行备份。

**行为：**
1. 非git目录自动 `git init`
2. 创建通用 `.gitignore` 排除交付目录
3. 检测变更后执行 `git add -A` 和 `git commit`
4. 无变更时跳过提交
5. 失败只记录日志，不阻塞任务

**Commit消息格式：** `对话开始前自动备份 2026-09-03 20:12:37`

## 2026-08-30：1.6.48 四端发布验证

### 健康检查

```bash
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/healthz
```

结果：`{"status":"ok"}`

### 版本接口

```bash
curl -sS --max-time 10 https://www.xyapi.top/codex/api/app/version
```

结果：返回 `version=1.6.48`、`version_code=195`，Android、Windows、macOS 三个下载项均为 `available=true`。

### 下载接口

```bash
curl --http1.1 -L --fail -H 'Range: bytes=0-1023' \
  https://www.xyapi.top/codex/api/app/download?v=195
curl --http1.1 -L --fail -H 'Range: bytes=0-1023' \
  'https://www.xyapi.top/codex/api/app/download?artifact=chat-codex-windows-x64-setup.exe&v=195'
curl --http1.1 -L --fail -H 'Range: bytes=0-1023' \
  'https://www.xyapi.top/codex/api/app/download?artifact=chat-codex-darwin-arm64.dmg&v=195'
```

结果：三个接口均返回 HTTP `206`，Range 内容长度为 `1024` 字节；完整下载 SHA-256 与本地构建产物一致。

## 2026-08-30：手动压缩上下文刷新修复验证

### 线上健康检查

```bash
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/healthz
```

结果：`{"status":"ok"}`。

### Flutter 完成事件 usage 合约

```bash
cd flutter_app
flutter test test/features/chat/presentation/goal_settings_test.dart \
  --plain-name "Completed event reads context usage from backend metadata"
```

结果：通过。验证 Flutter 能从后端真实的 `completed.metadata.usage` 结构读取 `compaction_count_tokens=12500` 和 `context_usage_percent=12.5`，并正确结束任务状态。

### Agent Relay 手动压缩合约

```bash
cd my-opencode2/packages/opencode
env -u OPENCODE_CONFIG_CONTENT \
  -u OPENCODE_DISABLE_PROJECT_CONFIG \
  -u OPENCODE_PROJECT_ROOT \
  HOME=/tmp/chat-codex-test-home \
  XDG_CONFIG_HOME=/tmp/chat-codex-test-config \
  XDG_DATA_HOME=/tmp/chat-codex-test-data \
  XDG_CACHE_HOME=/tmp/chat-codex-test-cache \
  bun test test/server/relay.test.ts \
  --timeout 30000 \
  --test-name-pattern "manual compaction|closes compaction"
```

结果：3 个测试全部通过，共执行 16 个断言：

- 成功链路完成真实会话创建和手动压缩，最终 usage 为 `compaction_context_ready`，`compaction_count_tokens > 0`，占用比例字段为非负数。
- 压缩摘要模型返回 HTTP 401 时，只发送 `task.failed`，不会发送 `task.compaction_completed` 或 `task.completed`。
- 带错误的 compaction summary message 不会被识别为压缩完成。

补充结果：`bun typecheck`、Flutter usage 回写测试和目标文件 `git diff --check` 均通过。

## 2026-08-30：进入对话定位最新消息验证

### 最近会话与消息排序接口

```bash
curl --fail -sS \
  -H 'Authorization: Bearer <TOKEN>' \
  'https://www.xyapi.top/codex/api/sessions?agent_id=agent_d720b0e9&limit=20'

curl --fail -sS \
  -H 'Authorization: Bearer <TOKEN>' \
  'https://www.xyapi.top/codex/api/tasks?session_id=<LATEST_SESSION_ID>&limit=6&sort=created_desc'
```

结果：通过。XYverify 会话列表按 `updated_at` 倒序返回，最新会话的 6 条任务按 `created_at` 倒序返回；后端返回顺序正确，旧消息定位问题位于 Flutter 客户端滚动生命周期。

### Flutter 滚动回归

```bash
cd flutter_app
flutter test test/features/chat/presentation/chat_history_scroll_test.dart
flutter test test/features/chat/presentation/goal_settings_test.dart \
  --plain-name "Initial session load waits for snapshot hydration"
flutter analyze \
  lib/features/chat/presentation/chat_page.dart \
  lib/features/chat/presentation/chat_provider.dart \
  test/features/chat/presentation/chat_history_scroll_test.dart \
  test/features/chat/presentation/goal_settings_test.dart
```

结果：通过。滚动测试 16 项全部通过，首次会话快照等待测试通过，相关文件静态分析无问题。

## 2026-08-31：上下文压缩保留策略优化验证

### OpenCode 压缩定向测试

```bash
cd my-opencode2/packages/opencode
env -u OPENCODE_CONFIG_CONTENT \
  -u OPENCODE_DISABLE_PROJECT_CONFIG \
  -u OPENCODE_PROJECT_ROOT \
  HOME=/var/folders/7g/csw1lbw93b55w0dw66q942600000gn/T/opencode/isolated-home \
  XDG_CONFIG_HOME=/var/folders/7g/csw1lbw93b55w0dw66q942600000gn/T/opencode/isolated-config \
  XDG_DATA_HOME=/var/folders/7g/csw1lbw93b55w0dw66q942600000gn/T/opencode/isolated-data \
  XDG_CACHE_HOME=/var/folders/7g/csw1lbw93b55w0dw66q942600000gn/T/opencode/isolated-cache \
  bun test test/session/compaction.test.ts --timeout 30000
bun typecheck
```

结果：`56 pass`、`0 fail`、`178 expect() calls`。验证默认保留最近 6 个 turn，显式 `tail_turns` 配置仍然生效；滚动摘要提示保留事实账本、具体错误、命令、路径、标识符和冲突事实；压缩 fallback 的工具输出保留上限提高到 1500 字符。`bun typecheck` 通过。

## 2026-08-31：消息记录本地化第一阶段验证

### 线上健康检查

```bash
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/healthz
```

结果：返回 `{"status":"ok"}`。

### Flutter 本地缓存接入回归

```bash
cd flutter_app
flutter analyze \
  lib/features/chat/data/local_chat_store.dart \
  lib/features/chat/data/local_chat_store_stub.dart \
  lib/features/chat/data/local_chat_store_sqlite.dart \
  lib/features/chat/data/chat_repository.dart \
  lib/features/chat/presentation/chat_provider.dart
flutter test test/features/chat/data/chat_repository_model_test.dart \
  test/features/chat/presentation/chat_history_scroll_test.dart
```

结果：目标文件静态分析无问题；聊天仓库模型测试和历史滚动测试全部通过，共 25 项测试通过。验证服务端任务原始 JSON 可回填本地 SQLite 任务表，Web 使用内存降级实现，SSE 事件写入事件去重表不阻塞当前渲染链路。

### Windows 构建机实机验证

```text
构建机：我的windows（192.168.10.105），Windows 用户 17150
Flutter：3.38.5，Dart 3.10.4
构建命令：powershell -NoProfile -ExecutionPolicy Bypass -File windows\build_installer.ps1
```

结果：Windows release 构建成功，Inno Setup 6.7.3 打包成功；安装包内确认包含 `sqlite3.dll` 和 `sqlite3_flutter_libs_plugin.dll`。在 Windows 实机运行 `lib/local_chat_store_smoke.dart`，输出 `LOCAL_CHAT_STORE_WINDOWS_SMOKE_OK`，实际通过 SQLite 建库、任务写入读取、账号隔离和重复事件去重验证。运行时出现远程图形上下文 DirectX/EGL 警告，但不影响 smoke 进程完成存储断言。

随后使用该安装包执行静默安装，安装文件检查返回 `True`，路径为 `%LOCALAPPDATA%\Programs\Chat Codex\chat_codex.exe`。启动检查受到远程桌面图形上下文限制，未将 GUI 进程持续运行作为通过条件；SQLite smoke 已在同一 Windows 原生环境完成。

## 2026-08-31：服务端增量校准协议验证

### Flutter 与后端增量接口回归

```bash
cd backend
go test ./...

cd ../flutter_app
flutter analyze \
  lib/features/chat/data/local_chat_store.dart \
  lib/features/chat/data/local_chat_store_stub.dart \
  lib/features/chat/data/local_chat_store_sqlite.dart \
  lib/features/chat/data/chat_repository.dart \
  lib/features/chat/presentation/chat_provider.dart
flutter test test/features/chat/data/chat_repository_model_test.dart \
  test/features/chat/presentation/chat_history_scroll_test.dart
```

结果：后端全部 Go 包测试通过；Flutter 目标文件静态分析通过，25 项聊天回归测试通过。新增接口为 `GET /api/tasks/delta?session_id=<SESSION_ID>&after_revision=<RFC3339_UPDATED_AT>|<TASK_ID>`，首次无 `after_revision` 时返回当前任务页和 revision，后续只返回游标之后更新的任务。该协议代码已完成本地验证并随后部署到公网服务器。

### 线上部署与真实会话闭环

```bash
./deploy.sh --backend-only
curl --fail -sS --max-time 15 https://www.xyapi.top/codex/healthz
curl --fail -sS --get \
  'https://www.xyapi.top/codex/api/tasks/delta' \
  --data-urlencode 'session_id=<REAL_SESSION_ID>' \
  --data-urlencode 'limit=5' \
  -H 'Authorization: Bearer <TOKEN>'
curl --fail -sS --get \
  'https://www.xyapi.top/codex/api/tasks/delta' \
  --data-urlencode 'session_id=<REAL_SESSION_ID>' \
  --data-urlencode 'after_revision=<REVISION_FROM_PREVIOUS_RESPONSE>' \
  --data-urlencode 'limit=5' \
  -H 'Authorization: Bearer <TOKEN>'
```

结果：后端部署脚本完成，`chat-codex-backend.service` 为 `active (running)`，nginx 配置检查通过，健康接口返回 `{"status":"ok"}`。admin 真实会话首次请求返回 5 条任务和 revision `2026-08-31T13:55:44Z|task_1788183016099035583`；携带该 revision 再请求返回 0 条任务，确认无变化时不会重复传输任务数据。

## 2026-08-31：SSE 断点续传本地验证

```bash
cd backend
go test ./...

cd ../flutter_app
flutter analyze \
  lib/core/config/api_client.dart \
  lib/core/config/sse_io.dart \
  lib/core/config/sse_web.dart \
  lib/features/chat/data/chat_repository.dart
flutter test test/core/config/sse_parser_test.dart \
  test/features/chat/data/chat_repository_model_test.dart \
  test/features/chat/presentation/chat_history_scroll_test.dart
```

结果：后端全部 Go 包测试通过；Flutter SSE 解析和聊天回归测试全部通过。事件新增 `sequence` 字段，服务端 SSE 发送 `id:`，客户端重连会携带 `Last-Event-ID`。本地代码验证通过，随后已部署线上并完成真实 SSE 验证。

## 2026-08-31：线上 SSE Last-Event-ID 验证

```bash
./deploy.sh --backend-only
curl --fail -sS --max-time 15 https://www.xyapi.top/codex/healthz
curl --fail -sS -N --max-time 8 \
  'https://www.xyapi.top/codex/api/tasks/<TASK_ID>/events' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Last-Event-ID: 3118'
```

结果：后端重新部署成功，`chat-codex-backend.service` 为 `active (running)`，健康接口通过。线上真实任务 SSE 返回标准 `id` 和 JSON `sequence`；携带 `Last-Event-ID: 3118` 重连时收到 0 条编号大于 `3118` 的事件，确认已确认事件不会重复发送。

## 2026-08-31：Android 前台后台任务通知实机验证

```bash
cd flutter_app
flutter build apk --debug \
  --dart-define=CHAT_CODEX_DEFAULT_BASE_URL=https://www.xyapi.top/codex
adb install -r build/app/outputs/flutter-apk/app-debug.apk
adb shell am force-stop com.chatcodex.chat_codex_app
adb shell monkey -p com.chatcodex.chat_codex_app 1
adb shell dumpsys activity services com.chatcodex.chat_codex_app
adb shell dumpsys notification --noredact
```

结果：APK 在 USB Android 16 设备 `PLC110` 上编译并安装成功，包版本为 `1.6.50 (197)`，通知权限已授予。应用启动后 `GlobalOverlayService` 已处于前台服务状态；即使没有开启全局助手，服务也会保持任务事件监听并可发送任务完成系统通知。系统已注册 `task_completion` 通知频道，通知点击参数会恢复对应设备、Agent、项目和会话路由。任务完成通知需在设备上运行实际任务后由服务事件触发。

补充验证：修正真正应用入口 `lib/main.dart` 后，应用启动服务 Intent 为 `START_NOTIFICATIONS`，服务保持前台运行；`dumpsys window` 未发现 `TYPE_APPLICATION_OVERLAY`，确认不会因后台通知服务自动打开全局助手。JPush 初始化改为可选流程，不影响后台任务通知服务启动。

补充行为：Android 主界面处于前台时，任务完成只更新应用内状态，不发送系统完成通知；应用进入后台后，任务完成才发送系统通知，避免前台重复提醒和通知折叠干扰。

根据 USB 实机反馈，将任务完成通知迁移到新的高重要性通知频道 `task_completion_v2`，使用高优先级、声音和振动；前台应用抑制规则保持不变，避免旧频道被系统折叠后无法通过代码恢复可见性。

## 2026-09-01：Android 后台任务完成通知最终闭环验证

```bash
cd backend
go test ./...
go test -race ./internal/store ./internal/notify ./internal/overlay ./internal/api ./internal/app

cd ../flutter_app
flutter build apk --debug --dart-define=CHAT_CODEX_DEFAULT_BASE_URL=https://www.xyapi.top/codex
adb install -r build/app/outputs/flutter-apk/app-debug.apk
adb shell am force-stop com.chatcodex.chat_codex_app
adb shell monkey -p com.chatcodex.chat_codex_app 1
adb shell input keyevent KEYCODE_HOME
```

结果：后端完整测试和相关竞态测试全部通过；debug APK 编译并安装到 USB Android 16 设备 `PLC110`。线上 `chat-codex-backend.service` 通过 `./deploy.sh --backend-only` 更新，systemd、nginx 和 `/codex/healthz` 均正常。

使用 admin 账户写入一条 `chat-task` 未读完成同步记录后，设备顶层为系统桌面，服务日志确认 `foreground=false`、通知权限 `enabled=true`、频道 `task_completion_v2` 的 `channelImportance=4`，并成功输出 `task notification posted`。验证了持久化通知 cursor、SSE 断线回放、后台系统通知投递和通知点击参数链路；测试记录随后已通过 dismiss 接口清理。

补充验证：针对设备未主动弹出横幅的问题，新建频道 `task_completion_v3`，显式开启 `IMPORTANCE_HIGH`、`showBanner`、声音、振动和指示灯。设备实际注册结果为 `mImportance=4`、`mShowBanner=true`、`mVibrationEnabled=true`；后台回放版本 `8632` 时成功在该频道投递通知。厂商系统仍可能根据用户的通知横幅设置决定是否显示悬浮横幅，通知栏投递不受影响。

## 2026-09-01：1.6.51 全量发布验证

```bash
./deploy.sh --backend-only
./deploy.sh --frontend-only
./deploy.sh --android-only
./deploy.sh --cli-only
./deploy.sh --launcher-only

curl --fail -sS --max-time 10 https://www.xyapi.top/codex/healthz
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/api/app/version
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/api/launcher/version
```

结果：后端、Flutter Web、Android APK、OpenCode CLI 和 Launcher 均发布成功。客户端线上版本为 `1.6.51+198`，Android 下载接口返回 HTTP `206` 且完整文件 SHA-256 校验通过；OpenCode `1.15.85` 的 Windows、macOS ARM64、Linux x64 包均可用并通过 Range/SHA-256 校验；Launcher `0.1.175` 的 CLI、GUI 和六个平台 tunnel 文件均可用并通过 Range/SHA-256 校验。健康接口返回 `{"status":"ok"}`，Launcher 版本接口返回 `0.1.175`。

## 2026-09-01：1.6.51 Windows 安装包构建验证

```powershell
Set-Location C:\Users\17150\chat-codex-release-1.6.51-198
.\windows\build_installer.ps1 -BaseUrl https://www.xyapi.top/codex
```

结果：在 Windows 构建机 `192.168.10.114` 上完成 Flutter Windows Release 编译，并由 Inno Setup 6.7.3 成功生成 `ChatCodex-1.6.51-windows-x64-setup.exe`。本地交付文件识别为 Windows PE GUI 可执行文件，大小约 13 MB，SHA-256 为 `3f2669c0bc7a52c660247c242303d85e6de091ae891146787e19be61b8ab0e37`。

## 2026-09-01：Windows 安装包线上发布验证

```bash
CHAT_CODEX_WINDOWS_INSTALLER="$PWD/.chatcodex-artifacts/task_1788273970274974192/ChatCodex-1.6.51-windows-x64-setup.exe" ./deploy.sh --mobile-app-only
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/healthz
curl --fail -sS --max-time 10 https://www.xyapi.top/codex/api/app/version
```

结果：Windows 安装包已上传至生产服务器，客户端版本 `1.6.51+198` 的 Android、Windows、macOS 下载项均返回 `available=true`。Windows 下载接口返回 HTTP `206`，完整下载 SHA-256 为 `3f2669c0bc7a52c660247c242303d85e6de091ae891146787e19be61b8ab0e37`，与 Windows 构建机产物一致；健康接口返回 `{"status":"ok"}`。

## 2026-09-03：Git 运行时 npm_global_tool 安装策略实现

### 配置文件修改

#### Backend Catalog
```bash
cat backend/internal/app/builtin/catalog_v4.json | jq '.runtime_catalog[] | select(.id == "git")'
cat backend/internal/app/builtin/catalog_v4.json | jq '.runtime_versions[] | select(.runtime_id == "git")'
```

结果：通过。添加了 `git` runtime（sort_order: 21）和 `git-latest` version 配置，install_strategy 为 `npm_global_tool`，npm_package 为 `dugite`，requires_runtime 为 `node`。

#### Launcher Catalog
```bash
cat launcher/internal/app/builtin/catalog_v4.json | jq '.runtime_catalog[] | select(.id == "git")'
cat launcher/internal/app/builtin/catalog_v4.json | jq '.runtime_versions[] | select(.runtime_id == "git")'
```

结果：通过。Launcher 配置与 backend 保持一致，JSON 格式正确。

### 代码实现验证

#### Backend 依赖检测
```bash
grep -n "npm_global_tool" backend/internal/api/runtime_preflight.go
```

结果：通过。在第 417 行添加了 `npm_global_tool` 策略的 Node.js 依赖检测逻辑。

#### Launcher 安装策略
```bash
grep -n "npm_global_tool" launcher/internal/app/runtime_preflight.go
```

结果：通过。在 launcher 中添加了完整的 `npm_global_tool` 安装策略实现：
- 第 468 行：依赖检测
- 第 546 行：变量声明
- 第 556 行：依赖验证
- 第 594 行：任务分组
- 第 636 行：分组添加
- 第 771 行：安装策略判断
- 第 909 行之后：`installNpmGlobalTool` 函数实现

### 编译验证

#### Backend 编译
```bash
cd backend && go build -o /tmp/test-backend ./cmd/backend
```

结果：编译成功，无错误。

#### Launcher 编译
```bash
cd launcher && go build -o /tmp/test-launcher-git2 ./cmd/launcher
```

结果：编译成功，无错误。

### 实现特性

✅ npm_global_tool 安装策略支持  
✅ 自动检测 Node.js 依赖  
✅ 使用 `npm install -g` 全局安装 npm 包  
✅ 自动查找已安装的可执行文件  
✅ 生成运行时 manifest  
✅ 支持 dugite（Git 的 npm 包装）  
✅ 按依赖顺序分组安装（Node.js → npm tools）  
✅ 错误处理和日志记录

### 待集成测试

需要启动后端服务并创建任务验证完整链路：

```bash
# 1. 启动后端
cd backend && ./backend

# 2. 查询 Git 运行时配置
curl http://localhost:8080/api/runtime-catalog | jq '.runtimes[] | select(.id == "git")'

# 3. 创建任务请求 Git 运行时
curl -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "测试Git运行时",
    "runtimes": ["git-latest"]
  }'

# 4. 验证安装过程
# - Node.js 是否已安装
# - 是否执行 npm install -g dugite
# - git 命令是否可用
```

### 网络配置支持

#### 代理方式（自动继承）
```bash
# 在启动前设置代理
export https_proxy=http://127.0.0.1:7897
export http_proxy=http://127.0.0.1:7897
export all_proxy=socks5://127.0.0.1:7897

# 启动launcher
cd launcher && ./launcher
```

#### npm镜像方式（推荐，无需代理）
```bash
# 方式1：环境变量
export LAUNCHER_NPM_REGISTRY=https://registry.npmmirror.com
cd launcher && ./launcher

# 方式2：npm全局配置
npm config set registry https://registry.npmmirror.com
cd launcher && ./launcher
```

详细实现文档：`git_npm_global_tool_implementation.md`
