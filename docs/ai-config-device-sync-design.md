# AI 配置设备同步（设计稿）

> 状态：已确认，按方案 A 实施 ｜ 改动面：仅 Flutter App

## 1. 需求

- 设备 A 的「AI配置」页新增同步入口：点击弹出设备列表，选择目标设备 B，把 A 的 AI 配置同步到 B。
- 出现供应商冲突（两台设备存在同名供应商且内容不一致）时，由用户选择保留哪一个。

## 2. 现状调研（结论）

AI 配置保存在设备端 opencode 全局配置文件（`opencode.json`）中：

- `provider`：供应商映射。每个供应商对象含 `options.apiKey`（真实密钥）、`options.baseURL`、`options.apiMode`、`console_url` 元数据、`models`（模型清单，含手填窗口、思考档位、预设标记等）。
- `model`：默认模型（`供应商/模型` 引用）。

现有链路（App → 后端 → Launcher）已具备同步所需的全部能力：

- 读取：`GET /api/devices/{machineId}/ai-config` 返回完整配置，其中 `raw_json` 是配置文件原文（含真实密钥）——现有「编辑全局配置」功能即基于它，已在现网使用。
- 写入：`POST /api/devices/{machineId}/ai-config/save-text` 整段文本写回设备（设备端做 JSONC 解析 → 原子写文件 → 自动重载 Agent）——现有「编辑全局配置」保存走的就是这条链路。
- 设备列表：`GET /api/devices`（含在线状态、自定义名称）。

结论：**同步功能不需要新增后端 / Launcher 能力，Flutter 端即可完整实现。**

## 3. 方案选择

| 方案 | 说明 | 结论 |
| --- | --- | --- |
| A. Flutter 端合并（选定） | 读 A、读 B → 端上计算差异 → 用户确认 → 以 B 为底合并 A 的供应商对象 → save-text 写回 B | 只改一个端；复用现网接口；对象级复制保真；可单元测试 |
| B. 后端新增同步接口 | 后端拉两端配置做合并 | 需发后端 + 新增接口；App 仍需读双方配置做冲突 UI；改动面大，不选 |
| C. Launcher 新增 merge 命令 | 目标设备端合并 | 三端都要发；冲突检测仍需 App 侧完成；收益低，不选 |

方案 A 的两个前提已核实：① `raw_json` 含真实密钥（现网行为）；② `save-text` 是既有保存链路。

## 4. 交互设计

### 4.1 入口

AI配置页顶栏操作区最左侧新增同步图标（`sync_alt`，tooltip「同步到其他设备」）。

- 加载中 / 保存中禁用；当前设备没有供应商时点击提示「当前设备还没有配置 AI 供应商」。
- 关键步骤写 App 日志（`device_ai_config_sync_*`）。

### 4.2 设备选择弹窗

- 打开后加载设备列表（加载态）；列出除当前设备外的全部设备：名称（自定义名 / 主机名）、在线状态。
- 在线设备可选；离线设备置灰并标「离线」（同步要求目标设备在线）。
- 无其他设备时显示空状态「没有可同步的其他设备」。

### 4.3 同步确认弹窗（含冲突处理）

选中 B 后并行读取 A / B 配置，计算同步计划：

- **新增**：A 有、B 无 → 直接列入同步。
- **冲突**：两边同名且原始对象不一致 → 逐项选择「使用设备A的配置 / 保留设备B的配置」。
- **一致**：两边同名且完全一致 → 自动跳过（汇总一行说明）。
- **仅 B 有**：保持不动（汇总一行说明）。

弹窗结构（示意）：

```text
同步 AI 配置
目标设备：设备B（host-2）

将新增 2 个供应商：p1、p2
2 个供应商冲突，请选择保留哪个：
┌ p3 ───────────────────────────────────────────────┐
│ 设备A：https://a.example.com · sk-****ab · 5 个模型  │
│ 设备B：https://b.example.com · sk-****cd · 3 个模型  │
│ (•) 使用设备A的配置   ( ) 保留设备B的配置            │
└────────────────────────────────────────────────────┘
（每项只列出存在差异的字段，如接口地址 / API Key / API 模式 / 模型数量）

[✓] 同步默认模型：设备A 的 p1/m1（设备B 当前：p2/m2）
     ↑ 仅当 A 已设置默认模型时显示，可取消

[取消]                              [开始同步]
```

- 无任何变化时：提示「两台设备的 AI 配置已一致，无需同步」。
- 读取失败：弹窗内显示错误 + 重试。

### 4.4 执行与反馈

- 点「开始同步」后按钮转圈（单次 save-text 写入，设备端原子写 + 重载 Agent）。
- 成功：关闭弹窗，页面提示「已同步到 设备B：新增 N 个供应商、更新 M 个」，并失效 B 的模型缓存。
- 失败：弹窗内展示错误，可重试。

## 5. 实现拆分（Flutter）

### 5.1 新增 `lib/features/devices/application/ai_config_sync.dart`（纯逻辑，无 UI）

- `stripJsonc`：去注释 / 尾逗号（移植 launcher 现有算法，附中文注释）。
- `parseDeviceConfigRaw`：JSONC 文本 → Map。
- `canonicalJson`：键递归排序后序列化，用于等价比较。
- `AiConfigSyncPlan buildAiConfigSyncPlan({sourceRaw, targetRaw})`：分类 add / conflict / identical / targetOnly + 默认模型差异。
- `describeProviderDiff`：冲突项差异描述（只列有差异的字段）。
- `buildSyncedConfigText({targetRaw, sourceRaw, overwriteProviderIds, syncDefaultModel})`：以 B 为底，覆盖 / 新增供应商对象，可选写入 `model`。

### 5.2 新增 `lib/features/devices/presentation/widgets/ai_config_sync_dialogs.dart`

- `AiConfigSyncDeviceDialog`：设备选择。
- `AiConfigSyncPlanDialog`：加载 / 错误 / 计划确认三态 + 冲突选择。

### 5.3 修改 `lib/features/devices/presentation/device_ai_config_page.dart`

- 顶栏新增图标；编排：拉设备列表 → 并行读 A/B → 计算计划 → 用户确认 → 应用 → 反馈。
- 复用 `deviceRepositoryProvider` 现有方法（`getDeviceAIConfig` / `saveDeviceAIConfigText`），不新增接口。

## 6. 规则与边界

1. 同步范围：A 的全部供应商（对象级复制：密钥、模型清单、手填窗口、思考档位、预设标记等原样带走）+ 默认模型（可关）。B 独有的供应商与其他字段保持不动。
2. 冲突定义：同名供应商且原始对象规范化后不一致；一致则跳过。比较基于配置原文，密钥差异也能识别。
3. 目标设备必须在线；A 读取失败（如离线）则中止并提示。
4. B 无配置文件（空配置）时全部按新增处理。
5. 写入为单次 `save-text`：设备端原子写文件并重载 Agent，不会出现半同步状态。
6. 不新增后端 / Launcher 接口；不修改设备端配置格式。

## 7. 测试计划

- 单元测试（flutter test）：stripJsonc（行 / 块注释、尾逗号、字符串内注释符、转义）、canonicalJson 等价性、计划分类、合并保真（B 独有保留、A 对象完整、model 开关、输出可再解析）。
- 组件测试：冲突弹窗交互（选择结果正确回传）。
- `flutter analyze` + `flutter test` 全量通过。
- 发布前：构建 web 推送供浏览器自测（含 A/B 两台设备真实联调），确认后再出 Android / Windows / macOS。

## 8. 发布

- 版本号：1.6.123+270（按现有节奏递增），release-notes.txt 只写本版新增。
- 仅 Flutter 一个端有改动 → 部署走 frontend-only / mobile-app-only，不动后端与 Launcher。

## 9. 决策结果（2026-10-13 确认）

1. 技术路线：方案 A（Flutter 端合并 + 复用现有接口）。
2. 冲突默认值：默认「使用设备A的配置」，可逐项改为保留目标设备。
3. 默认模型：随同步写入，弹窗内开关默认勾选。
4. 同步范围：全量同步（不做部分勾选）。

## 10. 实施记录

- 新增 `lib/features/devices/application/ai_config_sync.dart`（JSONC 清洗 / 差异计划 / 合并）。
- 新增 `lib/features/devices/presentation/widgets/ai_config_sync_dialogs.dart`（设备选择 + 同步确认）。
- `lib/features/devices/presentation/device_ai_config_page.dart` 增加顶栏同步图标与流程编排。
- 测试：`test/features/devices/application/ai_config_sync_test.dart`、`test/features/devices/presentation/ai_config_sync_dialogs_test.dart`。
- 版本：Flutter 1.6.123+270。

## 11. 验证记录（2026-10-14）

- `flutter analyze`：本功能相关文件无告警（仓库其余历史 info/warning 与本改动无关）。
- `flutter test` 全量：684 个用例通过，其中新增 27 个（JSONC 清洗 / 差异计划 / 合并保真 / 弹窗交互）。
- 真实设备链路沙箱端到端（两个 launcher 0.1.192 实例，独立 HOME/XDG，中继指向不可达地址，未注册到线上账号）：
  1. 读取两端 `raw_json`（源设备配置文件带 JSONC 注释，验证端上清洗）；
  2. 计划分类正确：新增 grok、冲突 openai、仅目标设备有 local、默认模型有差异；
  3. `save-text` 写回后回读：冲突项被源设备覆盖（含真实密钥）、新增项带入密钥、目标设备独有供应商与 `mcp`/`$schema` 保持不动、默认模型已更新；
  4. 磁盘配置文件与设备端二次上报一致，20/20 项校验通过。
- Web 已发布：https://www.xyapi.top/codex （1.6.123+270），线上 `main.dart.js` 已包含本功能。

## 12. 发布记录（2026-10-14）

- Web：已发布，`/api/app/version` 与页面均指向 1.6.123+270。
- Android APK：`chat-codex-1.6.123.apk`（35MB，sha256 `81c4bb27…4ba5`）已发布并通过线上 range/sha256 校验。
- Windows EXE：`chat-codex-windows-x64-setup.exe`（13MB，sha256 `d3e382cb…9fec`）在 Windows 构建机生成后发布，校验通过。
- macOS DMG：**未发布**。本机只有 Command Line Tools，`flutter build macos` 需要完整 Xcode；
  发布脚本为此新增 `--skip-macos`（见 `更新发布指南.md` 第 5 节），本次版本元数据里没有 macOS 下载条目。
- 版本说明只包含 1.6.123 的三条新增内容，已与 `release-history.json` 历史条目逐条比对，无重复。

### Android 签名修复

首次在本机构建时，APK 被本机新生成的 `~/.android/debug.keystore` 签名，与历史版本不一致
（证书 `40570794…` vs 历史 `afa01b3d…`）。原因是 release 构建使用 debug 签名配置，
签名随构建机变化。

- 已从百度网盘备份 `/mac全备份/batch-06/Users_yuminghao_.android.part-000`
  （sha256 与清单一致）恢复旧 Mac 的 `debug.keystore`，装回 `~/.android/debug.keystore`，
  重新构建后签名回到 `afa01b3d…`，线上 APK 已用正确签名重发。
- 归档：`~/Downloads/凭据/android-debug-keystore/`（含 README 与恢复步骤）。
- 防回归：`deploy.sh` 发布前用 `flutter_app/android/expected-signer-sha256.txt` 校验签名，
  不一致直接阻止发布（已做反向验证）。

## 13. 待办

- 装好 Xcode 后用同一版本号补发 DMG：`./deploy.sh --mobile-app-only`（需要 `CHAT_CODEX_WINDOWS_INSTALLER` 指向已构建的 EXE）。
- 线上真实两台设备联调（用真实账号在两台在线设备间点一次同步）由使用者在 web 端确认。
- `git push` 到 GitHub 未执行：本机 GitHub SSH 不通（未开代理），提交都在本地。
