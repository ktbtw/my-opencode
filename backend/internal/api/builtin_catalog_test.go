package api

import (
	"strings"
	"testing"
)

func TestBuiltinVerifySkillsContainRoutedReferences(t *testing.T) {
	tests := []struct {
		id                  string
		descriptionFragment string
		minimumFiles        int
		requiredPaths       []string
	}{
		{
			id:                  "verify-framework",
			descriptionFragment: "NPatch-based local Patch runtime",
			minimumFiles:        16,
			requiredPaths: []string{
				"agents/openai.yaml",
				"references/dex-cache-runtime.md",
				"references/inspect-patched-apk.md",
				"references/local-patch-runtime.md",
				"references/mcp-tools.md",
				"references/patch-local-apk.md",
				"references/runtime-crash-analysis.md",
				"references/wifi-adb-root.md",
			},
		},
		{
			id:                  "verify-shadowhook",
			descriptionFragment: "package ShadowHook resource zips",
			minimumFiles:        6,
			requiredPaths: []string{
				"agents/openai.yaml",
				"references/shadowhook-install.md",
				"references/shadowhook-java-runtime.md",
				"references/shadowhook-native.md",
				"references/shadowhook-troubleshooting.md",
			},
		},
	}

	byID := make(map[string]struct {
		description string
		content     string
		files       map[string]string
	})
	for _, skill := range builtinSkills() {
		files := make(map[string]string, len(skill.PackageFiles))
		for _, file := range skill.PackageFiles {
			files[file.Path] = file.Content
		}
		byID[skill.ID] = struct {
			description string
			content     string
			files       map[string]string
		}{skill.Description, skill.Content, files}
	}

	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			got, ok := byID[test.id]
			if !ok {
				t.Fatalf("missing built-in skill %q", test.id)
			}
			if !strings.Contains(got.description, test.descriptionFragment) {
				t.Fatalf("description is stale: %q", got.description)
			}
			if !strings.Contains(got.content, "## Reference Routing") {
				t.Fatal("skill entry is missing reference routing")
			}
			if len(got.files) < test.minimumFiles {
				t.Fatalf("expected at least %d package files, got %d", test.minimumFiles, len(got.files))
			}
			for _, path := range test.requiredPaths {
				if strings.TrimSpace(got.files[path]) == "" {
					t.Errorf("missing or empty package file %q", path)
				}
			}
		})
	}
}

func TestBuiltinAndroidAgentRequiresVerifiedNativeHookWorkflow(t *testing.T) {
	catalog := mustBuiltinSemanticCatalog()
	var agentPrompt, apkSkill string
	for _, agent := range catalog.SemanticAgents {
		if agent.ID == "reverse-android" {
			agentPrompt = agent.Prompt
			break
		}
	}
	for _, skill := range catalog.Skills {
		if skill.ID == "apk-reverse" {
			apkSkill = skill.Content
			break
		}
	}
	for label, content := range map[string]string{"agent prompt": agentPrompt, "apk skill": apkSkill} {
		if strings.TrimSpace(content) == "" {
			t.Fatalf("missing %s", label)
		}
		for _, required := range []string{
			"IDA MCP",
			"full auto-analysis",
			"60 minutes",
			"function boundaries",
			"hot-update",
			"native hook callback",
		} {
			if label == "apk skill" {
				required = map[string]string{
					"full auto-analysis":   "全量自动分析",
					"function boundaries":  "函数边界",
					"hot-update":           "热更新",
					"native hook callback": "native callback",
				}[required]
				if required == "" {
					continue
				}
			}
			if !strings.Contains(content, required) {
				t.Errorf("%s is missing %q", label, required)
			}
		}
	}
}
