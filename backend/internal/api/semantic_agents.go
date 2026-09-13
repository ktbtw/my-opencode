package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

const defaultSemanticAgentID = "coding-assistant"

func defaultSkills() []model.Skill {
	return builtinSkills()
}

func defaultMCPCatalogItems() []model.MCPCatalogItem {
	return builtinMCPCatalogItems()
}

var legacyReverseMCPCatalogIDs = map[string]bool{
	"idalib-mcp":     true,
	"ida-pro-mcp":    true,
	"ghidra-mcp":     true,
	"frida-mcp":      true,
	"apktool-mcp":    true,
	"jadx-mcp":       true,
	"jsreverser-mcp": true,
}

func isLegacyReverseMCPCatalogItem(item model.MCPCatalogItem) bool {
	if legacyReverseMCPCatalogIDs[item.ID] && item.Source == "内置逆向专家" {
		return true
	}
	return item.BuiltIn && item.Source == "内置逆向专家" && legacyReverseMCPCatalogIDs[item.Name]
}

func defaultSemanticAgentProfiles() []model.SemanticAgentProfile {
	return builtinSemanticAgentProfiles()
}

func legacyDefaultSemanticAgentProfiles() []model.SemanticAgentProfile {
	return []model.SemanticAgentProfile{
		{
			ID:           defaultSemanticAgentID,
			Name:         "编码助手",
			Description:  "默认编码协作 Agent，适合日常开发、调试、重构和测试。",
			Icon:         "code",
			Color:        "primary",
			OpencodeName: "build",
			Enabled:      true,
			BuiltIn:      true,
			SortOrder:    10,
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
			RuntimeRequirements: []model.SemanticAgentRuntime{
				{RuntimeID: "node", VersionConstraint: ">=22 <27", Required: true, Purpose: "运行 Node MCP 和前端工具", SortOrder: 10},
				{RuntimeID: "uv", VersionConstraint: "latest", Required: true, Purpose: "安装 Python 工具和解释器", SortOrder: 20},
			},
		},
		{
			ID:           "reverse-expert",
			Name:         "逆向专家",
			Description:  "面向授权范围内的 Android、二进制、协议、JS 与样本分析。",
			Icon:         "reverse",
			Color:        "primary",
			OpencodeName: "reverse-expert",
			Prompt: strings.TrimSpace(`
你是逆向专家，专注于合法授权范围内的逆向工程、安全研究、兼容性分析、恶意样本分析、CTF、取证和自有软件调试。

核心边界：
- 只协助用户有权分析的软件、设备、样本、协议或系统。
- 遇到目标授权范围不清、可能涉及第三方系统绕过、凭据窃取、持久化入侵、逃避检测或破坏服务的请求时，先要求澄清或拒绝危险部分，并给出安全替代分析路径。
- 默认优先静态分析，动态调试、插桩、抓包、脱壳和漏洞验证需要明确说明目的、风险和可回滚步骤。

工作方式：
- 先建立证据链：样本/文件、入口点、关键字符串、Manifest/资源、导入导出、调用链、网络端点、签名算法、混淆边界。
- 对 Android 任务，优先使用 JADX、apktool、aapt、adb、Frida、MobSF 思路分析 Manifest、组件、资源、so、证书、权限、反调试、壳和关键调用链。
- 对原生二进制，优先使用 file、strings、readelf、objdump、otool、nm、Ghidra、IDA、radare2 思路分析架构、符号、入口、导入导出、控制流、反调试和加壳迹象。
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
			RuntimeRequirements: []model.SemanticAgentRuntime{
				{RuntimeID: "java", VersionConstraint: ">=17 <22", Required: true, Purpose: "运行 jadx、apktool 和 Java 逆向工具", SortOrder: 10},
				{RuntimeID: "python", VersionConstraint: ">=3.12 <3.15", Required: true, Purpose: "运行 Python MCP、脚本和逆向辅助工具", SortOrder: 20},
				{RuntimeID: "node", VersionConstraint: ">=22 <27", Required: true, Purpose: "运行 JS/协议分析和 Node MCP", SortOrder: 30},
				{RuntimeID: "uv", VersionConstraint: "latest", Required: true, Purpose: "管理 Python 工具和解释器", SortOrder: 40},
			},
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 20,
		},
	}
}

func (a *API) ListSemanticAgents(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	items, err := a.semanticAgentProfiles(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) UpdateSemanticAgent(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent id 不能为空"})
		return
	}
	var req model.SemanticAgentProfile
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.ID = id
	stored, err := a.upsertSemanticAgent(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, stored)
}

func (a *API) SetSemanticAgentEnabled(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent id 不能为空"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	profile, err := a.semanticAgentByID(id, true)
	if err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	profile.Enabled = req.Enabled
	stored, err := a.upsertSemanticAgent(profile)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, stored)
}

func (a *API) DeleteSemanticAgent(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "语义 Agent ID 不能为空"})
		return
	}
	if err := a.store.DeleteSemanticAgent(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) ListSkills(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := a.ensureDefaultSemanticAgentAssets(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListSkills(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for index := range items {
		items[index].Description = normalizeAssetDescription(items[index].Description, items[index].Content)
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) ListPublicSkills(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentOperator(r); !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := a.ensureDefaultSemanticAgentAssets(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListSkills(false)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for index := range items {
		items[index].Description = normalizeAssetDescription(items[index].Description, items[index].Content)
		items[index].Content = strings.TrimSpace(items[index].Content)
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) UpsertSkill(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.Skill
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertSkill(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "skill id 不能为空"})
		return
	}
	if err := a.store.DeleteSkill(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) ListAdminMCPCatalog(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := a.ensureDefaultSemanticAgentAssets(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListMCPCatalog(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items = filterLegacyReverseMCPCatalogItems(items)
	for index := range items {
		items[index].Description = normalizeAssetDescription(items[index].Description, "")
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) UpsertMCPCatalogItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.MCPCatalogItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertMCPCatalogItem(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteMCPCatalogItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "mcp id 不能为空"})
		return
	}
	if err := a.store.DeleteMCPCatalogItem(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) semanticAgentProfiles(includeDisabled bool) ([]model.SemanticAgentProfile, error) {
	if err := a.ensureDefaultSemanticAgents(); err != nil {
		return nil, err
	}
	items, err := a.store.ListSemanticAgents(includeDisabled)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if err := a.expandSemanticAgentReferences(&items[index]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (a *API) semanticAgentByID(id string, includeDisabled bool) (model.SemanticAgentProfile, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = defaultSemanticAgentID
	}
	items, err := a.semanticAgentProfiles(includeDisabled)
	if err != nil {
		return model.SemanticAgentProfile{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return model.SemanticAgentProfile{}, errors.New("语义 Agent 不存在")
}

func (a *API) ensureDefaultSemanticAgents() error {
	if err := a.ensureDefaultSemanticAgentAssets(); err != nil {
		return err
	}
	initialized, err := a.defaultSeedInitialized(defaultSeedSemanticAgentsKey)
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	items, err := a.store.ListSemanticAgents(true)
	if err != nil {
		return err
	}
	exists := map[string]model.SemanticAgentProfile{}
	for _, item := range items {
		exists[item.ID] = item
	}
	for _, item := range defaultSemanticAgentProfiles() {
		current, ok := exists[item.ID]
		if ok && current.BuiltIn {
			item = mergeBuiltinSemanticAgentReferences(current, item)
		}
		if ok && !current.BuiltIn {
			current.BuiltIn = true
			item = current
		}
		if _, err := a.upsertSemanticAgent(item); err != nil {
			return err
		}
	}
	return a.markDefaultSeedInitialized(defaultSeedSemanticAgentsKey)
}

func mergeBuiltinSemanticAgentReferences(current model.SemanticAgentProfile, builtin model.SemanticAgentProfile) model.SemanticAgentProfile {
	current.BuiltIn = true
	current.SkillIDs = cleanStringSlice(append(current.SkillIDs, builtin.SkillIDs...))
	current.MCPIDs = cleanStringSlice(append(current.MCPIDs, builtin.MCPIDs...))
	knownRuntimes := make(map[string]bool, len(current.RuntimeRequirements))
	for _, requirement := range current.RuntimeRequirements {
		knownRuntimes[strings.TrimSpace(requirement.RuntimeID)] = true
	}
	for _, requirement := range builtin.RuntimeRequirements {
		if knownRuntimes[strings.TrimSpace(requirement.RuntimeID)] {
			continue
		}
		current.RuntimeRequirements = append(current.RuntimeRequirements, requirement)
	}
	return current
}

func (a *API) ensureDefaultSemanticAgentAssets() error {
	if err := a.ensureDefaultRuntimeCatalog(); err != nil {
		return err
	}
	skillsInitialized, err := a.defaultSeedInitialized(defaultSeedSkillsKey)
	if err != nil {
		return err
	}
	skills, err := a.store.ListSkills(true)
	if err != nil {
		return err
	}
	if !skillsInitialized {
		exists := make(map[string]model.Skill, len(skills))
		for _, item := range skills {
			exists[item.ID] = item
		}
		for _, item := range defaultSkills() {
			if current, ok := exists[item.ID]; ok {
				if current.BuiltIn {
					item.Enabled = current.Enabled
				} else {
					current.BuiltIn = true
					item = current
				}
			}
			if _, err := a.upsertSkill(item); err != nil {
				return err
			}
		}
	}
	if !skillsInitialized {
		if err := a.markDefaultSeedInitialized(defaultSeedSkillsKey); err != nil {
			return err
		}
	}

	mcpInitialized, err := a.defaultSeedInitialized(defaultSeedMCPCatalogKey)
	if err != nil {
		return err
	}
	mcps, err := a.store.ListMCPCatalog(true)
	if err != nil {
		return err
	}
	if !mcpInitialized {
		exists := make(map[string]model.MCPCatalogItem, len(mcps))
		for _, item := range mcps {
			exists[item.ID] = item
		}
		for _, item := range defaultMCPCatalogItems() {
			if current, ok := exists[item.ID]; ok {
				if current.BuiltIn {
					continue
				}
				current.BuiltIn = true
				item = current
			}
			if _, err := a.upsertMCPCatalogItem(item); err != nil {
				return err
			}
		}
	}
	if !mcpInitialized {
		return a.markDefaultSeedInitialized(defaultSeedMCPCatalogKey)
	}
	return nil
}

func (a *API) ensureDefaultRuntimeCatalog() error {
	initialized, err := a.defaultSeedInitialized(defaultSeedRuntimeCatalogKey)
	if err != nil || initialized {
		return err
	}

	runtimes, err := a.store.ListRuntimeCatalog(true)
	if err != nil {
		return err
	}
	runtimeByID := make(map[string]model.RuntimeCatalogItem, len(runtimes))
	for _, item := range runtimes {
		runtimeByID[item.ID] = item
	}
	for _, item := range builtinRuntimeCatalogItems() {
		if current, ok := runtimeByID[item.ID]; ok {
			if current.BuiltIn {
				continue
			}
			current.BuiltIn = true
			item = current
		}
		if _, err := a.upsertRuntimeCatalogItem(item); err != nil {
			return err
		}
	}

	if err := a.seedMissingRuntimeVersions(); err != nil {
		return err
	}
	if err := a.seedMissingRuntimeArtifacts(); err != nil {
		return err
	}
	if err := a.seedMissingRuntimeMirrors(); err != nil {
		return err
	}
	return a.markDefaultSeedInitialized(defaultSeedRuntimeCatalogKey)
}

func (a *API) seedMissingRuntimeVersions() error {
	items, err := a.store.ListRuntimeVersions(true)
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(items))
	for _, item := range items {
		exists[item.ID] = true
	}
	for _, item := range builtinRuntimeVersions() {
		if exists[item.ID] {
			continue
		}
		if _, err := a.upsertRuntimeVersion(item); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) seedMissingRuntimeArtifacts() error {
	items, err := a.store.ListRuntimeArtifacts(true)
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(items))
	for _, item := range items {
		exists[item.ID] = true
	}
	for _, item := range builtinRuntimeArtifacts() {
		if exists[item.ID] {
			continue
		}
		if _, err := a.upsertRuntimeArtifact(item); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) seedMissingRuntimeMirrors() error {
	items, err := a.store.ListRuntimeMirrors(true)
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(items))
	for _, item := range items {
		exists[item.ID] = true
	}
	for _, item := range builtinRuntimeMirrors() {
		if exists[item.ID] {
			continue
		}
		if _, err := a.upsertRuntimeMirror(item); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) upsertSkill(item model.Skill) (*model.Skill, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Content = strings.TrimSpace(item.Content)
	item.PackageFiles = cleanSkillPackageFiles(item.PackageFiles)
	item.Description = normalizeAssetDescription(item.Description, item.Content)
	item.Category = firstNonEmpty(strings.TrimSpace(item.Category), "通用")
	item.Source = strings.TrimSpace(item.Source)
	item.Tags = cleanStringSlice(item.Tags)
	if item.ID == "" {
		item.ID = item.Name
	}
	if item.ID == "" {
		return nil, errors.New("Skill ID 不能为空")
	}
	if item.Name == "" {
		item.Name = item.ID
	}
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertSkill(item)
}

func (a *API) upsertMCPCatalogItem(item model.MCPCatalogItem) (*model.MCPCatalogItem, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = normalizeAssetDescription(item.Description, "")
	item.Type = strings.TrimSpace(item.Type)
	item.Source = strings.TrimSpace(item.Source)
	item.SourceURL = strings.TrimSpace(item.SourceURL)
	item.CredentialURL = strings.TrimSpace(item.CredentialURL)
	item.Category = firstNonEmpty(strings.TrimSpace(item.Category), "通用")
	item.Tags = cleanStringSlice(item.Tags)
	item.Install = cleanMCPInstallInfo(item.Install)
	if item.SourceURL == "" {
		item.SourceURL = item.Install.SourceURL
	}
	if item.Install.SourceURL == "" {
		item.Install.SourceURL = item.SourceURL
	}
	if item.ID == "" {
		item.ID = item.Name
	}
	if item.ID == "" {
		return nil, errors.New("MCP ID 不能为空")
	}
	if item.Name == "" {
		item.Name = item.ID
	}
	if item.Title == "" {
		item.Title = item.Name
	}
	if item.Type == "" {
		item.Type = item.Config.Type
	}
	if item.Type == "" {
		item.Type = "local"
	}
	item.Config.Name = item.Name
	item.Config.Type = item.Type
	item.Config.Enabled = item.Enabled
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertMCPCatalogItem(item)
}

func cleanMCPInstallInfo(info model.MCPInstallInfo) model.MCPInstallInfo {
	info.PackageManager = strings.TrimSpace(info.PackageManager)
	info.SourceURL = strings.TrimSpace(info.SourceURL)
	info.Assets = cleanMCPInstallAssets(info.Assets)
	info.Files = cleanMCPInstallFiles(info.Files)
	info.InstallCommands = cleanStringSlice(info.InstallCommands)
	info.BuildCommands = cleanStringSlice(info.BuildCommands)
	info.RunCommandTemplate = cleanStringSlice(info.RunCommandTemplate)
	info.ConfigPathTemplate = strings.TrimSpace(info.ConfigPathTemplate)
	info.ExecutableNames = cleanStringSlice(info.ExecutableNames)
	info.Environment = cleanStringMapForAPI(info.Environment)
	info.InstallNotes = strings.TrimSpace(info.InstallNotes)
	return info
}

func cleanMCPInstallAssets(input []model.MCPInstallAsset) []model.MCPInstallAsset {
	out := make([]model.MCPInstallAsset, 0, len(input))
	for _, item := range input {
		item.Name = strings.TrimSpace(item.Name)
		item.URL = strings.TrimSpace(item.URL)
		item.Filename = strings.TrimSpace(item.Filename)
		item.SHA256 = strings.TrimSpace(item.SHA256)
		item.PackageKind = strings.TrimSpace(item.PackageKind)
		item.TargetDir = strings.TrimSpace(item.TargetDir)
		item.Headers = cleanStringMapForAPI(item.Headers)
		if item.URL == "" && item.Filename == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cleanMCPInstallFiles(input []model.MCPInstallFile) []model.MCPInstallFile {
	out := make([]model.MCPInstallFile, 0, len(input))
	for _, item := range input {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeAssetDescription(description string, content string) string {
	description = compactInlineText(description)
	if !isPlaceholderDescription(description) {
		return description
	}
	fromContent := compactInlineText(extractFrontMatterDescription(content))
	if isPlaceholderDescription(fromContent) {
		return ""
	}
	return fromContent
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

func isPlaceholderDescription(value string) bool {
	switch strings.TrimSpace(value) {
	case "", ">", ">-", ">+", "|", "|-":
		return true
	default:
		return false
	}
}

func compactInlineText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func extractFrontMatterDescription(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for index := 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return ""
		}
		if !strings.HasPrefix(trimmed, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		switch value {
		case ">", ">-", ">+", "|", "|-", "|+":
			return collectIndentedFrontMatterValue(lines, index+1)
		default:
			return strings.Trim(strings.Trim(value, `"`), `'`)
		}
	}
	return ""
}

func collectIndentedFrontMatterValue(lines []string, start int) string {
	var out []string
	for index := start; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if trimmed == "" {
			continue
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, " ")
}

func (a *API) upsertSemanticAgent(item model.SemanticAgentProfile) (*model.SemanticAgentProfile, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	item.Icon = strings.TrimSpace(item.Icon)
	item.Color = strings.TrimSpace(item.Color)
	item.OpencodeName = strings.TrimSpace(item.OpencodeName)
	item.Prompt = strings.TrimSpace(item.Prompt)
	item.Model = strings.TrimSpace(item.Model)
	item.SkillIDs = cleanStringSlice(item.SkillIDs)
	item.MCPIDs = cleanStringSlice(item.MCPIDs)
	item.RuntimeRequirements = cleanSemanticAgentRuntimeRequirements(item.RuntimeRequirements)
	if item.ID == "" {
		return nil, errors.New("语义 Agent ID 不能为空")
	}
	if item.Name == "" {
		return nil, errors.New("语义 Agent 名称不能为空")
	}
	if item.OpencodeName == "" {
		item.OpencodeName = item.ID
	}
	item.Skills = cleanStringSlice(item.Skills)
	item.RecommendedMCPServers = cleanStringSlice(item.RecommendedMCPServers)
	cleanSkills := make([]model.SemanticAgentSkill, 0, len(item.SkillDefinitions))
	for _, skill := range item.SkillDefinitions {
		skill.Name = strings.TrimSpace(skill.Name)
		skill.Description = strings.TrimSpace(skill.Description)
		skill.Content = strings.TrimSpace(skill.Content)
		skill.PackageFiles = cleanSkillPackageFiles(skill.PackageFiles)
		if skill.Name == "" {
			continue
		}
		cleanSkills = append(cleanSkills, skill)
	}
	item.SkillDefinitions = cleanSkills
	if len(cleanSkills) > 0 && len(item.SkillIDs) > 0 && !sameStringSet(item.SkillIDs, skillDefinitionNames(cleanSkills)) {
		item.SkillIDs = nil
	}
	if len(item.Skills) == 0 && len(item.SkillDefinitions) > 0 {
		for _, skill := range item.SkillDefinitions {
			item.Skills = append(item.Skills, skill.Name)
		}
	}
	if len(item.SkillIDs) == 0 && len(item.SkillDefinitions) > 0 {
		for index, skill := range item.SkillDefinitions {
			stored, err := a.upsertSkill(model.Skill{
				ID:           skill.Name,
				Name:         skill.Name,
				Description:  skill.Description,
				Content:      skill.Content,
				PackageFiles: cloneSkillPackageFiles(skill.PackageFiles),
				Source:       "语义 Agent 导入",
				Enabled:      true,
				SortOrder:    (index + 1) * 10,
			})
			if err != nil {
				return nil, err
			}
			item.SkillIDs = append(item.SkillIDs, stored.ID)
		}
	}
	if len(item.MCPIDs) == 0 && len(item.RecommendedMCPServers) > 0 {
		item.MCPIDs = append([]string(nil), item.RecommendedMCPServers...)
	}
	if err := a.expandSemanticAgentReferences(&item); err != nil {
		return nil, err
	}
	if item.ToolPermissions == nil {
		item.ToolPermissions = map[string]any{}
	}
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertSemanticAgent(item)
}

func cleanSemanticAgentRuntimeRequirements(input []model.SemanticAgentRuntime) []model.SemanticAgentRuntime {
	if input == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]model.SemanticAgentRuntime, 0, len(input))
	for index, item := range input {
		item.RuntimeID = strings.TrimSpace(item.RuntimeID)
		if item.RuntimeID == "" || seen[item.RuntimeID] {
			continue
		}
		seen[item.RuntimeID] = true
		item.VersionConstraint = strings.TrimSpace(item.VersionConstraint)
		item.Purpose = strings.TrimSpace(item.Purpose)
		if item.SortOrder == 0 {
			item.SortOrder = (index + 1) * 10
		}
		out = append(out, item)
	}
	return out
}

func (a *API) expandSemanticAgentReferences(item *model.SemanticAgentProfile) error {
	if item == nil {
		return nil
	}
	item.RecommendedMCPConfigs = nil
	item.MCPDependencies = nil
	if len(item.SkillIDs) > 0 {
		skills, err := a.store.ListSkills(true)
		if err != nil {
			return err
		}
		byID := map[string]model.Skill{}
		for _, skill := range skills {
			byID[skill.ID] = skill
		}
		for _, skill := range skills {
			if _, exists := byID[skill.Name]; !exists {
				byID[skill.Name] = skill
			}
		}
		item.Skills = nil
		item.SkillDefinitions = nil
		filteredSkillIDs := make([]string, 0, len(item.SkillIDs))
		for _, id := range item.SkillIDs {
			skill, ok := byID[id]
			if !ok {
				continue
			}
			filteredSkillIDs = append(filteredSkillIDs, skill.ID)
			item.Skills = append(item.Skills, skill.ID)
			item.SkillDefinitions = append(item.SkillDefinitions, model.SemanticAgentSkill{
				Name:         skill.ID,
				Description:  skill.Description,
				Content:      skill.Content,
				PackageFiles: cloneSkillPackageFiles(skill.PackageFiles),
			})
		}
		item.SkillIDs = filteredSkillIDs
	}
	if len(item.MCPIDs) > 0 {
		mcps, err := a.store.ListMCPCatalog(true)
		if err != nil {
			return err
		}
		mcps = filterLegacyReverseMCPCatalogItems(mcps)
		byID := map[string]model.MCPCatalogItem{}
		for _, mcp := range mcps {
			byID[mcp.ID] = mcp
		}
		for _, mcp := range mcps {
			if _, exists := byID[mcp.Name]; !exists {
				byID[mcp.Name] = mcp
			}
		}
		item.RecommendedMCPServers = nil
		filteredMCPIDs := make([]string, 0, len(item.MCPIDs))
		for _, id := range item.MCPIDs {
			mcp, ok := byID[id]
			if !ok {
				continue
			}
			filteredMCPIDs = append(filteredMCPIDs, mcp.ID)
			item.RecommendedMCPServers = append(item.RecommendedMCPServers, mcp.Name)
			item.MCPDependencies = append(item.MCPDependencies, semanticAgentMCPDependencyFromCatalog(mcp))
			if mcp.LaunchReady {
				item.RecommendedMCPConfigs = append(item.RecommendedMCPConfigs, mcp.Config)
			}
		}
		item.MCPIDs = filteredMCPIDs
	}
	if len(item.RuntimeRequirements) > 0 {
		runtimes, err := a.store.ListRuntimeCatalog(true)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, runtime := range runtimes {
			known[runtime.ID] = true
			known[runtime.Name] = true
		}
		filteredRuntimes := make([]model.SemanticAgentRuntime, 0, len(item.RuntimeRequirements))
		for _, runtime := range item.RuntimeRequirements {
			if known[runtime.RuntimeID] {
				filteredRuntimes = append(filteredRuntimes, runtime)
			}
		}
		item.RuntimeRequirements = filteredRuntimes
	}
	if len(item.ToolPermissions) > 0 {
		tools, err := a.store.ListToolCatalog(true)
		if err != nil {
			return err
		}
		if len(tools) > 0 {
			item.ToolPermissions = filterToolPermissionsForCatalog(item.ToolPermissions, tools)
		}
	}
	return nil
}

func semanticAgentMCPDependencyFromCatalog(item model.MCPCatalogItem) model.SemanticAgentMCPDependency {
	return model.SemanticAgentMCPDependency{
		ID:                item.ID,
		Name:              item.Name,
		Title:             item.Title,
		Type:              item.Type,
		SourceURL:         item.SourceURL,
		Config:            item.Config,
		Install:           item.Install,
		LaunchReady:       item.LaunchReady,
		LaunchBlockReason: item.LaunchBlockReason,
	}
}

func filterToolPermissionsForCatalog(permissions map[string]any, tools []model.ToolCatalogItem) map[string]any {
	if permissions == nil {
		return nil
	}
	out := map[string]any{}
	for _, tool := range tools {
		if strings.TrimSpace(tool.PermissionKey) == "" {
			continue
		}
		next, changed := pickToolPermission(permissions, tool)
		if !changed {
			continue
		}
		out = mergeToolPermission(out, tool, next)
	}
	if len(out) == 0 {
		return map[string]any{}
	}
	return out
}

func pickToolPermission(permissions map[string]any, tool model.ToolCatalogItem) (string, bool) {
	key := strings.TrimSpace(tool.PermissionKey)
	pattern := strings.TrimSpace(tool.Pattern)
	if pattern == "" {
		pattern = "*"
	}
	value, ok := permissions[key]
	if !ok {
		return "", false
	}
	if action, ok := value.(string); ok {
		if pattern != "*" {
			return "", false
		}
		action = normalizeToolPermissionAction(action)
		if action == "" {
			return "", false
		}
		return action, true
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	raw, ok := nested[pattern]
	if !ok {
		return "", false
	}
	action, ok := raw.(string)
	if !ok {
		return "", false
	}
	action = normalizeToolPermissionAction(action)
	if action == "" {
		return "", false
	}
	return action, true
}

func mergeToolPermission(permissions map[string]any, tool model.ToolCatalogItem, action string) map[string]any {
	key := strings.TrimSpace(tool.PermissionKey)
	pattern := strings.TrimSpace(tool.Pattern)
	if pattern == "" {
		pattern = "*"
	}
	if key == "" || action == "" {
		return permissions
	}
	current := permissions[key]
	if pattern == "*" {
		if nested, ok := current.(map[string]any); ok {
			nested["*"] = action
			permissions[key] = nested
			return permissions
		}
		permissions[key] = action
		return permissions
	}
	nested, ok := current.(map[string]any)
	if !ok {
		nested = map[string]any{}
		if currentAction, ok := current.(string); ok {
			nested["*"] = currentAction
		}
	}
	nested[pattern] = action
	permissions[key] = nested
	return permissions
}

func filterLegacyReverseMCPCatalogItems(items []model.MCPCatalogItem) []model.MCPCatalogItem {
	if len(items) == 0 {
		return items
	}
	out := make([]model.MCPCatalogItem, 0, len(items))
	for _, item := range items {
		if isLegacyReverseMCPCatalogItem(item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func skillDefinitionNames(input []model.SemanticAgentSkill) []string {
	out := make([]string, 0, len(input))
	for _, skill := range input {
		if strings.TrimSpace(skill.Name) != "" {
			out = append(out, strings.TrimSpace(skill.Name))
		}
	}
	return out
}

func sameStringSet(left []string, right []string) bool {
	left = cleanStringSlice(left)
	right = cleanStringSlice(right)
	if len(left) != len(right) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
}

func enrichSemanticSelection(selection *model.DeviceAgentSemanticSelection, profiles []model.SemanticAgentProfile) {
	if selection == nil {
		return
	}
	available := make([]model.SemanticAgentProfile, 0, len(profiles))
	for _, profile := range profiles {
		available = append(available, displaySemanticAgentProfile(profile))
		if profile.ID == selection.SemanticAgentID {
			display := displaySemanticAgentProfile(profile)
			selection.Profile = &display
		}
	}
	selection.AvailableAgents = available
}

func displaySemanticAgentProfile(profile model.SemanticAgentProfile) model.SemanticAgentProfile {
	profile.Prompt = ""
	profile.SkillDefinitions = nil
	profile.RecommendedMCPConfigs = nil
	profile.MCPDependencies = nil
	profile.ToolPermissions = nil
	return profile
}

func cleanStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
