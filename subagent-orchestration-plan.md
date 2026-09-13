# 自动子代理编排计划

## 目标

在不改变主 Agent 最终写入、安装、发布权的前提下，让 Relay 自动编排最多 5 个只读/测试子代理；所有任务、日志、结果和恢复状态由 Backend 持久化，并在 Flutter 对话中实时显示和控制。

## 已确认的决策

| 项目 | 决策 |
| --- | --- |
| 触发 | 主 Agent 自动拆解并立即派发 |
| 并发 | 单个主任务最多 5 个；设备全局默认最多 5 个 |
| 子代理权限 | 仅探索、研究、分析、审查、测试；禁止 edit、安装、发布和写入型 MCP |
| 主 Agent | 派发后继续协调；不重复子代理研究；负责所有副作用和最终合并 |
| 结果回注 | 每个结果立即落库并插入主任务上下文；关键结果或一批完成才唤醒主循环 |
| 故障 | 单节点失败不阻塞其他分支；主 Agent 使用已完成证据继续判断 |
| 保存与恢复 | 任务图、日志、产物索引和结果随会话永久保存；Relay/App 重连自动恢复 |
| 用户控制 | 查看树与日志、取消、暂停/恢复、追加指导、修改后续轮次模型与优先级 |
| 自定义 Agent | 由已配置 Skill、MCP、权限推导子角色，管理员可覆写 |

## 内置 Agent 与子角色

| 主 Agent | 允许的自动子角色 | 推荐任务 |
| --- | --- | --- |
| `coding-assistant` | `repo-explorer`、`implementation-planner`、`test-runner`、`code-reviewer`、`dependency-researcher` | 代码定位、方案对比、测试、审查、依赖调研 |
| `reverse-android` | `apk-scout`、`manifest-mapper`、`native-tracer`、`protocol-analyst`、`verification-runner` | APK 结构、组件与权限、so 调用链、协议、可重复验证 |
| `reverse-ios` | `ipa-scout`、`macho-mapper`、`objc-swift-tracer`、`runtime-observer`、`verification-runner` | IPA/Mach-O、符号与调用链、运行时观察、验证 |
| `reverse-windows` | `pe-scout`、`import-export-mapper`、`control-flow-analyst`、`installer-analyzer`、`verification-runner` | PE/DLL、导入导出、控制流、安装包、验证 |
| `reverse-mac` | `bundle-scout`、`codesign-entitlements-auditor`、`macho-mapper`、`objc-swift-tracer`、`runtime-observer` | App bundle、签名与权限、Mach-O、运行时观察 |
| `reverse-web` | `asset-scout`、`js-wasm-analyst`、`request-flow-mapper`、`browser-observer`、`verification-runner` | 静态资源、JS/WASM、请求状态机、浏览器观察 |

每个角色是受限执行配置，不是新的设备 Agent：它继承父 Agent 可读的 Skill/MCP 子集，使用独立 OpenCode 子会话、独立模型选择、取消令牌、预算和日志流。

## 文件架构

```text
my-opencode2/packages/opencode/src/orchestration/
  types.ts                 # 子任务、批次、事件、状态和幂等键
  policy.ts                # 语义 Agent -> 子角色/MCP/工具白名单
  planner.ts               # 校验主 Agent 给出的 DAG 派发计划
  scheduler.ts             # 依赖、配额、优先级、暂停、取消和超时
  subagent-runner.ts       # OpenCode 子会话创建、继续、取消和状态查询
  context-gate.ts          # 结果回注与主循环唤醒判定
  result-normalizer.ts     # 摘要、证据、产物、错误统一结构
  recovery.ts              # Relay 重连和不确定子会话恢复
  index.ts                 # 对 Relay 暴露的编排服务
my-opencode2/packages/opencode/test/orchestration/
  policy.test.ts planner.test.ts scheduler.test.ts context-gate.test.ts
  subagent-runner.test.ts recovery.test.ts integration.test.ts

backend/internal/orchestration/
  model.go                 # 数据库 DTO 和状态迁移校验
  store.go                 # Repository 接口及内存实现
  mysql_store.go           # MySQL 查询和迁移
  service.go               # 事件幂等、控制命令、恢复协调
  settings.go              # 账号级角色/模型/配额配置
backend/internal/api/
  orchestration.go         # HTTP/SSE 控制面
  orchestration_test.go    # API 和 SSE 回归
backend/internal/store/
  orchestration_archive.go # 归档层适配

flutter_app/lib/features/chat/
  data/subagent_models.dart
  data/subagent_repository.dart
  presentation/subagent_tree_notifier.dart
  presentation/widgets/subagent_task_tree.dart
  presentation/widgets/subagent_task_node.dart
  presentation/widgets/subagent_log_sheet.dart
  presentation/widgets/subagent_control_bar.dart
flutter_app/test/features/chat/
  data/subagent_models_test.dart
  presentation/subagent_tree_notifier_test.dart
  presentation/subagent_task_tree_test.dart
```

`server/relay.ts`、`internal/app/app.go` 和 `chat_provider.dart`只负责协议接线、快照转发和现有会话事件合并；不新增第二套队列或会话状态机。

## 状态与事件约束

节点状态：`planned -> queued -> running -> paused -> running -> completed|failed|cancelled|timed_out|unknown`。终态不可回退；`unknown` 只允许通过真实子会话查询收敛为终态或显式重试。

核心事件：`orchestration.plan_created`、`subagent.queued`、`subagent.started`、`subagent.progress`、`subagent.result`、`subagent.control_applied`、`subagent.recovered`、`orchestration.batch_ready`。每个事件带 `operator_id`、`task_id`、`node_id`、`attempt`、`sequence`、`occurred_at`；写入以 `node_id + attempt + terminal_at` 去重。

结果通过既有 `task.input_applied` 链路写入 `context_type=subagentResult`。普通进度只更新任务树；关键结论、所有依赖满足、主循环正等待或用户插入到达时才唤醒主 Agent。用户插入与子代理结果使用同一 `inputChain` 串行化。

## 边界处理

| 场景 | 预期行为 |
| --- | --- |
| 计划含环、未知角色、超过 5 个并发或越权工具 | 拒绝该计划，返回可定位的结构化错误，不创建子会话 |
| 子代理请求写入/安装/发布/写入型 MCP | 拦截并生成 `requires_primary_agent` 结果；主 Agent 决定是否接管 |
| 同一完成事件重放 | 幂等忽略，不重复注入上下文或唤醒主循环 |
| 子代理失败、超时、取消 | 节点终态落库，独立分支继续；依赖该节点的节点标记阻塞原因 |
| 父任务取消 | 停止未启动节点、取消运行节点、保留已完成日志与结果 |
| Relay 重启/App 断开 | 从 Backend 快照恢复；查询子会话；无法确认则标记 `unknown`，绝不盲目重跑 |
| 用户改模型/优先级 | 只影响未开始节点或运行节点的下一轮；记录控制事件 |
| 模型/设备全局拥塞 | 节点保持 `queued`，展示排队原因；不占用父任务完成判定 |
| 同时存在后台 Job | 与子代理结果共同进入 `inputChain`，按事件序列回注，分别渲染 |

## 执行计划

| 阶段 | 工作项 | 依赖 | 完成标准 |
| --- | --- | --- | --- |
| 1 | 定义 OpenCode 编排类型、角色策略、计划 schema 和只读工具派生 | 无 | 类型测试覆盖所有状态和角色映射 |
| 2 | 实现 DAG planner、scheduler、超时/预算/取消和 `context-gate` | 1 | 并发、依赖、幂等与唤醒单测通过 |
| 3 | 实现 `subagent-runner`，接入原生 `task` 子会话与 Relay 恢复 | 1-2 | fixture 验证创建、完成、恢复、取消和未知态 |
| 4 | 建立 Backend 任务图表、Store、迁移、账号设置和事件归档 | 1 | MySQL/Memory 一致性、版本冲突和权限隔离测试通过 |
| 5 | 提供 Backend 控制 API、SSE 快照和 Relay 事件桥接 | 2-4 | API/SSE 测试验证顺序、重放、断线补全和控制命令 |
| 6 | 实现 Flutter 数据层、任务树、日志抽屉和节点控制栏 | 5 | Widget/Notifier 测试验证状态、控制、折叠日志和恢复 UI |
| 7 | 接入账号设置页：自动编排、并发、角色模型映射、自定义 Agent 覆写 | 4-6 | 设置跨设备读写、默认回退和模型影响范围测试通过 |
| 8 | 构建跨进程集成 fixture，覆盖主 Agent、5 子代理、后台 Job 与用户插入交错 | 2-7 | 一次完整任务从计划到汇总的事件序列完全匹配 |
| 9 | 故障注入、性能回归、发布前全量验证与版本发布 | 8 | 下列全部命令成功，线上 staging 任务图可恢复 |

## 测试清单

| 范围 | 必测场景 |
| --- | --- |
| OpenCode 单元 | 五并发上限、优先级抢占、DAG 依赖、循环拒绝、角色权限缩减、模型回退、超时、取消级联、结果幂等、关键结果门控 |
| OpenCode 集成 | 原生子会话创建/恢复/取消、同任务用户插入与子结果串行化、后台 Job 和子代理交错、主循环不提前完成 |
| Backend 单元 | 状态迁移、乐观版本冲突、账号/会话隔离、永久日志分页、设置覆盖与默认回退 |
| Backend API/SSE | 快照、补丁、重连、控制命令授权、重复事件、顺序反转、离线设备排队 |
| Flutter 单元/Widget | 任务树拓扑排序、状态文案、失败证据、日志折叠、暂停/恢复/取消、后续轮次模型标识、恢复/未知态 |
| 联合 E2E | 5 个子代理，1 个失败、1 个超时、用户插入、后台 Job 完成、Relay 重启、App 重连、主 Agent 汇总且不重复调用 |
| 故障/性能 | Store 暂时失败、模型慢首包、子会话丢失、SSE 抖动、重复投递、连续 20 个任务不泄漏并发槽位 |

## 发布前验证

```bash
cd my-opencode2/packages/opencode && bun typecheck && bun test test/orchestration
cd backend && go test ./internal/orchestration ./internal/api ./internal/app
cd flutter_app && flutter analyze lib/features/chat && flutter test test/features/chat
cd backend && go test ./... && cd ../my-opencode2/packages/opencode && bun test
```

最后执行 Relay + Backend + Flutter fixture；断线恢复、重复结果和主任务汇总必须同时通过，才允许提升版本、构建三端包并部署。

## 决策记录

1. 采用 Relay 编排、Backend 持久化、OpenCode 子会话执行的单一协调架构。
2. 不使用 Launcher 作为任务调度器，也不新建独立聊天队列。
3. 所有内置语义 Agent（包括默认禁用的 `reverse-mac`）具备领域限定子角色。
4. 以 `task.input_applied` 作为子代理和后台 Job 的统一主任务上下文入口。
