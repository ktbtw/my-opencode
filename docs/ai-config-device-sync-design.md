# AI 配置设备同步（设计稿）

> 状态：待评审 ｜ 改动面：仅 Flutter App

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

## 9. 决策点（待确认）

1. 技术路线：确认走「方案 A：Flutter 端合并」？
2. 冲突默认值：推荐「默认使用设备A的配置，可逐项改」；备选「必须逐项选择后才能点开始同步」。
3. 默认模型：随同步（开关默认勾选）可以吗？还是默认不勾选？
4. 同步范围：按全量同步（不做部分勾选）。如需「勾选部分供应商」，请说明。
