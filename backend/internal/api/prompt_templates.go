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

type promptSlotOverrideView struct {
	SemanticAgentID   string `json:"semantic_agent_id"`
	SemanticAgentName string `json:"semantic_agent_name"`
}

// promptSlotView 是控制台看到的槽位视图：定义 + 平台值 + 覆盖情况。
type promptSlotView struct {
	promptregistry.Slot
	PlatformValue string                   `json:"platform_value"`
	PlatformSet   bool                     `json:"platform_set"`
	Revision      int64                    `json:"revision"`
	UpdatedAt     string                   `json:"updated_at,omitempty"`
	Overrides     []promptSlotOverrideView `json:"overrides,omitempty"`
}

type promptTemplatesPayload struct {
	AgentID   string            `json:"agent_id"`
	Revision  string            `json:"revision"`
	Templates map[string]string `json:"templates"`
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
	items := make([]promptSlotView, 0, len(promptregistry.All()))
	for _, slot := range promptregistry.All() {
		view := promptSlotView{Slot: slot}
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
	a.pushPromptTemplatesToOnlineDevices(r.Context())
	write(w, http.StatusOK, stored)
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
	a.pushPromptTemplatesToOnlineDevices(r.Context())
	write(w, http.StatusOK, map[string]any{"success": true})
}

// resolvePromptTemplates 解析某设备 Agent 的最终提示词：
// 设备 Agent 覆盖 → 语义 Agent 覆盖 → 平台默认 → 内置默认。
func (a *API) resolvePromptTemplates(operatorID int64, agentID, semanticAgentID string) (map[string]string, map[string]string) {
	stored, err := a.store.ListPromptTemplates()
	if err != nil {
		log.Printf("[prompt] 读取平台默认失败 operator_id=%d err=%v", operatorID, err)
		stored = map[string]store.PromptTemplate{}
	}
	deviceOverrides := map[string]string{}
	if settings, err := a.store.GetDeviceSettings(operatorID, agentID); err == nil {
		for key, value := range settings {
			if strings.HasPrefix(key, promptTemplateSettingPrefix) {
				deviceOverrides[strings.TrimPrefix(key, promptTemplateSettingPrefix)] = value
			}
		}
	}
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

// syncDeviceAgentPromptTemplates 解析并向指定设备 Agent 推送提示词（重启前 / 控制台保存后调用）。
func (a *API) syncDeviceAgentPromptTemplates(ctx context.Context, operatorID int64, launcherID, agentID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	semanticAgentID := ""
	if loaded, err := a.requestDeviceLauncher(ctx, operatorID, launcherID, fmt.Sprintf("req_prompt_slots_get_%d", time.Now().UnixNano()), "device.launcher.semantic_agent_get", model.DeviceAgentSemanticSelectionPayload{AgentID: agentID}); err != nil {
		log.Printf("[prompt] 读取语义 Agent 绑定失败 agent_id=%s err=%v", agentID, err)
	} else if loaded.SemanticSelection != nil {
		semanticAgentID = loaded.SemanticSelection.SemanticAgentID
	}
	templates, sources := a.resolvePromptTemplates(operatorID, agentID, semanticAgentID)
	revision := promptTemplatesRevision(templates)
	payload := promptTemplatesPayload{AgentID: agentID, Revision: revision, Templates: templates}
	if _, err := a.requestDeviceLauncher(ctx, operatorID, launcherID, fmt.Sprintf("req_prompt_slots_save_%d", time.Now().UnixNano()), "device.launcher.prompt_templates_save", payload); err != nil {
		log.Printf("[prompt] 推送提示词失败 agent_id=%s revision=%s err=%v", agentID, revision, err)
		return
	}
	log.Printf("[prompt] 已推送提示词 agent_id=%s semantic_agent_id=%s revision=%s sources=%v", agentID, semanticAgentID, revision, sources)
}

// pushPromptTemplatesToOnlineDevices 平台默认变更后推送给所有在线设备。
func (a *API) pushPromptTemplatesToOnlineDevices(ctx context.Context) {
	operators, err := a.store.ListOperators()
	if err != nil {
		log.Printf("[prompt] 平台默认推送跳过：读取账号失败 err=%v", err)
		return
	}
	for _, operator := range operators {
		machines, err := a.listDevicesForOperator(ctx, operator.ID)
		if err != nil {
			continue
		}
		for _, machine := range machines {
			launcherID, err := a.deviceLauncherAgent(operator.ID, machine.MachineID)
			if err != nil {
				continue
			}
			for _, agent := range machine.Agents {
				if !agent.Enabled || strings.TrimSpace(agent.ID) == "" {
					continue
				}
				templates, _ := a.resolvePromptTemplates(operator.ID, agent.ID, agent.SemanticAgentID)
				payload := promptTemplatesPayload{
					AgentID:   agent.ID,
					Revision:  promptTemplatesRevision(templates),
					Templates: templates,
				}
				if _, err := a.requestDeviceLauncher(ctx, operator.ID, launcherID, fmt.Sprintf("req_prompt_slots_save_%d", time.Now().UnixNano()), "device.launcher.prompt_templates_save", payload); err != nil {
					log.Printf("[prompt] 平台默认推送失败 machine_id=%s agent_id=%s err=%v", machine.MachineID, agent.ID, err)
				}
			}
		}
	}
}
