package aiconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"launcher/internal/model"
)

// 会员 provider 必须写进运行时私有覆盖文件，用户可见的 opencode.json 里不能出现。
func TestManagedProviderWrittenToOverlayOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	overlay := filepath.Join(dir, "membership.json")
	t.Setenv("CHAT_CODEX_MEMBERSHIP_CONFIG", overlay)

	// 先放一份用户自己的配置
	userPath, _, _, err := readRaw()
	if err != nil {
		t.Fatalf("readRaw: %v", err)
	}
	seed := map[string]any{
		"provider": map[string]any{
			"my-own": map[string]any{"options": map[string]any{"apiKey": "sk-user"}},
		},
		"model": "my-own/gpt-x",
	}
	raw, err := marshalConfig(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRaw(userPath, raw); err != nil {
		t.Fatalf("writeRaw: %v", err)
	}

	// 下发会员 provider（托管）
	_, err = Save(model.DeviceAIConfigInfo{
		Provider:     "cc-pro",
		Managed:      true,
		BaseURL:      "http://114.66.31.184:3050/v1",
		APIKeyMasked: "sk-member-key",
		APIMode:      "chat",
		Models:       []model.DeviceAIModelInfo{{ID: "cc-pro-flash", Name: "高级专线 · DeepSeek Flash"}},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	overlayRaw, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("覆盖文件未生成: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(overlayRaw, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := asMap(parsed["provider"])["cc-pro"]; !ok {
		t.Fatalf("会员 provider 应写进覆盖文件：%s", string(overlayRaw))
	}

	userRaw, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	var userConfig map[string]any
	if err := json.Unmarshal(userRaw, &userConfig); err != nil {
		t.Fatal(err)
	}
	if _, ok := asMap(userConfig["provider"])["cc-pro"]; ok {
		t.Fatalf("用户可见配置里不应出现会员 provider：%s", string(userRaw))
	}
	if _, ok := asMap(userConfig["provider"])["my-own"]; !ok {
		t.Fatal("用户自己的 provider 被弄丢了")
	}

	// 会员到期：清除后覆盖文件里也不应再有它
	if _, err := ClearProvider("cc-pro"); err != nil {
		t.Fatalf("ClearProvider: %v", err)
	}
	after, err := os.ReadFile(overlay)
	if err == nil && len(after) > 0 {
		var parsedAfter map[string]any
		_ = json.Unmarshal(after, &parsedAfter)
		if _, ok := asMap(parsedAfter["provider"])["cc-pro"]; ok {
			t.Fatalf("清除后覆盖文件仍含会员 provider：%s", string(after))
		}
	}
}
