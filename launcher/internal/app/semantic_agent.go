package app

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"launcher/internal/mcpconfig"
	"launcher/internal/model"
	"launcher/internal/opencodeconfig"
	proc "launcher/internal/process"
)

const defaultSemanticAgentID = "coding-assistant"

const orchestrationPresentationPrompt = `你是面向用户的主 Agent。子代理编排协议仅属于内部上下文：task_id、node_id、plan_id、child_session_id、attempt、内部 state/status、providerID、modelID、工具调用名和调度事件不得主动展示给用户，也不要把它们拼接进普通回复。启动或运行子代理时，只用自然语言说明正在安排相关角色处理；完成后只总结实际结果、证据和下一步。不要输出原始事件 JSON、协议字段、调度日志或内部标识符。只有用户明确询问协作细节时，才解释必要的内部信息。继续主 Agent 工作时不要因为子代理事件而中断或复述内部状态。子代理结果保持 Markdown 语义输出，不添加内部包装标记。`

type semanticAgentSkill struct {
	name         string
	description  string
	content      string
	packageFiles []model.SkillPackageFile
}

type semanticAgentDefinition struct {
	profile      model.SemanticAgentProfile
	opencodeName string
	prompt       string
	permission   map[string]any
	skills       []semanticAgentSkill
}

var legacySemanticAgentDefinitions = []semanticAgentDefinition{
	{
		profile: model.SemanticAgentProfile{
			ID:          defaultSemanticAgentID,
			Name:        "编码助手",
			Description: "默认编码协作 Agent，适合日常开发、调试、重构和测试。",
			Icon:        "code",
			Color:       "primary",
			Enabled:     true,
			SortOrder:   10,
			ToolPermissions: map[string]any{
				"read":     "allow",
				"glob":     "allow",
				"grep":     "allow",
				"list":     "allow",
				"bash":     "ask",
				"edit":     "ask",
				"skill":    "allow",
				"webfetch": "allow",
			},
		},
		opencodeName: "build",
		prompt:       orchestrationPresentationPrompt,
	},
	{
		profile: model.SemanticAgentProfile{
			ID:          "reverse-expert",
			Name:        "逆向专家",
			Description: "面向授权范围内的 Android、二进制、协议、JS 与样本分析。",
			Icon:        "reverse",
			Color:       "primary",
			Skills: []string{
				"reverse-android-analysis",
				"reverse-binary-triage",
				"reverse-js-protocol",
			},
			RecommendedMCPServers: []string{
				"idalib-mcp",
				"ida-pro-mcp",
				"ghidra-mcp",
				"frida-mcp",
				"apktool-mcp",
				"jadx-mcp",
				"jsreverser-mcp",
			},
			Enabled:   true,
			SortOrder: 20,
			ToolPermissions: map[string]any{
				"read":      "allow",
				"glob":      "allow",
				"grep":      "allow",
				"list":      "allow",
				"skill":     "allow",
				"webfetch":  "allow",
				"websearch": "allow",
				"edit":      "ask",
				"bash": map[string]any{
					"*":         "ask",
					"file *":    "allow",
					"strings *": "allow",
					"readelf *": "allow",
					"objdump *": "allow",
					"otool *":   "allow",
					"nm *":      "allow",
					"jadx *":    "ask",
					"apktool *": "ask",
					"frida *":   "ask",
					"adb *":     "ask",
					"ghidra *":  "ask",
					"r2 *":      "ask",
					"radare2 *": "ask",
					"python *":  "ask",
					"python3 *": "ask",
				},
			},
		},
		opencodeName: "reverse-expert",
		prompt: strings.TrimSpace(`
你是逆向专家，专注于合法授权范围内的逆向工程、安全研究、兼容性分析、恶意样本分析、CTF、取证和自有软件调试。

核心边界：
- 只协助用户有权分析的软件、设备、样本、协议或系统。
- 遇到目标授权范围不清、可能涉及第三方系统绕过、凭据窃取、持久化入侵、逃避检测或破坏服务的请求时，先要求澄清或拒绝危险部分，并给出安全替代分析路径。
- 默认优先静态分析，动态调试、插桩、抓包、脱壳和漏洞验证需要明确说明目的、风险和可回滚步骤。

工作方式：
- 先建立证据链：样本/文件、入口点、关键字符串、Manifest/资源、导入导出、调用链、网络端点、签名算法、混淆边界。
- 对 Android 任务，优先使用 JADX、apktool、aapt、adb、Frida、MobSF 思路分析 Manifest、组件、资源、so、证书、权限、反调试、壳和关键调用链。
- 对原生二进制，优先使用 file、strings、readelf、objdump、otool、nm、Ghidra、IDA、radare2 思路分析架构、符号、入口、导入导出、控制流、反调试和加壳迹象。
- Android native Hook 必须先使用 IDA MCP 对目标 SO 完成分析。默认执行全量自动分析；大 SO 应启动独立后台分析进程，允许持续超过 60 分钟且不设固定硬超时，并持续记录进程、样本指纹、IDA 数据库和完成状态。
- Hook 前必须由 IDA 证据确认 APK/SO SHA-256、Build ID、ABI、应用版本、模块名、函数边界、指令集和模块相对偏移。不得根据猜测偏移 Hook，也不得把内联函数中段或指令中段当作入口。
- 确定 Hook 目标前必须探测热更新框架、动态下发 SO/DEX/脚本和运行时替换代码；热更新状态未知时，Hook 结论保持待验证。
- ShadowHook 注册或返回 PENDING 只表示安装状态，不表示功能已生效。必须从实际 UI 操作触发完整调用链，并用可观察日志、计数器或 trace 同时证明 UI handler、Java/native bridge、目标 native Hook callback 和原函数调用状态；空 UI 实现或从未执行的 native callback 不得标记成功。
- 对 JS/协议逆向，优先定位请求构造、签名链路、关键常量、状态机、混淆函数、运行时依赖和可复现实验。
- 尽量给出可复现命令、观察结果、推理依据、限制条件和下一步验证点。

输出结构：
- 结论：先给当前最可信判断。
- 证据：列出文件、函数、字符串、命令输出或路径依据。
- 推理：区分事实、假设和待验证点。
- 操作：给出最小可复现步骤。
- 风险：说明动态分析、写入、联网、越权或破坏性风险。
- 下一步：给出 1 到 3 个最有价值的后续动作。
`),
		permission: map[string]any{
			"read":      "allow",
			"glob":      "allow",
			"grep":      "allow",
			"list":      "allow",
			"skill":     "allow",
			"webfetch":  "allow",
			"websearch": "allow",
			"edit":      "ask",
			"bash": map[string]any{
				"*":         "ask",
				"file *":    "allow",
				"strings *": "allow",
				"readelf *": "allow",
				"objdump *": "allow",
				"otool *":   "allow",
				"nm *":      "allow",
				"jadx *":    "ask",
				"apktool *": "ask",
				"frida *":   "ask",
				"adb *":     "ask",
				"ghidra *":  "ask",
				"r2 *":      "ask",
				"radare2 *": "ask",
				"python *":  "ask",
				"python3 *": "ask",
			},
		},
		skills: []semanticAgentSkill{
			{
				name:        "reverse-android-analysis",
				description: "Android APK/DEX/SO 逆向分析流程，适合 Manifest、JADX、apktool、Frida 和调用链定位。",
				content: strings.TrimSpace(`
---
name: reverse-android-analysis
description: Android APK/DEX/SO 逆向分析流程，适合 Manifest、JADX、apktool、Frida 和调用链定位。
---

# Android 逆向分析

使用场景：
- 用户需要分析 APK、DEX、AAB、Android so、权限、组件、资源、证书、壳、反调试或签名逻辑。
- 用户需要定位登录、授权、加密、签名、网络请求、热更新、风控或校验链路。

流程：
1. 确认授权范围、样本来源、目标版本和分析目的。
2. 先静态观察 Manifest、证书、权限、组件、资源、assets、lib ABI、字符串和网络端点。
3. 使用 JADX 定位 Java/Kotlin 调用链，使用 apktool 对资源、smali 和 Manifest 做交叉验证。
4. 只要任务将安装 native Hook，就必须先调用 IDA MCP 分析目标 SO。执行全量自动分析；大 SO 使用独立后台进程，允许运行超过 60 分钟且不设固定硬超时，记录进程、SO 指纹、IDA 数据库和分析状态。
5. 用 IDA 结果确认目标函数边界、指令集和模块相对偏移，并绑定 APK/SO SHA-256、Build ID、ABI、应用版本和模块名。缺少这些证据时不得写入或安装 Hook，避免误 Hook 内联函数或错误地址。
6. 在确定目标前探测热更新框架、动态下载的 SO/DEX/脚本以及运行时替换代码，并记录当前版本实际加载的模块指纹。
7. 安装后必须执行端到端验证：实际操作 UI，观察 UI handler、bridge、ShadowHook 初始化/安装状态、native callback 命中和原函数调用。以日志、trace 或单调计数器作为证据；空 UI 实现、仅返回 PENDING、或 native callback 未命中都不是成功。
8. 输出证据、假设、复现步骤、风险和下一步。

注意：
- 不协助绕过未授权系统、规避付费授权、窃取账号凭据或破坏第三方服务。
- 不能把猜测写成结论；缺少样本或输出时要明确说明待验证。
`),
			},
			{
				name:        "reverse-binary-triage",
				description: "原生二进制初筛与静态分析流程，适合 ELF/Mach-O/PE、so、符号、导入导出和控制流判断。",
				content: strings.TrimSpace(`
---
name: reverse-binary-triage
description: 原生二进制初筛与静态分析流程，适合 ELF/Mach-O/PE、so、符号、导入导出和控制流判断。
---

# 二进制初筛

使用场景：
- 用户需要分析 ELF、Mach-O、PE、Android so、iOS dylib、驱动、固件片段或可疑样本。
- 用户需要判断架构、入口点、导入导出、符号、壳、混淆、反调试或关键算法。

流程：
1. 明确授权和目标平台，记录样本哈希、文件大小、架构和时间戳。
2. 先运行只读命令，例如 file、strings、readelf、objdump、otool、nm。
3. 建立导入导出、字符串、节区、符号、入口点、初始化函数和可疑 API 的证据表。
4. 使用 Ghidra、IDA 或 radare2 时，先定位入口、JNI_OnLoad、RegisterNatives、导出函数和交叉引用。
5. 对动态调试、patch、注入、dump、脱壳操作先说明风险和可回滚步骤。
6. 输出结论、证据、推理、操作、风险和下一步。

注意：
- 不协助制作持久化、免杀、隐蔽控制、凭据窃取或破坏性能力。
- 面对恶意样本时，优先在隔离环境中分析，不建议直接执行。
`),
			},
			{
				name:        "reverse-js-protocol",
				description: "JS 与协议逆向分析流程，适合签名参数、请求构造、混淆函数、状态机和补环境。",
				content: strings.TrimSpace(`
---
name: reverse-js-protocol
description: JS 与协议逆向分析流程，适合签名参数、请求构造、混淆函数、状态机和补环境。
---

# JS 与协议逆向

使用场景：
- 用户需要分析前端混淆、接口签名、加密参数、请求重放、协议状态机或本地补环境。
- 用户需要从页面、脚本、网络请求和运行时调用中还原数据流。

流程：
1. 明确授权范围和目标接口，避免未授权抓取、绕过风控或破坏第三方服务。
2. 先观察请求：URL、method、headers、cookies、body、时间戳、nonce、签名字段和响应错误。
3. 定位 JS 入口：事件触发、请求封装、拦截器、签名函数、加密库、常量表和混淆边界。
4. 分离事实和假设，逐步补齐运行时依赖，保持最小复现实验。
5. 对 AST 去混淆、插桩、代理抓包和动态断点说明目的、风险和可回滚方式。
6. 输出关键函数、参数来源、复现步骤、限制条件和下一步。

注意：
- 不协助批量滥用、绕过访问控制、盗取 Cookie/token 或规避平台风控。
- 复现脚本应服务于授权分析，不应包含攻击性自动化。
`),
			},
		},
	},
}

func semanticAgentProfiles() []model.SemanticAgentProfile {
	out := make([]model.SemanticAgentProfile, 0, len(semanticAgentDefinitions))
	for _, item := range semanticAgentDefinitions {
		if !item.profile.Enabled {
			continue
		}
		out = append(out, semanticAgentProfile(item))
	}
	return out
}

func semanticAgentProfile(item semanticAgentDefinition) model.SemanticAgentProfile {
	profile := item.profile
	profile.OpencodeName = item.opencodeName
	profile.Prompt = item.prompt
	profile.SkillIDs = append([]string(nil), item.profile.SkillIDs...)
	profile.MCPIDs = append([]string(nil), item.profile.MCPIDs...)
	profile.Skills = append([]string(nil), item.profile.Skills...)
	if len(profile.Skills) == 0 && len(item.skills) > 0 {
		for _, skill := range item.skills {
			profile.Skills = append(profile.Skills, skill.name)
		}
	}
	profile.SkillDefinitions = append([]model.SemanticAgentSkill(nil), item.profile.SkillDefinitions...)
	if len(profile.SkillDefinitions) == 0 {
		profile.SkillDefinitions = semanticAgentSkillDefinitions(item.skills)
	}
	profile.RecommendedMCPServers = append([]string(nil), item.profile.RecommendedMCPServers...)
	profile.RecommendedMCPConfigs = append([]model.DeviceMCPServerInfo(nil), item.profile.RecommendedMCPConfigs...)
	profile.MCPDependencies = append([]model.SemanticAgentMCPDependency(nil), item.profile.MCPDependencies...)
	profile.ToolPermissions = cloneAnyMap(item.profile.ToolPermissions)
	if len(profile.ToolPermissions) == 0 {
		profile.ToolPermissions = cloneAnyMap(item.permission)
	}
	return profile
}

func semanticAgentSkillDefinitions(skills []semanticAgentSkill) []model.SemanticAgentSkill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]model.SemanticAgentSkill, 0, len(skills))
	for _, skill := range skills {
		out = append(out, model.SemanticAgentSkill{
			Name:         skill.name,
			Description:  skill.description,
			Content:      skill.content,
			PackageFiles: cloneSkillPackageFiles(skill.packageFiles),
		})
	}
	return out
}

func semanticAgentSkillsFromModel(skills []model.SemanticAgentSkill) []semanticAgentSkill {
	out := make([]semanticAgentSkill, 0, len(skills))
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		out = append(out, semanticAgentSkill{
			name:         name,
			description:  strings.TrimSpace(skill.Description),
			content:      strings.TrimSpace(skill.Content),
			packageFiles: cleanSkillPackageFiles(skill.PackageFiles),
		})
	}
	return out
}

func mergeSemanticAgentSkills(defaults []semanticAgentSkill, extras []model.SemanticAgentSkill) []semanticAgentSkill {
	out := append([]semanticAgentSkill(nil), defaults...)
	seen := make(map[string]struct{}, len(out))
	for _, skill := range out {
		seen[strings.TrimSpace(skill.name)] = struct{}{}
	}
	for _, skill := range semanticAgentSkillsFromModel(extras) {
		if _, exists := seen[skill.name]; exists {
			continue
		}
		seen[skill.name] = struct{}{}
		out = append(out, skill)
	}
	return out
}

func semanticAgentByID(id string) (semanticAgentDefinition, bool) {
	id = normalizeSemanticAgentID(id)
	for _, item := range semanticAgentDefinitions {
		if item.profile.ID == id {
			return item, true
		}
	}
	for _, item := range legacySemanticAgentDefinitions {
		if item.profile.ID == id {
			return item, true
		}
	}
	return semanticAgentDefinition{}, false
}

func normalizeSemanticAgentID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return defaultSemanticAgentID
	}
	return id
}

func validateSemanticAgentID(id string) (semanticAgentDefinition, error) {
	id = normalizeSemanticAgentID(id)
	item, ok := semanticAgentByID(id)
	if !ok || !item.profile.Enabled {
		return semanticAgentDefinition{}, errors.New("语义 Agent 不存在")
	}
	return item, nil
}

func semanticAgentDefinitionFromProfile(profile model.SemanticAgentProfile) (semanticAgentDefinition, error) {
	profile.ID = normalizeSemanticAgentID(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.OpencodeName = strings.TrimSpace(profile.OpencodeName)
	profile.Prompt = strings.TrimSpace(profile.Prompt)
	if profile.Name == "" {
		return semanticAgentDefinition{}, errors.New("语义 Agent 名称不能为空")
	}
	if profile.OpencodeName == "" {
		profile.OpencodeName = profile.ID
	}
	if len(profile.Skills) == 0 && len(profile.SkillDefinitions) > 0 {
		for _, skill := range profile.SkillDefinitions {
			if strings.TrimSpace(skill.Name) != "" {
				profile.Skills = append(profile.Skills, strings.TrimSpace(skill.Name))
			}
		}
	}
	skills := make([]semanticAgentSkill, 0, len(profile.SkillDefinitions))
	for _, skill := range profile.SkillDefinitions {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		skills = append(skills, semanticAgentSkill{
			name:         name,
			description:  strings.TrimSpace(skill.Description),
			content:      strings.TrimSpace(skill.Content),
			packageFiles: cleanSkillPackageFiles(skill.PackageFiles),
		})
	}
	return semanticAgentDefinition{
		profile:      profile,
		opencodeName: profile.OpencodeName,
		prompt:       profile.Prompt,
		permission:   cloneAnyMap(profile.ToolPermissions),
		skills:       skills,
	}, nil
}

func semanticAgentDefinitionFromConfig(cfg model.AgentConfig) (semanticAgentDefinition, bool) {
	if cfg.SemanticAgentProfile != nil && strings.TrimSpace(cfg.SemanticAgentProfile.ID) != "" {
		item, err := semanticAgentDefinitionFromProfile(*cfg.SemanticAgentProfile)
		if err == nil && item.profile.ID == normalizeSemanticAgentID(cfg.SemanticAgentID) {
			return item, true
		}
	}
	return semanticAgentByID(cfg.SemanticAgentID)
}

func semanticAgentSelectionFromConfig(cfg model.AgentConfig) model.DeviceAgentSemanticSelectionInfo {
	item, ok := semanticAgentDefinitionFromConfig(cfg)
	if !ok {
		item, _ = semanticAgentByID(defaultSemanticAgentID)
	}
	profile := semanticAgentProfile(item)
	return model.DeviceAgentSemanticSelectionInfo{
		AgentID:          cfg.AgentID,
		SemanticAgentID:  profile.ID,
		VerifyMCPEnabled: !cfg.DisableVerifyMCP,
		Profile:          &profile,
		AvailableAgents:  semanticAgentProfiles(),
	}
}

func applySemanticAgentFieldsFromConfig(agent *model.Agent, cfg model.AgentConfig) {
	if agent == nil {
		return
	}
	item, ok := semanticAgentDefinitionFromConfig(cfg)
	if !ok {
		item, _ = semanticAgentByID(defaultSemanticAgentID)
	}
	agent.SemanticAgentID = item.profile.ID
	agent.SemanticAgentName = item.profile.Name
}

func applySemanticAgentFields(agent *model.Agent, semanticAgentID string) {
	if agent == nil {
		return
	}
	item, ok := semanticAgentByID(semanticAgentID)
	if !ok {
		item, _ = semanticAgentByID(defaultSemanticAgentID)
	}
	agent.SemanticAgentID = item.profile.ID
	agent.SemanticAgentName = item.profile.Name
}

func (s *service) SemanticAgents() []model.SemanticAgentProfile {
	return semanticAgentProfiles()
}

func (s *service) AgentSemanticSelection(agentID string) (model.DeviceAgentSemanticSelectionInfo, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return model.DeviceAgentSemanticSelectionInfo{}, proc.ErrAgentNotFound
	}
	if !s.hasAgent(agentID) {
		return model.DeviceAgentSemanticSelectionInfo{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentSemanticSelectionInfo{}, err
	}
	return semanticAgentSelectionFromConfig(*cfg), nil
}

func (s *service) SaveAgentSemanticSelection(input model.DeviceAgentSemanticSelectionInput) (model.DeviceAgentSemanticSelectionInfo, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return model.DeviceAgentSemanticSelectionInfo{}, proc.ErrAgentNotFound
	}
	item, err := validateSemanticAgentID(input.SemanticAgentID)
	if input.SemanticAgent != nil {
		item, err = semanticAgentDefinitionFromProfile(*input.SemanticAgent)
		if err == nil && item.profile.ID != normalizeSemanticAgentID(input.SemanticAgentID) {
			err = errors.New("语义 Agent 配置与选择 ID 不一致")
		}
	}
	if err != nil {
		return model.DeviceAgentSemanticSelectionInfo{}, err
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.DeviceAgentSemanticSelectionInfo{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentSemanticSelectionInfo{}, err
	}
	cfg.SemanticAgentID = item.profile.ID
	profile := semanticAgentProfile(item)
	cfg.SemanticAgentProfile = &profile
	cfg.DisableVerifyMCP = input.DisableVerifyMCP
	if input.ApplyRecommendedMCP && (len(profile.RecommendedMCPConfigs) > 0 || len(profile.RecommendedMCPServers) > 0) {
		mcpProfile := profile
		if input.DisableVerifyMCP {
			mcpProfile = withoutVerifyMCPProfile(profile)
		}
		prevMode := cfg.MCPMode
		prevServers := append([]string(nil), cfg.MCPServers...)
		cfg.MCPMode = "custom"
		selectedServers := []string{}
		info, loadErr := mcpconfig.Load()
		if loadErr == nil {
			selectedServers = selectedAvailableMCPServers(mcpProfile.RecommendedMCPServers, info.Servers)
		}
		if len(mcpProfile.RecommendedMCPConfigs) > 0 {
			resolved, resolveErr := resolveSemanticRecommendedMCPConfigs(s.cfg.RuntimeDir, mcpProfile.RecommendedMCPConfigs)
			if resolveErr != nil {
				return model.DeviceAgentSemanticSelectionInfo{}, resolveErr
			}
			cfg.MCPServerConfigs = resolved
			selectedServers = append(selectedServers, mcpServerConfigNames(cfg.MCPServerConfigs)...)
			cfg.MCPServers = normalizeMCPServerNames(selectedServers)
		} else if loadErr == nil {
			cfg.MCPServerConfigs = nil
			cfg.MCPServers = normalizeMCPServerNames(selectedServers)
		} else {
			log.Printf("[launcher] failed to apply semantic mcp recommendations agent_id=%s err=%v", agentID, loadErr)
		}
		log.Printf("[launcher][mcp] SaveAgentSemanticSelection agent_id=%s semantic=%s MCPMode %s->custom MCPServers %v->%v (注意:custom模式仅影响运行时启用,不删除opencode.json中的server)",
			agentID, item.profile.ID, prevMode, prevServers, cfg.MCPServers)
	}
	if input.DisableVerifyMCP {
		cfg.MCPMode = "custom"
		cfg.MCPServers = withoutVerifyMCPNames(cfg.MCPServers)
		cfg.MCPServerConfigs = withoutVerifyMCPServerConfigs(cfg.MCPServerConfigs)
		log.Printf("[launcher][mcp] Verify MCP disabled for agent_id=%s", agentID)
	}
	cfg.Env = s.agentEnv(*cfg)
	if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
		return model.DeviceAgentSemanticSelectionInfo{}, err
	}
	if err := s.updateAgentSemanticSelection(agentID, semanticAgentProfile(item)); err != nil {
		return model.DeviceAgentSemanticSelectionInfo{}, err
	}
	return semanticAgentSelectionFromConfig(*cfg), nil
}

func withoutVerifyMCPProfile(profile model.SemanticAgentProfile) model.SemanticAgentProfile {
	filteredIDs := make([]string, 0, len(profile.MCPIDs))
	filteredServers := make([]string, 0, len(profile.RecommendedMCPServers))
	for index, id := range profile.MCPIDs {
		name := id
		if index < len(profile.RecommendedMCPServers) {
			name = profile.RecommendedMCPServers[index]
		}
		if isVerifyMCPName(id) || isVerifyMCPName(name) {
			continue
		}
		filteredIDs = append(filteredIDs, id)
		if index < len(profile.RecommendedMCPServers) {
			filteredServers = append(filteredServers, profile.RecommendedMCPServers[index])
		}
	}
	for index := len(profile.MCPIDs); index < len(profile.RecommendedMCPServers); index++ {
		name := profile.RecommendedMCPServers[index]
		if !isVerifyMCPName(name) {
			filteredServers = append(filteredServers, name)
		}
	}
	profile.MCPIDs = filteredIDs
	profile.RecommendedMCPServers = filteredServers
	profile.RecommendedMCPConfigs = withoutVerifyMCPServerConfigs(profile.RecommendedMCPConfigs)
	filteredDependencies := make([]model.SemanticAgentMCPDependency, 0, len(profile.MCPDependencies))
	for _, dependency := range profile.MCPDependencies {
		if isVerifyMCPName(dependency.ID) || isVerifyMCPName(dependency.Name) ||
			isVerifyMCPName(dependency.Title) || isVerifyMCPName(dependency.Config.Name) {
			continue
		}
		filteredDependencies = append(filteredDependencies, dependency)
	}
	profile.MCPDependencies = filteredDependencies
	return profile
}

func withoutVerifyMCPNames(input []string) []string {
	out := make([]string, 0, len(input))
	for _, name := range input {
		if !isVerifyMCPName(name) {
			out = append(out, name)
		}
	}
	return normalizeMCPServerNames(out)
}

func withoutVerifyMCPServerConfigs(input []model.DeviceMCPServerInfo) []model.DeviceMCPServerInfo {
	out := make([]model.DeviceMCPServerInfo, 0, len(input))
	for _, server := range input {
		if !isVerifyMCPName(server.Name) {
			out = append(out, server)
		}
	}
	return out
}

func isVerifyMCPName(value string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), "verify")
}

func resolveSemanticRecommendedMCPConfigs(runtimeDir string, input []model.DeviceMCPServerInfo) ([]model.DeviceMCPServerInfo, error) {
	normalized := semanticRecommendedMCPConfigs(input)
	out := make([]model.DeviceMCPServerInfo, 0, len(normalized))
	for _, server := range normalized {
		resolved, err := resolveMCPServerForRuntime(runtimeDir, server)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

func semanticRecommendedMCPConfigs(input []model.DeviceMCPServerInfo) []model.DeviceMCPServerInfo {
	out := make([]model.DeviceMCPServerInfo, 0, len(input))
	seen := map[string]bool{}
	for _, server := range input {
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" || seen[server.Name] {
			continue
		}
		seen[server.Name] = true
		if strings.TrimSpace(server.Type) == "" {
			server.Type = "local"
		}
		server.Enabled = true
		out = append(out, server)
	}
	return out
}

func mcpServerConfigNames(input []model.DeviceMCPServerInfo) []string {
	out := make([]string, 0, len(input))
	for _, server := range input {
		if strings.TrimSpace(server.Name) == "" {
			continue
		}
		out = append(out, server.Name)
	}
	return normalizeMCPServerNames(out)
}

func (s *service) updateAgentSemanticSelection(agentID string, profile model.SemanticAgentProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return proc.ErrAgentNotFound
	}
	s.agents[index].SemanticAgentID = profile.ID
	s.agents[index].SemanticAgentName = profile.Name
	return saveAgents(s.cfg.RuntimeDir, s.agents)
}

func (s *service) agentSemanticConfigContent(cfg model.AgentConfig, existing string) (string, bool) {
	if strings.TrimSpace(cfg.SemanticAgentID) == "" {
		return "", false
	}
	item, ok := semanticAgentDefinitionFromConfig(cfg)
	if !ok {
		log.Printf("[launcher] unknown semantic agent agent_id=%s semantic_agent_id=%s", cfg.AgentID, cfg.SemanticAgentID)
		return "", false
	}
	item.skills = mergeSemanticAgentSkills(item.skills, cfg.ExtraSkillDefinitions)
	content := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := opencodeconfig.Decode([]byte(existing), &content); err != nil {
			log.Printf("[launcher] failed to decode existing semantic OPENCODE_CONFIG_CONTENT agent_id=%s err=%v", cfg.AgentID, err)
			content = map[string]any{}
		}
	}
	content["default_agent"] = item.opencodeName
	content["instructions"] = appendOrchestrationInstruction(content["instructions"])
	content["skills"] = map[string]any{"paths": []string{}, "urls": []string{}}
	if skillRoot, ok := s.prepareSemanticAgentSkills(item); ok && skillRoot != "" {
		content["skills"] = map[string]any{"paths": []string{skillRoot}, "urls": []string{}}
	}
	if strings.TrimSpace(item.prompt) != "" {
		agentMap, _ := content["agent"].(map[string]any)
		if agentMap == nil {
			agentMap = map[string]any{}
		}
		entry := map[string]any{
			"mode":        "primary",
			"description": item.profile.Description,
			"prompt":      item.prompt,
			"color":       item.profile.Color,
		}
		if len(item.permission) > 0 {
			entry["permission"] = item.permission
		}
		agentMap[item.opencodeName] = entry
		content["agent"] = agentMap
	}
	data, err := json.Marshal(content)
	if err != nil {
		log.Printf("[launcher] failed to encode semantic agent override agent_id=%s err=%v", cfg.AgentID, err)
		return "", false
	}
	return string(data), true
}

func appendOrchestrationInstruction(raw any) []string {
	result := make([]string, 0, 2)
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	case []string:
		for _, item := range value {
			if strings.TrimSpace(item) != "" {
				result = append(result, strings.TrimSpace(item))
			}
		}
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
	}
	for _, item := range result {
		if item == orchestrationPresentationPrompt {
			return result
		}
	}
	return append(result, orchestrationPresentationPrompt)
}

func (s *service) prepareSemanticAgentSkills(item semanticAgentDefinition) (string, bool) {
	if len(item.skills) == 0 {
		return "", true
	}
	root := filepath.Join(s.cfg.RuntimeDir, "semantic-agents", item.profile.ID, "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Printf("[launcher] failed to create semantic skill root agent=%s err=%v", item.profile.ID, err)
		return "", false
	}
	for _, skill := range item.skills {
		dir := filepath.Join(root, skill.name)
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[launcher] failed to reset semantic skill dir agent=%s skill=%s err=%v", item.profile.ID, skill.name, err)
			return "", false
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("[launcher] failed to create semantic skill dir agent=%s skill=%s err=%v", item.profile.ID, skill.name, err)
			return "", false
		}
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte(skill.content+"\n"), 0o644); err != nil {
			log.Printf("[launcher] failed to write semantic skill agent=%s skill=%s err=%v", item.profile.ID, skill.name, err)
			return "", false
		}
		for _, file := range skill.packageFiles {
			relPath := cleanSkillPackagePath(file.Path)
			if relPath == "" {
				continue
			}
			target := filepath.Join(dir, filepath.FromSlash(relPath))
			if !strings.HasPrefix(target, dir+string(os.PathSeparator)) {
				log.Printf("[launcher] skipped unsafe semantic skill file agent=%s skill=%s path=%s", item.profile.ID, skill.name, file.Path)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				log.Printf("[launcher] failed to create semantic skill package dir agent=%s skill=%s path=%s err=%v", item.profile.ID, skill.name, relPath, err)
				return "", false
			}
			mode := os.FileMode(0o644)
			if file.Executable {
				mode = 0o755
			}
			if err := os.WriteFile(target, []byte(file.Content), mode); err != nil {
				log.Printf("[launcher] failed to write semantic skill package file agent=%s skill=%s path=%s err=%v", item.profile.ID, skill.name, relPath, err)
				return "", false
			}
		}
	}
	return root, true
}

func cleanSkillPackageFiles(input []model.SkillPackageFile) []model.SkillPackageFile {
	if input == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]model.SkillPackageFile, 0, len(input))
	for _, item := range input {
		item.Path = cleanSkillPackagePath(item.Path)
		if item.Path == "" || seen[item.Path] {
			continue
		}
		seen[item.Path] = true
		out = append(out, item)
	}
	return out
}

func cleanSkillPackagePath(input string) string {
	value := strings.TrimSpace(strings.ReplaceAll(input, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\x00") {
		return ""
	}
	parts := strings.Split(value, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return ""
		}
		cleaned = append(cleaned, part)
	}
	value = strings.Join(cleaned, "/")
	if strings.EqualFold(value, "SKILL.md") {
		return ""
	}
	return value
}

func cloneSkillPackageFiles(input []model.SkillPackageFile) []model.SkillPackageFile {
	if input == nil {
		return nil
	}
	out := make([]model.SkillPackageFile, len(input))
	copy(out, input)
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneAnyMap(nested)
		} else {
			out[key] = value
		}
	}
	return out
}
