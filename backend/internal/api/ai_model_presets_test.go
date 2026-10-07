package api

import (
	"encoding/json"
	"relay-server/internal/model"
	"testing"
)

func TestNormalizeDeviceAIModelsPreservesOpenCodePresetSource(t *testing.T) {
	items := normalizeDeviceAIModels("relay", []model.DeviceAIModel{{
		ID: "gpt-5.4", PresetContext: 1050000, PresetOutput: 128000,
		PresetSource: "opencode", PresetModel: "openai/gpt-5.4",
		ModalitiesSource: "opencode",
	}})
	if items[0].Context != 1050000 || items[0].Output != 128000 || items[0].ManualContext != 0 {
		t.Fatalf("preset values lost or promoted: %+v", items)
	}
	data, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if json.Unmarshal(data, &wire) != nil {
		t.Fatal("bad wire format")
	}
	if wire["preset_model"] != "openai/gpt-5.4" || wire["preset_context_limit"] != float64(1050000) {
		t.Fatalf("source markers dropped: %s", data)
	}
	if wire["modalities_source"] != "opencode" {
		t.Fatalf("automatic modalities source dropped: %s", data)
	}
	items[0].ManualContext = 200000
	items[0].ManualOutput = 4096
	got := normalizeDeviceAIModels("relay", items)[0]
	if got.Context != 200000 || got.Output != 4096 {
		t.Fatalf("manual values did not override preset: %+v", got)
	}
}
