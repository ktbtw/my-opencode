package aiconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

// 会员 provider：读的时候要隐藏（列表 + 展示 JSON），写的时候要保留。
func TestManagedProviderHiddenOnReadAndPreservedOnWrite(t *testing.T) {
	raw := `{
	  "provider": {
	    "cc-pro": {
	      "x-chat-codex-managed": "membership",
	      "options": {"apiKey": "sk-member-key", "baseURL": "http://114.66.31.184:3050/v1"},
	      "models": {"cc-pro-flash": {"name": "高级专线 · DeepSeek Flash"}}
	    },
	    "my-own": {
	      "options": {"apiKey": "sk-user-key", "baseURL": "https://api.example.com/v1"},
	      "models": {"gpt-x": {"name": "自己配的"}}
	    }
	  },
	  "model": "my-own/gpt-x"
	}`

	info, err := parseInfo("/tmp/opencode.json", raw)
	if err != nil {
		t.Fatalf("parseInfo: %v", err)
	}
	for _, provider := range info.Providers {
		if provider.ID == "cc-pro" {
			t.Fatal("托管的会员 provider 不应出现在 provider 列表里")
		}
	}
	if len(info.Providers) != 1 || info.Providers[0].ID != "my-own" {
		t.Fatalf("应只剩用户自己的 provider：%+v", info.Providers)
	}
	if strings.Contains(info.RawJSON, "cc-pro") || strings.Contains(info.PreviewJSON, "cc-pro") {
		t.Fatalf("展示用 JSON 不应包含托管 provider：%s", info.RawJSON)
	}
	if strings.Contains(info.RawJSON, "sk-member-key") {
		t.Fatalf("展示用 JSON 不应包含会员密钥：%s", info.RawJSON)
	}
	if !strings.Contains(info.RawJSON, "my-own") {
		t.Fatalf("用户自己的 provider 必须保留：%s", info.RawJSON)
	}

	// 保存用户编辑后的 JSON（里面没有托管 provider）→ 必须把托管 provider 合回去
	var current, incoming map[string]any
	if err := json.Unmarshal([]byte(raw), &current); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"provider":{"my-own":{"options":{"apiKey":"sk-new"}}},"model":"my-own/gpt-x"}`), &incoming); err != nil {
		t.Fatal(err)
	}
	restoreManagedProviders(incoming, current)

	providers := asMap(incoming["provider"])
	if _, ok := providers["cc-pro"]; !ok {
		t.Fatal("保存后托管 provider 被冲掉了")
	}
	if got := asMap(providers["cc-pro"])[managedProviderKey]; got != managedProviderValue {
		t.Fatalf("托管标记应保留：%v", got)
	}
	if asMap(asMap(providers["my-own"])["options"])["apiKey"] != strings.TrimSpace("sk-new") {
		t.Fatal("用户自己的改动应保留")
	}
}

// 没有托管 provider 时不能误伤：JSON 原样返回。
func TestStripManagedProvidersNoopWithoutMarker(t *testing.T) {
	raw := `{"provider":{"relay":{"models":{"a":{"name":"A"}}}}}`
	if got := stripManagedProviders(raw, managedProviderIDs(map[string]any{})); got != raw {
		t.Fatalf("无托管 provider 时不应改写：%s", got)
	}
}

// 平台下发的供应商名要写进 provider.name（原来是空则回退成 provider id）。
func TestProviderNameFromPlatform(t *testing.T) {
	parsed := applyConfigInput(map[string]any{}, "cc-pro", "http://gw/v1", "", "sk-key", "chat", "", "会员专属", nil, true, false, true)
	block := asMap(asMap(parsed["provider"])["cc-pro"])
	if got := strings.TrimSpace(asString(block["name"])); got != "会员专属" {
		t.Fatalf("供应商名应为 会员专属，实际 %q", got)
	}

	// 没传名字时保持原样（空则回退成 id）
	parsed = applyConfigInput(map[string]any{}, "cc-pro", "http://gw/v1", "", "sk-key", "chat", "", "", nil, true, false, true)
	block = asMap(asMap(parsed["provider"])["cc-pro"])
	if got := asString(block["name"]); got != "cc-pro" {
		t.Fatalf("未传名字时应回退成 id，实际 %q", got)
	}

	// 已存在自定义名字且平台没传 → 不要被覆盖
	seed := map[string]any{"provider": map[string]any{"cc-pro": map[string]any{"name": "我自己起的"}}}
	parsed = applyConfigInput(seed, "cc-pro", "http://gw/v1", "", "sk-key", "chat", "", "", nil, true, false, true)
	block = asMap(asMap(parsed["provider"])["cc-pro"])
	if got := asString(block["name"]); got != "我自己起的" {
		t.Fatalf("不应覆盖已有名字，实际 %q", got)
	}
}

// 迁移场景：老启动器写的托管块没有标记，按平台给的 provider id 也要能清掉。
func TestDropManagedBlockByProviderIDWithoutMarker(t *testing.T) {
	parsed := map[string]any{"provider": map[string]any{
		"cc-pro": map[string]any{"name": "cc-pro"},
		"my-own": map[string]any{"name": "自己用的"},
	}}
	out, changed := dropManagedBlocksFromUserConfig(parsed, "cc-pro")
	if !changed {
		t.Fatal("应识别出需要清理")
	}
	providers := asMap(out["provider"])
	if _, ok := providers["cc-pro"]; ok {
		t.Fatal("没有标记但平台指明托管的 provider 也应被清掉")
	}
	if _, ok := providers["my-own"]; !ok {
		t.Fatal("用户自己的 provider 不能被误删")
	}
	// 传了不存在的 id 且没有标记 → 不该动文件
	if _, changed := dropManagedBlocksFromUserConfig(map[string]any{"provider": map[string]any{"x": map[string]any{}}}, "cc-pro"); changed {
		t.Fatal("没有可清理项时不应改动配置")
	}
}
