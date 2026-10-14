package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
	"relay-server/internal/promptregistry"
	"relay-server/internal/store"
)

// promptTemplateSettingPrefix 是设备 Agent 级覆盖在 device_settings 里的 key 前缀。
const promptTemplateSettingPrefix = "prompt_template."

// promptPushMinInterval 限制同一设备 Agent 的重复下发频率：
// 连续保存 / 重复触发时，revision 未变就不再发 RPC。
const promptPushMinInterval = 3 * time.Second

// promptPushState 记录最近一次下发结果，用于抖动抑制与增量判断。
type promptPushRecord struct {
	revision string
	at       time.Time
}

func promptPushKey(operatorID int64, agentID string) string {
	return fmt.Sprintf("%d/%s", operatorID, agentID)
}

type promptSlotOverrideView struct {
	SemanticAgentID   string `json:"semantic_agent_id"`
	SemanticAgentName string `json:"semantic_agent_name"`
}

// promptSlotView 是控制台看到的槽位视图：定义 + 平台值 + 覆盖情况。
type promptSlotView struct {
	promptregistry.Slot
	PlatformValue       string                   `json:"platform_value"`
	PlatformSet         bool                     `json:"platform_set"`
	Revision            int64                    `json:"revision"`
	UpdatedAt           string                   `json:"updated_at,omitempty"`
	Overrides           []promptSlotOverrideView `json:"overrides,omitempty"`
	DeviceOverrideCount int                      `json:"device_override_count"`
}

type promptTemplatesPayload struct {
	AgentID   string            `json:"agent_id"`
	Revision  string            `json:"revision"`
	Templates map[string]string `json:"templates"`
}

// promptDeviceAgentView 是设备 Agent 列表项（控制台设备级覆盖的下拉数据源）。
type promptDeviceAgentView struct {
	OperatorID        int64  `json:"operator_id"`
	OperatorUsername  string `json:"operator_username"`
	MachineID         string `json:"machine_id"`
	Hostname          string `json:"hostname"`
	LauncherID        string `json:"launcher_id"`
	AgentID           string `json:"agent_id"`
	AgentName         string `json:"agent_name"`
	SemanticAgentID   string `json:"semantic_agent_id,omitempty"`
	SemanticAgentName string `json:"semantic_agent_name,omitempty"`
	Status            string `json:"status,omitempty"`
	AppliedRevision   string `json:"applied_revision,omitempty"`
}

// promptSlotResolution 是单个槽位在某一设备 Agent 上的三层取值明细。
type promptSlotResolution struct {
	SlotID        string `json:"slot_id"`
	DeviceValue   string `json:"device_value"`
	SemanticValue string `json:"semantic_value"`
	PlatformValue string `json:"platform_value"`
	Effective     string `json:"effective"`
	Source        string `json:"source"`
}

// ListPromptRegistry 返回全部提示词槽位（含内置默认、平台值与覆盖统计）。
func (a *API) ListPromptRegistry(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	stored, err := a.store.ListPromptTemplates()
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	agents, err := a.store.ListSemanticAgents(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	deviceCounts, err := a.store.PromptTemplateDeviceOverrideCounts()
	if err != nil {
		// 覆盖统计失败不影响主流程，按 0 展示。
		log.Printf("[prompt] 读取设备覆盖统计失败 err=%v", err)
		deviceCounts = map[string]int{}
	}
	items := make([]promptSlotView, 0, len(promptregistry.All()))
	for _, slot := range promptregistry.All() {
		view := promptSlotView{Slot: slot, DeviceOverrideCount: deviceCounts[slot.ID]}
		if item, ok := stored[slot.ID]; ok {
			view.PlatformValue = item.Content
			view.PlatformSet = strings.TrimSpace(item.Content) != ""
			view.Revision = item.Revision
			if !item.UpdatedAt.IsZero() {
				view.UpdatedAt = item.UpdatedAt.Format(time.RFC3339)
			}
		}
		for _, agent := range agents {
			if content, ok := agent.PromptTemplates[slot.ID]; ok && strings.TrimSpace(content) != "" {
				view.Overrides = append(view.Overrides, promptSlotOverrideView{
					SemanticAgentID:   agent.ID,
					SemanticAgentName: agent.Name,
				})
			}
		}
		items = append(items, view)
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

// SavePromptTemplate 保存平台默认值。
func (a *API) SavePromptTemplate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	slot, ok := promptregistry.ByID(chi.URLParam(r, "slotID"))
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "未知提示词槽位"})
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "内容不能为空；如需恢复内置默认请使用 reset"})
		return
	}
	if missing := promptregistry.MissingVariables(slot, req.Content); len(missing) > 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("缺少必需占位符: %s", strings.Join(missing, ", "))})
		return
	}
	stored, err := a.store.UpsertPromptTemplate(slot.ID, req.Content)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pushed := a.pushPromptTemplatesToOnlineDevices(r.Context())
	write(w, http.StatusOK, map[string]any{
		"slot_id":    stored.SlotID,
		"revision":   stored.Revision,
		"updated_at": stored.UpdatedAt,
		"pushed":     pushed,
	})
}

// ResetPromptTemplate 删除平台默认值，槽位回落到内置默认。
func (a *API) ResetPromptTemplate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	slot, ok := promptregistry.ByID(chi.URLParam(r, "slotID"))
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "未知提示词槽位"})
		return
	}
	if err := a.store.DeletePromptTemplate(slot.ID); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pushed := a.pushPromptTemplatesToOnlineDevices(r.Context())
	write(w, http.StatusOK, map[string]any{"success": true, "pushed": pushed})
}

// ListPromptDeviceAgents 列出当前在线的设备 Agent，供控制台做设备级覆盖。
// 只列在线设备：设备级覆盖必须下发到设备才生效，离线设备上线后由后端自动补齐。
func (a *API) ListPromptDeviceAgents(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	operators, err := a.store.ListOperators()
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	semanticAgents, err := a.store.ListSemanticAgents(true)
	if err != nil {
		semanticAgents = nil
	}
	semanticNames := map[string]string{}
	for _, agent := range semanticAgents {
		semanticNames[agent.ID] = agent.Name
	}
	items := make([]promptDeviceAgentView, 0)
	for _, operator := range operators {
		for _, machine := range a.broker.ListMachines(operator.ID) {
			if !machine.LauncherOnline {
				continue
			}
			launcherID, err := a.deviceLauncherAgent(operator.ID, machine.MachineID)
			if err != nil {
				continue
			}
			// 合并 launcher 托管的 Agent 清单：补齐显示名与启用状态（含已停止的 Agent，
			// 槽位文件由 launcher 落盘，Agent 下次启动即生效）。
			machine = a.mergeLauncherManagedAgents(r.Context(), operator.ID, machine)
			for _, agent := range machine.Agents {
				agentID := strings.TrimSpace(agent.ID)
				if agentID == "" || !agent.Enabled {
					continue
				}
				applied, _ := a.store.PromptTemplateAppliedRevision(operator.ID, agentID)
				items = append(items, promptDeviceAgentView{
					OperatorID:        operator.ID,
					OperatorUsername:  operator.Username,
					MachineID:         machine.MachineID,
					Hostname:          machine.Hostname,
					LauncherID:        launcherID,
					AgentID:           agentID,
					AgentName:         agent.Name,
					SemanticAgentID:   agent.SemanticAgentID,
					SemanticAgentName: semanticNames[agent.SemanticAgentID],
					Status:            agent.Status,
					AppliedRevision:   applied,
				})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OperatorID != items[j].OperatorID {
			return items[i].OperatorID < items[j].OperatorID
		}
		if items[i].MachineID != items[j].MachineID {
			return items[i].MachineID < items[j].MachineID
		}
		return items[i].AgentID < items[j].AgentID
	})
	write(w, http.StatusOK, map[string]any{"items": items})
}

// resolveDeviceAgentOwner 解析设备 Agent 的归属账号：以在线 launcher 的归属为准。
func (a *API) resolveDeviceAgentOwner(machineID string) (int64, string, error) {
	launcher, ok := a.broker.GetLauncher(machineID)
	if !ok {
		return 0, "", fmt.Errorf("设备不在线或 launcher 未连接，暂时无法管理设备级覆盖")
	}
	return launcher.OperatorID, launcher.ID, nil
}

// devicePromptOverrides 读取某设备 Agent 在 device_settings 里的提示词覆盖。
func (a *API) devicePromptOverrides(operatorID int64, agentID string) map[string]string {
	out := map[string]string{}
	settings, err := a.store.GetDeviceSettings(operatorID, agentID)
	if err != nil {
		log.Printf("[prompt] 读取设备覆盖失败 operator_id=%d agent_id=%s err=%v", operatorID, agentID, err)
		return out
	}
	for key, value := range settings {
		if !strings.HasPrefix(key, promptTemplateSettingPrefix) {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		out[strings.TrimPrefix(key, promptTemplateSettingPrefix)] = value
	}
	return out
}

// resolvePromptTemplates 解析某设备 Agent 的最终提示词：
// 设备 Agent 覆盖 → 语义 Agent 覆盖 → 平台默认 → 内置默认。
func (a *API) resolvePromptTemplates(operatorID int64, agentID, semanticAgentID string) (map[string]string, map[string]string) {
	stored, err := a.store.ListPromptTemplates()
	if err != nil {
		log.Printf("[prompt] 读取平台默认失败 operator_id=%d err=%v", operatorID, err)
		stored = map[string]store.PromptTemplate{}
	}
	deviceOverrides := a.devicePromptOverrides(operatorID, agentID)
	semanticOverrides := map[string]string{}
	if strings.TrimSpace(semanticAgentID) != "" {
		if profile, err := a.semanticAgentByID(semanticAgentID, true); err == nil {
			semanticOverrides = profile.PromptTemplates
		}
	}
	out := map[string]string{}
	sources := map[string]string{}
	for _, slot := range promptregistry.All() {
		switch {
		case strings.TrimSpace(deviceOverrides[slot.ID]) != "":
			out[slot.ID] = deviceOverrides[slot.ID]
			sources[slot.ID] = "device"
		case strings.TrimSpace(semanticOverrides[slot.ID]) != "":
			out[slot.ID] = semanticOverrides[slot.ID]
			sources[slot.ID] = "semantic"
		case strings.TrimSpace(stored[slot.ID].Content) != "":
			out[slot.ID] = stored[slot.ID].Content
			sources[slot.ID] = "platform"
		default:
			// 未覆盖的槽位也下发（内容为空）：设备端据此知道完整槽位清单，
			// 用于在 opencode 配置里声明 prompt_slots；空内容运行时会回落内置默认。
			out[slot.ID] = ""
			sources[slot.ID] = "builtin"
		}
	}
	return out, sources
}

// resolvePromptTemplateDetail 返回三层取值明细，供控制台在设备级覆盖页展示"当前生效值"。
func (a *API) resolvePromptTemplateDetail(operatorID int64, agentID, semanticAgentID string) []promptSlotResolution {
	stored, err := a.store.ListPromptTemplates()
	if err != nil {
		stored = map[string]store.PromptTemplate{}
	}
	deviceOverrides := a.devicePromptOverrides(operatorID, agentID)
	semanticOverrides := map[string]string{}
	if strings.TrimSpace(semanticAgentID) != "" {
		if profile, err := a.semanticAgentByID(semanticAgentID, true); err == nil {
			semanticOverrides = profile.PromptTemplates
		}
	}
	items := make([]promptSlotResolution, 0, len(promptregistry.All()))
	for _, slot := range promptregistry.All() {
		item := promptSlotResolution{
			SlotID:        slot.ID,
			DeviceValue:   deviceOverrides[slot.ID],
			SemanticValue: semanticOverrides[slot.ID],
			PlatformValue: stored[slot.ID].Content,
		}
		switch {
		case strings.TrimSpace(item.DeviceValue) != "":
			item.Effective, item.Source = item.DeviceValue, "device"
		case strings.TrimSpace(item.SemanticValue) != "":
			item.Effective, item.Source = item.SemanticValue, "semantic"
		case strings.TrimSpace(item.PlatformValue) != "":
			item.Effective, item.Source = item.PlatformValue, "platform"
		default:
			item.Effective, item.Source = slot.Default, "builtin"
		}
		items = append(items, item)
	}
	return items
}

// GetPromptDeviceOverrides 返回某设备 Agent 的三层取值明细。
func (a *API) GetPromptDeviceOverrides(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if machineID == "" || agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "machine_id 与 agent_id 必填"})
		return
	}
	operatorID, launcherID, err := a.resolveDeviceAgentOwner(machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	semanticAgentID := strings.TrimSpace(r.URL.Query().Get("semantic_agent_id"))
	if semanticAgentID == "" {
		semanticAgentID = a.deviceAgentSemanticID(r.Context(), operatorID, launcherID, agentID)
	}
	write(w, http.StatusOK, map[string]any{
		"operator_id":       operatorID,
		"machine_id":        machineID,
		"agent_id":          agentID,
		"semantic_agent_id": semanticAgentID,
		"items":             a.resolvePromptTemplateDetail(operatorID, agentID, semanticAgentID),
	})
}

// SavePromptDeviceOverrides 保存某设备 Agent 的提示词覆盖（空内容 = 清除该槽位覆盖）。
func (a *API) SavePromptDeviceOverrides(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req struct {
		MachineID       string            `json:"machine_id"`
		AgentID         string            `json:"agent_id"`
		SemanticAgentID string            `json:"semantic_agent_id"`
		Overrides       map[string]string `json:"overrides"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.MachineID = strings.TrimSpace(req.MachineID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	if req.MachineID == "" || req.AgentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "machine_id 与 agent_id 必填"})
		return
	}
	operatorID, launcherID, err := a.resolveDeviceAgentOwner(req.MachineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	for slotID, content := range req.Overrides {
		slot, ok := promptregistry.ByID(slotID)
		if !ok {
			write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("未知提示词槽位: %s", slotID)})
			return
		}
		if !promptregistry.SupportsScope(slot, promptregistry.ScopeDevice) {
			write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("槽位 %s 不支持设备级覆盖", slotID)})
			return
		}
		if strings.TrimSpace(content) == "" {
			if err := a.store.DeleteDeviceSetting(operatorID, req.AgentID, promptTemplateSettingPrefix+slotID); err != nil {
				write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			continue
		}
		if missing := promptregistry.MissingVariables(slot, content); len(missing) > 0 {
			write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("缺少必需占位符: %s", strings.Join(missing, ", "))})
			return
		}
		if err := a.store.SetDeviceSetting(operatorID, req.AgentID, promptTemplateSettingPrefix+slotID, content); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	revision, pushed := a.syncDeviceAgentPromptTemplatesWithSemantic(r.Context(), operatorID, launcherID, req.AgentID, req.SemanticAgentID, false)
	write(w, http.StatusOK, map[string]any{
		"success":           true,
		"operator_id":       operatorID,
		"machine_id":        req.MachineID,
		"agent_id":          req.AgentID,
		"revision":          revision,
		"pushed":            pushed,
		"effective_changed": pushed,
	})
}

// SavePromptSemanticOverrides 保存某语义 Agent 的提示词覆盖（空内容 = 清除该槽位覆盖）。
// 只改 prompt_templates 字段，其余角色配置保持库里原样，避免控制台局部编辑覆盖整份配置。
func (a *API) SavePromptSemanticOverrides(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req struct {
		SemanticAgentID string            `json:"semantic_agent_id"`
		Overrides       map[string]string `json:"overrides"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.SemanticAgentID = strings.TrimSpace(req.SemanticAgentID)
	if req.SemanticAgentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "semantic_agent_id 必填"})
		return
	}
	profile, err := a.semanticAgentByID(req.SemanticAgentID, true)
	if err != nil {
		write(w, http.StatusNotFound, map[string]string{"error": "语义 Agent 不存在"})
		return
	}
	next := map[string]string{}
	for slotID, content := range profile.PromptTemplates {
		next[slotID] = content
	}
	for slotID, content := range req.Overrides {
		slot, ok := promptregistry.ByID(slotID)
		if !ok {
			write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("未知提示词槽位: %s", slotID)})
			return
		}
		if !promptregistry.SupportsScope(slot, promptregistry.ScopeSemantic) {
			write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("槽位 %s 不支持语义 Agent 级覆盖", slotID)})
			return
		}
		if strings.TrimSpace(content) == "" {
			delete(next, slotID)
			continue
		}
		if missing := promptregistry.MissingVariables(slot, content); len(missing) > 0 {
			write(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("缺少必需占位符: %s", strings.Join(missing, ", "))})
			return
		}
		next[slotID] = content
	}
	profile.PromptTemplates = promptregistry.SanitizeOverrides(promptregistry.ScopeSemantic, next)
	stored, err := a.upsertSemanticAgent(profile)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	pushed := a.pushPromptTemplatesToSemanticAgentDevices(r.Context(), stored.ID)
	write(w, http.StatusOK, map[string]any{
		"success":           true,
		"semantic_agent_id": stored.ID,
		"prompt_templates":  stored.PromptTemplates,
		"pushed":            pushed,
	})
}

// promptTemplatesRevision 对解析结果取稳定短哈希，供设备端比对是否需要更新。
func promptTemplatesRevision(templates map[string]string) string {
	ids := make([]string, 0, len(templates))
	for id := range templates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sum := sha256.New()
	for _, id := range ids {
		sum.Write([]byte(id))
		sum.Write([]byte{0})
		sum.Write([]byte(templates[id]))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

// recentPromptPush 判断同一设备 Agent 在抖动窗口内是否刚下发过同一 revision。
func (a *API) recentPromptPush(operatorID int64, agentID, revision string) bool {
	a.promptPushMu.Lock()
	defer a.promptPushMu.Unlock()
	recent, ok := a.promptPushLog[promptPushKey(operatorID, agentID)]
	return ok && recent.revision == revision && time.Since(recent.at) < promptPushMinInterval
}

// recordPromptPush 记录一次成功下发，用于抖动抑制。
func (a *API) recordPromptPush(operatorID int64, agentID, revision string) {
	a.promptPushMu.Lock()
	defer a.promptPushMu.Unlock()
	if a.promptPushLog == nil {
		a.promptPushLog = map[string]promptPushRecord{}
	}
	a.promptPushLog[promptPushKey(operatorID, agentID)] = promptPushRecord{revision: revision, at: time.Now()}
}

// deviceAgentSemanticID 读取设备 Agent 当前绑定的语义 Agent（launcher 不可达时返回空串）。
func (a *API) deviceAgentSemanticID(ctx context.Context, operatorID int64, launcherID, agentID string) string {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(launcherID) == "" {
		return ""
	}
	loaded, err := a.requestDeviceLauncher(ctx, operatorID, launcherID, fmt.Sprintf("req_prompt_slots_get_%d", time.Now().UnixNano()), "device.launcher.semantic_agent_get", model.DeviceAgentSemanticSelectionPayload{AgentID: agentID})
	if err != nil || loaded.SemanticSelection == nil {
		return ""
	}
	return loaded.SemanticSelection.SemanticAgentID
}

// syncDeviceAgentPromptTemplates 解析并向指定设备 Agent 推送提示词（重启前 / 控制台保存后调用）。
func (a *API) syncDeviceAgentPromptTemplates(ctx context.Context, operatorID int64, launcherID, agentID string, force bool) (string, bool) {
	semanticAgentID := a.deviceAgentSemanticID(ctx, operatorID, launcherID, agentID)
	return a.syncDeviceAgentPromptTemplatesWithSemantic(ctx, operatorID, launcherID, agentID, semanticAgentID, force)
}

// syncDeviceAgentPromptTemplatesWithSemantic 是带已知语义 Agent 的推送实现：
// revision 与最近一次成功下发一致时跳过（增量），force=true 时强制下发（重启前补推）。
func (a *API) syncDeviceAgentPromptTemplatesWithSemantic(ctx context.Context, operatorID int64, launcherID, agentID, semanticAgentID string, force bool) (string, bool) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", false
	}
	templates, sources := a.resolvePromptTemplates(operatorID, agentID, semanticAgentID)
	revision := promptTemplatesRevision(templates)
	if !force {
		if applied, err := a.store.PromptTemplateAppliedRevision(operatorID, agentID); err == nil && applied == revision {
			return revision, false
		}
		if a.recentPromptPush(operatorID, agentID, revision) {
			return revision, false
		}
	}
	payload := promptTemplatesPayload{AgentID: agentID, Revision: revision, Templates: templates}
	if _, err := a.requestDeviceLauncher(ctx, operatorID, launcherID, fmt.Sprintf("req_prompt_slots_save_%d", time.Now().UnixNano()), "device.launcher.prompt_templates_save", payload); err != nil {
		log.Printf("[prompt] 推送提示词失败 agent_id=%s revision=%s err=%v", agentID, revision, err)
		return revision, false
	}
	if err := a.store.SetPromptTemplateAppliedRevision(operatorID, agentID, revision); err != nil {
		log.Printf("[prompt] 记录已下发 revision 失败 agent_id=%s revision=%s err=%v", agentID, revision, err)
	}
	a.recordPromptPush(operatorID, agentID, revision)
	log.Printf("[prompt] 已推送提示词 agent_id=%s semantic_agent_id=%s revision=%s sources=%v", agentID, semanticAgentID, revision, sources)
	return revision, true
}

// pushPromptTemplatesToOnlineDevices 平台默认变更后推送给所有在线设备 Agent。
// 逐台比对 revision：未变化的设备只做一次内存/DB 判断，不发 RPC。
func (a *API) pushPromptTemplatesToOnlineDevices(ctx context.Context) int {
	operators, err := a.store.ListOperators()
	if err != nil {
		log.Printf("[prompt] 平台默认推送跳过：读取账号失败 err=%v", err)
		return 0
	}
	pushed := 0
	for _, operator := range operators {
		for _, machine := range a.broker.ListMachines(operator.ID) {
			if !machine.LauncherOnline {
				continue
			}
			launcherID, err := a.deviceLauncherAgent(operator.ID, machine.MachineID)
			if err != nil {
				continue
			}
			for _, agent := range machine.Agents {
				if !agent.Enabled || strings.TrimSpace(agent.ID) == "" {
					continue
				}
				if _, ok := a.syncDeviceAgentPromptTemplatesWithSemantic(ctx, operator.ID, launcherID, agent.ID, agent.SemanticAgentID, false); ok {
					pushed++
				}
			}
		}
	}
	return pushed
}

// pushPromptTemplatesToSemanticAgentDevices 语义 Agent 覆盖变更后，推送给所有绑定它的在线设备 Agent。
func (a *API) pushPromptTemplatesToSemanticAgentDevices(ctx context.Context, semanticAgentID string) int {
	semanticAgentID = strings.TrimSpace(semanticAgentID)
	if semanticAgentID == "" {
		return 0
	}
	operators, err := a.store.ListOperators()
	if err != nil {
		return 0
	}
	pushed := 0
	for _, operator := range operators {
		for _, machine := range a.broker.ListMachines(operator.ID) {
			if !machine.LauncherOnline {
				continue
			}
			launcherID, err := a.deviceLauncherAgent(operator.ID, machine.MachineID)
			if err != nil {
				continue
			}
			for _, agent := range machine.Agents {
				if !agent.Enabled || strings.TrimSpace(agent.ID) == "" {
					continue
				}
				if strings.TrimSpace(agent.SemanticAgentID) != semanticAgentID {
					continue
				}
				if _, ok := a.syncDeviceAgentPromptTemplatesWithSemantic(ctx, operator.ID, launcherID, agent.ID, semanticAgentID, false); ok {
					pushed++
				}
			}
		}
	}
	return pushed
}

// catchUpPromptTemplates 周期性补齐：离线期间错过的提示词变更，在设备上线后自动补推。
func (a *API) catchUpPromptTemplates(ctx context.Context) {
	operators, err := a.store.ListOperators()
	if err != nil {
		return
	}
	for _, operator := range operators {
		for _, machine := range a.broker.ListMachines(operator.ID) {
			if !machine.LauncherOnline {
				continue
			}
			launcherID, err := a.deviceLauncherAgent(operator.ID, machine.MachineID)
			if err != nil {
				continue
			}
			for _, agent := range machine.Agents {
				if !agent.Enabled || strings.TrimSpace(agent.ID) == "" {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				a.syncDeviceAgentPromptTemplatesWithSemantic(ctx, operator.ID, launcherID, agent.ID, agent.SemanticAgentID, false)
			}
		}
	}
}

// StartPromptTemplateSyncLoop 启动提示词补齐循环：设备重新上线后自动收到最新槽位。
func (a *API) StartPromptTemplateSyncLoop(ctx context.Context) {
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				a.catchUpPromptTemplates(ctx)
				timer.Reset(3 * time.Minute)
			}
		}
	}()
}
