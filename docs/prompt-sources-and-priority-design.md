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
