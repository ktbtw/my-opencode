package projectmemory

import (
	"context"
	"strings"

	"relay-server/internal/store"
)

const (
	GlobalSettingsAgentID = "__user__"
	EnabledSettingKey     = "project_memory_enabled"
	ModelSettingKey       = "project_memory_model"
	VariantSettingKey     = "project_memory_variant"
	projectSettingsPrefix = "__project_memory_scope__:"
)

func DeviceSettingsAgentID(machineID string) string {
	return strings.TrimSpace(machineID)
}

// Settings contains both explicit values and the values that will be used by
// the runtime after applying project-over-global inheritance.
type Settings struct {
	GlobalEnabled    bool
	GlobalEnabledSet bool
	GlobalModel      string
	GlobalVariant    string
	ProjectEnabled   *bool
	ProjectModel     string
	ProjectVariant   string
	Enabled          bool
	Model            string
	Variant          string
	EnabledSource    string
	ModelSource      string
}

func ProjectSettingsAgentID(scopeID string) string {
	return projectSettingsPrefix + strings.TrimSpace(scopeID)
}

func readSettings(ctx context.Context, memoryStore *store.Memory, operatorID int64, agentID string) map[string]string {
	_ = ctx
	if memoryStore == nil || operatorID == 0 || strings.TrimSpace(agentID) == "" {
		return nil
	}
	settings, err := memoryStore.GetDeviceSettings(operatorID, agentID)
	if err != nil {
		return nil
	}
	return settings
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// Resolve applies the inheritance contract shared by API, task injection and
// background organization:
// project explicit value > global explicit value > default.
func Resolve(ctx context.Context, memoryStore *store.Memory, operatorID int64, machineID, scopeID string) Settings {
	global := readSettings(ctx, memoryStore, operatorID, GlobalSettingsAgentID)
	device := readSettings(ctx, memoryStore, operatorID, DeviceSettingsAgentID(machineID))
	project := readSettings(ctx, memoryStore, operatorID, ProjectSettingsAgentID(scopeID))

	globalEnabled, globalEnabledSet := parseBool(global[EnabledSettingKey])
	if !globalEnabledSet {
		globalEnabled = true
	}
	settings := Settings{
		GlobalEnabled:    globalEnabled,
		GlobalEnabledSet: globalEnabledSet,
		GlobalModel:      strings.TrimSpace(device[ModelSettingKey]),
		GlobalVariant:    strings.TrimSpace(device[VariantSettingKey]),
		Enabled:          globalEnabled,
		Model:            strings.TrimSpace(device[ModelSettingKey]),
		Variant:          strings.TrimSpace(device[VariantSettingKey]),
		EnabledSource:    "default",
		ModelSource:      "agent_default",
	}
	if globalEnabledSet {
		settings.EnabledSource = "global"
	}
	if settings.Model != "" {
		settings.ModelSource = "global"
	}

	if project != nil {
		if value, ok := parseBool(project[EnabledSettingKey]); ok {
			settings.ProjectEnabled = &value
			settings.Enabled = value
			settings.EnabledSource = "project"
		}
		settings.ProjectModel = strings.TrimSpace(project[ModelSettingKey])
		settings.ProjectVariant = strings.TrimSpace(project[VariantSettingKey])
		if settings.ProjectModel != "" {
			settings.Model = settings.ProjectModel
			settings.Variant = settings.ProjectVariant
			settings.ModelSource = "project"
		}
	}
	return settings
}
