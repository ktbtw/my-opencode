package projectmemory

import (
	"context"
	"testing"

	"relay-server/internal/store"
)

func TestResolveProjectMemorySettingsInheritance(t *testing.T) {
	ctx := context.Background()
	memory := store.NewMemory(nil)
	set := func(agentID, key, value string) {
		t.Helper()
		if err := memory.SetDeviceSetting(7, agentID, key, value); err != nil {
			t.Fatal(err)
		}
	}

	defaults := Resolve(ctx, memory, 7, "machine", "scope")
	if !defaults.Enabled || defaults.EnabledSource != "default" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	set(GlobalSettingsAgentID, EnabledSettingKey, "true")
	set(DeviceSettingsAgentID("machine"), ModelSettingKey, "provider/global")
	set(DeviceSettingsAgentID("machine"), VariantSettingKey, "high")
	inherited := Resolve(ctx, memory, 7, "machine", "scope")
	if !inherited.Enabled || inherited.Model != "provider/global" || inherited.Variant != "high" ||
		inherited.EnabledSource != "global" || inherited.ModelSource != "global" {
		t.Fatalf("unexpected inherited settings: %+v", inherited)
	}

	projectAgent := ProjectSettingsAgentID("scope")
	set(projectAgent, EnabledSettingKey, "false")
	set(projectAgent, ModelSettingKey, "provider/project")
	overridden := Resolve(ctx, memory, 7, "machine", "scope")
	if overridden.Enabled || overridden.EnabledSource != "project" ||
		overridden.Model != "provider/project" || overridden.ModelSource != "project" || overridden.Variant != "" {
		t.Fatalf("unexpected project override: %+v", overridden)
	}
}
