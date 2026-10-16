package aiconfig

import (
	"os"
	"strings"
	"testing"
)

// 用开发机上的真实配置验证：打上托管标记后，provider 列表与展示 JSON 都不再出现它。
// 没有真实配置时跳过（CI 环境）。
func TestManagedProviderHiddenAgainstRealConfig(t *testing.T) {
	path := os.Getenv("HOME") + "/.config/opencode/opencode.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("没有真实配置，跳过")
	}
	marked := strings.Replace(string(raw), `"cc-pro": {`, `"cc-pro": {
      "x-chat-codex-managed": "membership",`, 1)
	if marked == string(raw) {
		t.Skip("真实配置里没有 cc-pro")
	}

	info, err := parseInfo(path, marked)
	if err != nil {
		t.Fatalf("parseInfo: %v", err)
	}
	for _, provider := range info.Providers {
		if provider.ID == "cc-pro" {
			t.Fatalf("cc-pro 仍出现在可见列表：%+v", info.Providers)
		}
	}
	if strings.Contains(info.RawJSON, "cc-pro-flash") || strings.Contains(info.PreviewJSON, "cc-pro") {
		t.Fatalf("展示 JSON 仍包含托管 provider：%s", info.RawJSON[:200])
	}
	if len(info.Providers) == 0 {
		t.Fatal("用户自己的 provider 被误删了")
	}
	t.Logf("可见 provider %d 个，cc-pro 已隐藏", len(info.Providers))
}
