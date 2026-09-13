package mcpconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"launcher/internal/model"
)

func TestLoadParsesRemoteAndLocalServers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
	  "mcp": {
	    "docs": {
	      "type": "remote",
	      "url": "https://docs.example.com/mcp",
	      "headers": {
	        "X-Token": "secret-value"
	      }
	    },
	    "files": {
	      "type": "local",
	      "command": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
	      "environment": {
	        "ROOT": "/tmp/project"
	      },
	      "enabled": false
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(info.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %+v", info.Servers)
	}
	if info.Servers[0].Name != "docs" || info.Servers[0].Type != "remote" {
		t.Fatalf("unexpected first server: %+v", info.Servers[0])
	}
	if !strings.Contains(info.PreviewJSON, `"X-Token": "secr****alue"`) {
		t.Fatalf("expected preview to mask token header, got %s", info.PreviewJSON)
	}
	if info.Servers[1].Enabled {
		t.Fatalf("expected local server disabled, got %+v", info.Servers[1])
	}
}

func TestSaveRoundTripsLifecycleTimeoutsAndAsyncTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	info, err := Save(model.DeviceMCPConfigInfo{
		Servers: []model.DeviceMCPServerInfo{{
			Name:             "idalib-mcp",
			Type:             "local",
			Enabled:          true,
			Timeout:          30_000,
			ConnectTimeout:   31_000,
			DiscoveryTimeout: 32_000,
			ToolTimeout:      3_600_000,
			AsyncTools:       []string{"idb_open", "", " idb_open_many "},
			Command:          []string{"uvx", "idalib-mcp"},
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(info.Servers) != 1 {
		t.Fatalf("expected one server, got %+v", info.Servers)
	}
	server := info.Servers[0]
	if server.ConnectTimeout != 31_000 || server.DiscoveryTimeout != 32_000 || server.ToolTimeout != 3_600_000 {
		t.Fatalf("unexpected lifecycle timeouts: %+v", server)
	}
	if got := strings.Join(server.AsyncTools, ","); got != "idb_open,idb_open_many" {
		t.Fatalf("unexpected async tools: %q", got)
	}
	for _, want := range []string{`"connect_timeout": 31000`, `"discovery_timeout": 32000`, `"tool_timeout": 3600000`, `"async_tools"`} {
		if !strings.Contains(info.RawJSON, want) {
			t.Fatalf("expected raw config to contain %s, got %s", want, info.RawJSON)
		}
	}
}

func TestSavePreservesExistingOAuthSecretWhenInputIsBlank(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
	  "mcp": {
	    "jira": {
	      "type": "remote",
	      "url": "https://jira.example.com/mcp",
	      "oauth": {
	        "clientId": "jira-client",
	        "clientSecret": "jira-secret",
	        "scope": "read"
	      }
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Save(model.DeviceMCPConfigInfo{
		Servers: []model.DeviceMCPServerInfo{{
			Name:              "jira",
			Type:              "remote",
			Enabled:           true,
			URL:               "https://jira.example.com/mcp",
			OAuthMode:         "custom",
			OAuthClientID:     "jira-client-next",
			OAuthClientSecret: "",
			OAuthScope:        "read write",
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(info.Servers) != 1 {
		t.Fatalf("expected 1 server, got %+v", info.Servers)
	}
	if info.Servers[0].OAuthClientSecretMasked == "" {
		t.Fatalf("expected secret mask to remain, got %+v", info.Servers[0])
	}
	if !strings.Contains(info.RawJSON, `"clientSecret": "jira-secret"`) {
		t.Fatalf("expected raw config to preserve secret, got %s", info.RawJSON)
	}
}

func TestSavePreservesExistingCredentialEnvironmentWhenInputUsesPlaceholders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
	  "mcp": {
	    "verify": {
	      "type": "local",
	      "command": ["npx", "-y", "@ktbtw/verify-mcp"],
	      "environment": {
	        "VERIFY_API_TOKEN": "vat_real_token",
	        "VERIFY_PROTECT_TOKEN": "vpt_real_token",
	        "VERIFY_BASE_URL": "https://old.example.com/verify"
	      }
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Save(model.DeviceMCPConfigInfo{
		Servers: []model.DeviceMCPServerInfo{{
			Name:    "verify",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
			Environment: map[string]string{
				"VERIFY_API_TOKEN":     "vat_xxx_replace_me",
				"VERIFY_PROTECT_TOKEN": "vpt_xxx_replace_me",
				"VERIFY_BASE_URL":      "https://new.example.com/verify",
			},
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(info.Servers) != 1 {
		t.Fatalf("expected one server, got %+v", info.Servers)
	}
	env := info.Servers[0].Environment
	if env["VERIFY_API_TOKEN"] != "vat_real_token" || env["VERIFY_PROTECT_TOKEN"] != "vpt_real_token" {
		t.Fatalf("expected real credential values preserved, got %+v", env)
	}
	if env["VERIFY_BASE_URL"] != "https://new.example.com/verify" {
		t.Fatalf("expected non-credential environment updated, got %+v", env)
	}
}

func TestSaveDropsCredentialPlaceholdersWithoutExistingValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	info, err := Save(model.DeviceMCPConfigInfo{
		Servers: []model.DeviceMCPServerInfo{{
			Name:    "verify",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
			Environment: map[string]string{
				"VERIFY_API_TOKEN":     "vat_xxx_replace_me",
				"VERIFY_PROTECT_TOKEN": "vpt_xxx",
				"VERIFY_BASE_URL":      "https://verify.example.com",
			},
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	env := info.Servers[0].Environment
	if _, ok := env["VERIFY_API_TOKEN"]; ok {
		t.Fatalf("expected API token placeholder removed, got %+v", env)
	}
	if _, ok := env["VERIFY_PROTECT_TOKEN"]; ok {
		t.Fatalf("expected protect token placeholder removed, got %+v", env)
	}
	if env["VERIFY_BASE_URL"] != "https://verify.example.com" {
		t.Fatalf("expected non-credential environment preserved, got %+v", env)
	}
}

func TestEnvironmentWithAgentCredentialOverrides(t *testing.T) {
	env, changed := EnvironmentWithAgentCredentialOverrides(
		map[string]string{
			"VERIFY_BASE_URL":  "https://verify.example.com",
			"VERIFY_API_TOKEN": "vat_shared_token",
		},
		map[string]string{"VERIFY_API_TOKEN": "vat_agent_token"},
	)
	if !changed {
		t.Fatal("expected credential override")
	}
	if env["VERIFY_API_TOKEN"] != "vat_agent_token" || env["VERIFY_BASE_URL"] != "https://verify.example.com" {
		t.Fatalf("unexpected merged environment: %+v", env)
	}

	if env, changed := EnvironmentWithAgentCredentialOverrides(
		map[string]string{"VERIFY_API_TOKEN": "vat_shared_token"},
		map[string]string{"VERIFY_API_TOKEN": "vat_xxx_replace_me"},
	); changed || env != nil {
		t.Fatalf("placeholder must not replace a shared credential: %+v", env)
	}
}

func TestSavePreservesExistingServersMissingFromInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
	  "mcp": {
	    "verify": {
	      "type": "local",
	      "command": ["npx", "-y", "@ktbtw/verify-mcp"],
	      "environment": {"VERIFY_API_TOKEN": "vat_real_token"}
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Save(model.DeviceMCPConfigInfo{Servers: []model.DeviceMCPServerInfo{{
		Name:    "playwright",
		Type:    "local",
		Enabled: true,
		Command: []string{"npx", "-y", "@executeautomation/playwright-mcp-server"},
	}}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(info.Servers) != 2 {
		t.Fatalf("expected existing and incoming servers, got %+v", info.Servers)
	}
	if info.Servers[1].Name != "verify" || info.Servers[1].Environment["VERIFY_API_TOKEN"] != "vat_real_token" {
		t.Fatalf("expected verify server and token preserved, got %+v", info.Servers)
	}
}

func TestSaveRejectsUnresolvedAbsolutePathPlaceholder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := Save(model.DeviceMCPConfigInfo{
		Servers: []model.DeviceMCPServerInfo{{
			Name:    "frida-analykit",
			Type:    "local",
			Enabled: true,
			Command: []string{"frida-analykit-mcp", "--config", "/ABSOLUTE/PATH/frida-analykit/mcp.toml"},
		}},
	})
	if err == nil {
		t.Fatal("expected unresolved placeholder error")
	}
	if !strings.Contains(err.Error(), "/ABSOLUTE/PATH") {
		t.Fatalf("expected placeholder in error, got %v", err)
	}
}
