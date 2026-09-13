package api

import "strings"

const (
	defaultSeedSettingsAgentID = "__admin_default_seed__"

	defaultSeedEnvironmentPresetsKey = "env_presets"
	defaultSeedSkillsKey             = "skills_v6"
	defaultSeedMCPCatalogKey         = "mcp_catalog_v5"
	defaultSeedRuntimeCatalogKey     = "runtime_catalog_v6"
	defaultSeedSemanticAgentsKey     = "semantic_agents_v5"
	defaultSeedToolCatalogKey        = "tool_catalog"
)

func (a *API) defaultSeedInitialized(key string) (bool, error) {
	settings, err := a.store.GetDeviceSettings(0, defaultSeedSettingsAgentID)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(settings[key]), "true"), nil
}

func (a *API) markDefaultSeedInitialized(key string) error {
	return a.store.SetDeviceSetting(0, defaultSeedSettingsAgentID, key, "true")
}
