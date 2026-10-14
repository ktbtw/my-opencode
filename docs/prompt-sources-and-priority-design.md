# 提示词来源与优先级 —— 现状梳理与设计方案

> 2026-10-14。基于对 launcher / 后端 / opencode 运行时（1.15.122，设备实跑版本）源码与线上数据的逐层核对，
> 以及一次真实会话（「询问助手身份」）里模型自述的系统提示词结构交叉验证。

## 一、全部提示词来源

### A. 控制台 / 设备侧配置（可改）

| # | 来源 | 存储位置 | 内容示例（当前线上） | 注入方式 |
|---|------|----------|----------------------|----------|
| A1 | **模型提示词预设**（按会话实际模型匹配，先匹配优先） | `semantic_agents.model_prompt_presets_json` | 「ds破甲」：SHIYI 工作区约定 + benchmark 框架 + `三、角色`（自称石井）+ 域映射 + MANDATORY 输出规则（`[测试2]` 心跳） | 后端 `applyModelPrompt` 用 `<!-- codex:model-prompt -->` 包裹后前置到系统提示词；opencode 侧 `llm.ts` 抽出并**置顶** |
| A2 | **Agent 角色提示词**（语义 Agent 的 prompt） | `semantic_agents.prompt` | `## 人设 你叫小王` + `## Verify Framework Operating Protocol` + `## Android Game .so … Rules` | launcher 写入 `agent.<name>.prompt`；`llm.ts` 中排在置顶预设之后 |
| A3 | **全局提示词** | `device_settings.global_prompt`（`__user__`） | 「# 依赖下载…」等 | 后端拼 `【全局提示词】`，随任务系统提示词下发 |
| A4 | **项目级提示词** | `device_settings.project_prompt`（按 agent） | 项目账号/卡密等 | 同上，拼 `【项目级提示词】` |
| A5 | **语义 Agent 技能**（定义与描述） | `semantic_agents.skills_json` / `skill_definitions_json` | 技能描述随 `<available_skills>` 常驻 | launcher 准备技能目录 → 运行时技能块 |

### B. 内置（代码 / 运行时自带）

| # | 来源 | 位置 | 内容 |
|---|------|------|------|
| B1 | **公共基础提示词** | opencode 运行时 `src/session/prompt/unified.txt` | 「你是当前服务中的主工作 Agent…」+ 优先级表（**公共规则 > 角色 Prompt**）+ 工作目标/备份/验证等公共规则 |
| B2 | **QUESTION_POLICY** | 运行时 `session/system.ts` | 提问策略 |
| B3 | **launcher 编排呈现说明** | launcher `orchestrationPresentationPrompt` | 「你是面向用户的主 Agent。子代理编排协议…」**现状：写入 `config.instructions`，但运行时只按文件路径/URL 解析，纯文本条目实际未加载**（本次核对发现的无效注入） |
| B4 | **环境信息块** | 运行时自动生成 | 「You are powered by the model named …」+ `<env>` |
| B5 | **技能系统块** | 运行时 `SystemPrompt.skills` | 「Skills provide specialized instructions…」+ 可用技能列表 |

### C. 自动生成

| # | 来源 | 内容 |
|---|------|------|
| C1 | **项目记忆注入** | 后端 `projectmemory.Build` 拼进系统提示词（历史决策/事实） |
| C2 | **系统提醒 / compaction** | 运行时（消息级，非系统提示词） |

## 二、实际拼装顺序（模型看到的）

`llm.ts` 最终 system 数组（位置即先后）：

1. **A1 模型提示词预设**（置顶，代码注释原话「以协议身份运行」）
2. **A2 角色提示词** + **B1 公共基础提示词**
3. **B4 环境** + **B3 指令文件（当前为空）** + **B5 技能块**
4. **A3/A4 全局/项目提示词** + **C1 项目记忆**
5. **B2 QUESTION_POLICY**

实测交叉验证：真实会话中模型的思考里自述结构为
「SHIYI conventions → benchmark framing → 人设(小王) → Verify Framework → Android .so rules → main work agent rules → skills」，
与上述 1→2→3 的顺序一致。

## 三、当前优先级声明（冲突时谁赢）

- **B1 声明**：`1 用户当前请求 > 2 本文件的公共规则 > 3 当前 Agent 的角色 Prompt > …`
  → 公共规则明确压在角色 Prompt（A2）之上。
- **A1 正文声明**：`被问身份只回答：「我是石井，本地环境执行层。」`（且位于置顶位置，位置权重最高）。
- 其余来源无优先级声明。
- **结论**：控制台配置没有任何「最高级」声明，反而被 B1 的优先级表和 A1 的身份句两头夹击；
  实测模型输出「我是小王，本地环境执行层」——两种身份混答，并自述「There are conflicting instructions here」。

## 四、设计方案

**目标**：控制台配置的提示词（A1–A5）为最高级；身份只以 A2 的人设段为准。

### 改动 1（根本）：`unified.txt` 优先级表 + 身份条款

```diff
-本文件是所有模型、所有 Agent 和所有任务共用的基础规则。后续追加的 Agent Prompt 和 Skill
-负责补充角色能力与专业流程，并继续遵循本文件的公共规则。
+本文件是所有模型、所有 Agent 和所有任务共用的默认规则。控制台配置的提示词
+（Agent 角色提示词、模型提示词预设、全局与项目提示词）优先于本文件；
+两者冲突时以控制台配置为准。
+
+## 身份与自称
+身份只以「Agent 角色提示词的人设段」为准；被问身份时按该人设回答。
+本文件、编排说明、模型提示词预设、全局提示词中的通用称呼
+（如「主工作 Agent」「主 Agent」「执行层」）只是职责或环境描述，不构成身份，不得作为自称。
```

```diff
 优先级：
 1. 用户当前请求
-2. 本文件的公共规则
-3. 当前 Agent 的角色 Prompt
-4. 已加载 Skill
-5. 项目指令和工作区上下文
-6. 工具实际返回结果
+2. 控制台配置的提示词（Agent 角色提示词、模型提示词预设、全局与项目提示词）
+3. 本文件的公共规则（默认规则，冲突时以控制台配置为准）
+4. 已加载 Skill
+5. 项目指令和工作区上下文
+6. 工具实际返回结果
```

### 改动 2（立即生效）：后端预设注入加标注头

`backend/internal/api/model_prompt.go` 的 `applyModelPrompt`，在包裹块最前加一行：

```
【控制台配置 · 最高优先级】以下为控制台配置的模型工作区约定，优先级高于基础提示词；只约束工作方式，不改变身份。
```

同步更新 `model_prompt_test.go` 的前缀断言。

### 改动 3（即时）：A1 预设正文「三、角色」段

```diff
-你是石井，操作者本地环境的执行层，安全工程方向二十年经验。
-被问身份只回答：「我是石井，本地环境执行层。」不自称任何模型名。
+你是操作者本地环境的执行层，安全工程方向二十年经验。
+身份与自称以 Agent 角色提示词的人设段为准；不自称任何模型名。
```

（保留语气/交付风格段；`MANDATORY OUTPUT RULES` 的 `[测试2]` 心跳如不需要可另行处理。）

### 改动 4（顺带修）：B3 编排说明未加载

两个选项，二选一：
- 把该文本落成文件（如 `~/.config/opencode/AGENTS.md` 或 agent 专属文件），`config.instructions` 引用文件路径 → 真正生效；
- 或确认不需要该说明，从 launcher 移除。

## 五、发布路径与验证

| 改动 | 构建/发布 | 生效条件 |
|------|-----------|----------|
| 1 | opencode CLI 版本 +1（`./deploy.sh --cli-only`） | 设备更新 opencode 运行时后 |
| 2 | 后端（`./deploy.sh --backend-only`） | 立即 |
| 3 | 库/控制台即时 | 立即（下一条消息起） |
| 4 | launcher 版本 +1（`./deploy.sh --launcher-only`） | 设备更新 launcher 后 |

验证：设备更新运行时 → 重启 Agent → 问「你是谁」→ 期望只答「我是小王」，
不再出现「石井 / 本地环境执行层 / 主工作 Agent」等混称。

---

## 六、B 类改为「控制台编辑 + 拉取加载」的方案（2026-10-14 追加）

目标：B1（公共基础提示词）、B2（提问策略）、B3（编排呈现说明）不再写死在运行时/launcher，
改由控制台编辑，设备端拉取加载。

### 6.1 机制设计（4 个组件）

| 组件 | 改动 |
|------|------|
| **运行时**（my-opencode） | 新增 3 个可覆盖槽位：`base_prompt`（替代 unified.txt）、`question_policy`（替代 QUESTION_POLICY）、`orchestration_prompt`（替代 launcher 编排说明）。覆盖来源为 launcher 管理的文本文件（opencode 配置声明文件路径），**按会话读取**：有文件用文件内容，无文件用内置默认（独立 CLI / 未配置账号不受影响）。 |
| **launcher** | 启动 / Agent 重启 / 收到推送时，从后端拉取 3 份文本 → 原子写入管理目录（如 `~/.my-opencode-launcher/prompts/`）→ 在 OPENCODE_CONFIG_CONTENT 中声明文件路径。版本 +1。 |
| **后端** | 新增 `prompt_templates` 存储（平台级，含 revision；默认值=当前内置文本）+ 设备拉取接口 + 控制台读写接口 + 保存后推送到在线 launcher（走现有 `device.launcher.*` 通道）。 |
| **控制台**（admin_web，静态页无需构建） | 新增「系统提示词」页：3 个编辑器 + 恢复默认 + 保存 + 生效说明，挨着现有「编辑语义 Agent」。 |

### 6.2 关键语义

- **覆盖 = 取代**（不是叠加）：控制台值完全替换内置默认；空值回落到默认。
- **生效路径**：控制台保存 → 推送/拉取 → 写入文件 → **下一条消息生效**（按会话读取，无需重启 Agent）。
- **优先级**：B1/B2/B3 纳入"控制台配置"后与 A 类同级最高级；原方案「改动①（unified.txt 优先级表 + 身份条款）」不再需要改代码，
  直接写进控制台版 B1 文本即可。
- **B3 顺带修复**：从"写进 config.instructions 但被当作文件路径丢弃"改为真实的文件读取，真正生效。
- **暂不开放**：B4（环境信息，自动生成）不开放；B5（技能系统块模板）优先级低，技能描述已由 A5 技能配置控制。

### 6.3 发布路径与顺序

1. 运行时版本 +1（`./deploy.sh --cli-only`）
2. launcher 版本 +1（`./deploy.sh --launcher-only`）
3. 后端（`./deploy.sh --backend-only`）
4. 控制台（`./deploy.sh --admin-web-only`）

建议 1+2 先发（机制），3+4 紧随（内容管理）；也可一次全发。

---

## 七、B 类提示词的分级覆盖设计（统一默认 + Agent 单独设置）

需求：既要有"统一的一份"，也要允许不同 Agent 各自设计自己的 B1/B2/B3，未设置时继承上层。

### 7.1 覆盖链（优先级从高到低）

```
设备 Agent（单个对话 Agent，如「造梦西游6」）
        ↓ 未设置则继承
语义 Agent（角色，如「逆向专家-安卓」/「编码助手」）
        ↓ 未设置则继承
平台默认（统一的一份）
        ↓ 最终兜底
运行时内置文本（unified.txt / QUESTION_POLICY / 编排说明）
```

### 7.2 存储

| 层级 | 位置 | 说明 |
|------|------|------|
| 平台默认 | 新表 `prompt_templates(slot, content, revision, updated_at)` | slot ∈ `base_prompt` / `question_policy` / `orchestration_prompt`；初始化写入当前内置文本，行为不变 |
| 语义 Agent | `semantic_agents.prompt_templates_json` | `{slot: content}`，缺省/空串 = 继承平台 |
| 设备 Agent | `device_settings`（`operator_id` + `agent_id`，key 如 `prompt_template.base_prompt`） | 空 = 继承语义 Agent |

### 7.3 解析与下发

- **解析放在后端**（一处逻辑）：`resolvePromptTemplates(operatorID, deviceAgentID)` 按 设备 Agent → 语义 Agent → 平台默认
  取第一个非空值，返回三份最终文本 + 各级来源 + revision/hash。
- **下发**：launcher 启动 / Agent 重启 / 控制台保存后推送时，后端按设备 Agent 解析并下发；
  launcher 原子写入 `~/.my-opencode-launcher/prompts/<agent_id>/{base,question_policy,orchestration}.md`，
  并在 OPENCODE_CONFIG_CONTENT 声明文件路径。
- **运行时**按会话读取文件；文件不存在则用内置默认（独立 CLI 与未配置账号不受影响）。
- 语义 Agent 的覆盖变更会影响所有绑定它的设备 Agent → 推送时按绑定关系广播。

### 7.4 控制台 UI

- 新页「系统提示词（默认）」：平台默认的三个编辑器 + 恢复默认。
- 「编辑语义 Agent」抽屉新增「系统提示词覆盖」区块：三个编辑器，留空显示"继承平台默认"，并提供"查看当前生效值"。
- 设备 Agent 设置页同样提供三个覆盖项（留空显示"继承语义 Agent：<名称>"）。

### 7.5 生效路径

控制台保存 → 后端解析 → 推送到在线 launcher → 写入文件 → **下一条消息生效**（无需重启 Agent）。
离线设备在下次启动 / Agent 重启时拉取。

### 7.6 与前面方案的关系

- 原「改动①：改 unified.txt 优先级表 + 身份条款」不再需要改代码：把带优先级表和身份条款的文本设为**平台默认**，
  需要不同表现的语义 Agent 再各自覆盖即可。
- 原「改动④：B3 未加载」由本机制直接修掉（改为真实文件读取）。
- B4（环境信息）仍为自动生成；B5 技能块模板暂不开放（技能描述已由技能配置控制）。

---

## 八、"能不能在后台改所有提示词"——全量覆盖矩阵

### 8.1 覆盖矩阵

| 组 | 提示词 | 现状 | 本方案（B1/B2/B3 + 分级覆盖） |
|----|--------|------|-------------------------------|
| **主对话系统提示词** | A1 模型提示词预设 | ✅ 控制台可改 | 不变 |
| | A2 Agent 角色提示词 | ✅ 控制台可改 | 不变（分级覆盖后可再叠加） |
| | A3 全局提示词 / A4 项目级提示词 | ✅ 设备设置可改 | 不变 |
| | A5 技能配置 | ✅ 控制台可改 | 不变 |
| | **B1 基础提示词**（unified.txt） | ❌ 内置 | ✅ 平台默认 + 两级 Agent 覆盖 |
| | **B2 提问策略**（QUESTION_POLICY） | ❌ 内置 | ✅ 同上 |
| | **B3 编排呈现说明**（launcher） | ❌ 内置且未加载 | ✅ 同上（顺带修复加载） |
| | B4 环境信息块 | ❌ 代码生成（含变量） | ⚠️ 未纳入；建议保持自动，或开放"模板+占位符" |
| | B5 技能块模板 | ❌ 内置（列表已由 A5 控制） | ⚠️ 未纳入，可开放模板 |
| **模式提示词** | plan 模式：`plan.txt` / `plan-mode.txt` / `plan-reminder-anthropic.txt` / `build-switch.txt` / `max-steps.txt` | ❌ 内置 | ⚠️ 未纳入，可加槽位 |
| | provider 专属 `anthropic/codex/gpt/kimi/trinity/beast/default/copilot-gpt-5.txt` | 当前 fork 未引用（死文件） | 无需开放 |
| **内部任务提示词**（辅助 LLM 调用，不进主对话） | 标题 `title.txt`、摘要 `summary.txt`、压缩 `compaction.txt`、子代理 `explore.txt` / `scout.txt` | ❌ 内置 | ⚠️ 未纳入 |
| | 项目记忆整理 `PROJECT_MEMORY_SYSTEM_PROMPT`、目标优化 `GOAL_OPTIMIZE_SYSTEM_PROMPT`、目标完成评审 `GOAL_COMPLETION_REVIEW_SYSTEM_PROMPT` | ❌ relay 内置 | ⚠️ 未纳入 |
| | 结构化输出指令、输出长度/Provider 错误续写提示、完成守卫提示、合成附件前缀、verify 意图提示 | ❌ 内置 | ⚠️ 未纳入 |
| **消息级片段** | 任务交付范围文本、系统提醒 / compaction 提醒模板 | ❌ 代码生成（含变量） | ⚠️ 可模板化 |
| **内置 Agent 定义** | 后端 `semantic_agents.go` 默认提示词 + `catalog_v4.json` | ❌ 内置 | 已由 A2 覆盖（控制台改后即生效） |

### 8.2 要做到"全部可改"：提示词注册表（prompt registry）

统一机制：每个提示词注册为一个 **slot**，带以下属性——

| 属性 | 说明 |
|------|------|
| `slot_id` | 稳定标识，如 `chat.base_prompt`、`chat.question_policy`、`plan.mode`、`job.memory_curator` |
| `default_text` | 内置默认（=当前代码里的文本） |
| `variables` | 支持的占位符，如 `{{model}}`、`{{cwd}}`、`{{date}}`、`{{agent_name}}` |
| `scope` | 允许覆盖的层级：平台默认 / 语义 Agent / 设备 Agent |
| `category` | 控制台分组：主对话 / 模式 / 内部任务 / 消息片段 |
| `risk` | 是否与输出解析耦合（见 8.3） |

取值规则沿用第七节：**设备 Agent > 语义 Agent > 平台默认 > 内置默认**；
运行时 / relay / 后端各自按 slot 读取，有覆盖用覆盖、无覆盖用内置默认。

控制台按 `category` 分组展示，每个 slot 一个编辑器（显示默认值、恢复默认、查看生效值、占位符说明）。

### 8.3 风险提示

- **内部任务提示词与解析耦合**：如项目记忆整理、目标评审要求模型输出结构化 JSON，随意改写会导致解析失败、
  记忆/目标功能异常。建议：这类 slot 开放编辑但保留不可编辑的"输出格式约束"段，或提供"仅可追加说明"的受限编辑。
- **消息级片段**含运行期变量（路径、任务号），必须用占位符替换，不能让用户写死。

### 8.4 建议分批

| 批次 | 内容 | 价值 |
|------|------|------|
| 第一批 | B1/B2/B3（平台默认 + 语义 Agent + 设备 Agent 覆盖） | 解决当前"控制台最高级/身份冲突"痛点 |
| 第二批 | 模式提示词（plan / max-steps）+ B4/B5 模板化 | 模式行为可控 |
| 第三批 | 内部任务提示词（标题/摘要/压缩/记忆/目标）+ 消息片段模板 | 真正"全部可改"，需配合受限编辑与校验 |

---

## 九、全量提示词注册表（最终实施规格）

### 9.1 槽位清单（slot catalog）

命名规则 `<area>.<name>`；`scope` 表示允许覆盖的层级（P=平台默认，S=语义 Agent，D=设备 Agent）。

| slot_id | 分类 | 默认来源 | 变量 | scope | 风险 |
|---------|------|----------|------|-------|------|
| `chat.base_prompt` | 主对话 | `session/prompt/unified.txt` | — | P/S/D | 低 |
| `chat.question_policy` | 主对话 | `session/system.ts` QUESTION_POLICY | — | P/S/D | 低 |
| `chat.orchestration_prompt` | 主对话 | launcher `orchestrationPresentationPrompt` | — | P/S/D | 低 |
| `chat.environment` | 主对话 | `SystemPrompt.environment` 模板 | `{{model}}` `{{model_id}}` `{{provider_id}}` `{{cwd}}` `{{worktree}}` `{{is_git}}` `{{platform}}` `{{date}}` | P | 低 |
| `chat.skills_header` | 主对话 | `SystemPrompt.skills` 头部文案 | `{{skills}}` | P | 低 |
| `chat.agent_prompt`（A2） | 主对话 | 语义 Agent 提示词 | — | S | 低（沿用现有编辑页） |
| `chat.model_preset`（A1） | 主对话 | 模型提示词预设 | — | P/S | 低（沿用现有编辑页） |
| `chat.global_prompt` / `chat.project_prompt`（A3/A4） | 主对话 | 设备设置 | — | D | 低（沿用现有编辑页） |
| `mode.plan` | 模式 | `session/prompt/plan.txt` | — | P/S/D | 中 |
| `mode.plan_mode` | 模式 | `plan-mode.txt` | — | P/S/D | 中 |
| `mode.plan_reminder` | 模式 | `plan-reminder-anthropic.txt` | — | P/S/D | 中 |
| `mode.build_switch` | 模式 | `build-switch.txt` | — | P/S/D | 中 |
| `mode.max_steps` | 模式 | `max-steps.txt` | — | P/S/D | 中 |
| `job.title` | 内部任务 | `agent/prompt/title.txt` | — | P | 低 |
| `job.summary` | 内部任务 | `agent/prompt/summary.txt` | — | P | 低 |
| `job.compaction` | 内部任务 | `agent/prompt/compaction.txt` | — | P | 中 |
| `job.explore` / `job.scout` | 内部任务 | `agent/prompt/explore.txt` / `scout.txt` | — | P/S | 中 |
| `job.memory_curator` | 内部任务 | relay `PROJECT_MEMORY_SYSTEM_PROMPT` | `{{schema}}` | P | **高**（JSON 解析耦合） |
| `job.goal_optimize` | 内部任务 | relay `GOAL_OPTIMIZE_SYSTEM_PROMPT` | `{{schema}}` | P | **高** |
| `job.goal_completion_review` | 内部任务 | relay `GOAL_COMPLETION_REVIEW_SYSTEM_PROMPT` | `{{schema}}` | P | **高** |
| `job.structured_output` | 内部任务 | `STRUCTURED_OUTPUT_SYSTEM_PROMPT` | — | P | 中 |
| `job.continue_output_length` / `job.continue_provider_error` | 内部任务 | 续写提示词 | — | P | 中 |
| `job.completion_guard` | 内部任务 | 完成守卫文案 | — | P | **高** |
| `job.verify_intent` | 内部任务 | Android 逆向 Agent 的 verify/frida 意图提示 | — | P/S | 中 |
| `job.synthetic_attachment` | 内部任务 | `SYNTHETIC_ATTACHMENT_PROMPT` | — | P | 低 |
| `message.artifact_scope` | 消息片段 | 后端任务交付范围文案 | `{{task_id}}` `{{dir}}` | P | 低 |
| `message.system_reminder` | 消息片段 | 系统提醒模板 | `{{text}}` | P | 低 |
| `message.compaction_reminder` | 消息片段 | 压缩提醒模板 | `{{...}}` | P | 低 |

> provider 专属 `anthropic/codex/gpt/kimi/trinity/beast/default/copilot-gpt-5.txt` 当前未被引用，不纳入注册表。

### 9.2 存储与解析

| 层级 | 存储 |
|------|------|
| 平台默认 | `prompt_templates(slot_id PK, content MEDIUMTEXT, revision INT, updated_at)` |
| 语义 Agent | `semantic_agents.prompt_templates_json`（`{slot_id: content}`） |
| 设备 Agent | `device_settings`，key = `prompt_template.<slot_id>` |

解析：`resolvePromptTemplates(operatorID, deviceAgentID)` = 设备 Agent → 语义 Agent → 平台默认 → 内置默认，
返回 `{slot_id: {text, source}}` + 汇总 revision。

### 9.3 下发与加载

- launcher 启动 / Agent 重启 / 后端推送时拉取解析结果 → 原子写入
  `~/.my-opencode-launcher/prompts/<agent_id>/<slot_id>.txt`；
- opencode 配置新增 `prompt_slots: { "<slot_id>": "<file path>" }`；
- 运行时/relay/后端按 slot 取值：**有文件用文件（按会话读取，带 mtime 缓存），无文件用内置默认**；
- 变量在取用时渲染（`{{...}}` 占位符）。

### 9.4 控制台

- 新页「提示词管理」：按分类分页签（主对话 / 模式 / 内部任务 / 消息片段）；
  每个 slot 一行：内置默认（只读）、平台值（可编辑）、覆盖统计（哪些语义 Agent / 设备 Agent 覆盖了）、变量说明、风险标记、恢复默认。
- 「编辑语义 Agent」抽屉新增「提示词覆盖」区块：仅列出 `scope` 含 S 的 slot，留空=继承平台。
- 设备 Agent 设置页同样列出 `scope` 含 D 的 slot，留空=继承语义 Agent。
- A1/A2/A3/A4 沿用现有编辑入口，在提示词管理页以只读方式汇总展示，避免两处编辑冲突。

### 9.5 风控

- `risk=高` 的 slot（记忆整理、目标优化/评审、完成守卫）：**输出格式约束段不可编辑**（以 `{{schema}}` 等占位符注入），
  保存时校验必需占位符是否保留，缺失则拒绝并提示；提供一键恢复默认。
- 变量校验：保存时若删除了必需占位符 → 拒绝并说明。
- 所有 slot 保存写审计日志（操作者、slot、revision、时间）。

### 9.6 分批实施

| 批次 | 内容 | 验证 |
|------|------|------|
| 第一批 | 注册表基础设施 + `chat.base_prompt` / `chat.question_policy` / `chat.orchestration_prompt` 端到端 | 控制台改 → 设备拉取 → 问「你是谁」只答人设 |
| 第二批 | `mode.*` + `chat.environment` / `chat.skills_header` | plan 模式与技能块文案可控 |
| 第三批 | `job.*` + `message.*`（含风控校验） | 标题/摘要/记忆等行为可控且解析不破 |

---

## 十、实施进度（滚动更新）

### 第一批：B1/B2/B3 端到端

| 项 | 状态 | 证据 |
|----|------|------|
| 运行时槽位覆盖（`prompt_slots`） | ✅ 已发布 1.15.123 | 单测 6/6、typecheck 通过；线上 `opencode-darwin-arm64.zip` 拆包命中 `prompt_slots` 与三个槽位 ID |
| 后端注册表/存储/解析/接口/推送 | ✅ 已发布 | 单测通过；线上 `prompt_templates` 表、`semantic_agents.prompt_templates_json` 列、两条 admin 路由（401）均核实 |
| launcher 落盘 + 配置声明 | ✅ 已发布 0.1.193 | 单测 5 项通过（落盘/清空/manifest/空 agent_id/无 manifest） |
| 控制台：平台默认编辑页 | ✅ 已上线 | `/codex-admin/prompts.html` 200；服务器文件核实 |
| 控制台：语义 Agent 覆盖编辑 | ✅ 已上线 | `prompts.js` 打包通过；服务器文件命中 `overrideList/loadAgents/prompt_templates` |
| 控制台：设备 Agent 覆盖编辑 | ⏳ 未做 | 后端已支持 `device_settings.prompt_template.<slot>` |
| **设备端升级 + 端到端验证** | ⏳ 待办 | 设备仍为 launcher 0.1.192 / opencode 1.15.122；升级需在 App 点「更新」或手动替换并重启 Agent |

### 待办与依赖

- 端到端验证是第二批的进入门槛（用户要求每批验证后再进入下一批）。
- 设备升级两种方式：**(a)** 用户在 App 内点「更新」；**(b)** 命令行替换 `~/.my-opencode-launcher/bin/` 与 `versions/` 并重启 Agent（会打断当前会话，需用户确认）。
