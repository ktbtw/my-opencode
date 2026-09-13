package app

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"launcher/internal/model"
)

const builtinSemanticCatalogVersion = 4

// This file is synchronized from backend/internal/api/builtin/catalog_v4.json
// by launcher/script/sync-builtin-catalog.sh before release builds.
//
// Keep the security-oriented skill text compressed so raw tool signatures are
// not copied into the Windows PE image and mistaken for executable payloads.
//
//go:embed builtin/catalog_v4.json.gz
var builtinSemanticCatalogGZIP []byte

type builtinSemanticCatalog struct {
	Version        int                          `json:"version"`
	Skills         []builtinSemanticSkill       `json:"skills"`
	MCPCatalog     []builtinSemanticMCP         `json:"mcp_catalog"`
	SemanticAgents []model.SemanticAgentProfile `json:"semantic_agents"`
}

type builtinSemanticSkill struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Description  string                   `json:"description,omitempty"`
	Content      string                   `json:"content,omitempty"`
	PackageFiles []model.SkillPackageFile `json:"package_files,omitempty"`
	Enabled      bool                     `json:"enabled"`
}

type builtinSemanticMCP struct {
	ID                string                    `json:"id"`
	Name              string                    `json:"name"`
	Title             string                    `json:"title,omitempty"`
	Type              string                    `json:"type,omitempty"`
	SourceURL         string                    `json:"source_url,omitempty"`
	Config            model.DeviceMCPServerInfo `json:"config"`
	Install           model.MCPInstallInfo      `json:"install,omitempty"`
	LaunchReady       bool                      `json:"launch_ready"`
	LaunchBlockReason string                    `json:"launch_block_reason,omitempty"`
	Enabled           bool                      `json:"enabled"`
}

var semanticAgentDefinitions = mustBuiltinSemanticAgentDefinitions()

func mustBuiltinSemanticAgentDefinitions() []semanticAgentDefinition {
	catalogJSON, err := readBuiltinSemanticCatalog()
	if err != nil {
		panic(fmt.Errorf("解压 Launcher 内置 Agent 能力目录失败: %w", err))
	}
	var catalog builtinSemanticCatalog
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		panic(fmt.Errorf("解析 Launcher 内置 Agent 能力目录失败: %w", err))
	}
	if catalog.Version != builtinSemanticCatalogVersion {
		panic(fmt.Errorf(
			"Launcher 内置 Agent 能力目录版本不匹配: got=%d want=%d",
			catalog.Version,
			builtinSemanticCatalogVersion,
		))
	}

	skills := make(map[string]builtinSemanticSkill, len(catalog.Skills)*2)
	for _, skill := range catalog.Skills {
		if !skill.Enabled {
			continue
		}
		skills[strings.TrimSpace(skill.ID)] = skill
		if name := strings.TrimSpace(skill.Name); name != "" {
			if _, exists := skills[name]; !exists {
				skills[name] = skill
			}
		}
	}
	mcps := make(map[string]builtinSemanticMCP, len(catalog.MCPCatalog)*2)
	for _, mcp := range catalog.MCPCatalog {
		if !mcp.Enabled {
			continue
		}
		mcps[strings.TrimSpace(mcp.ID)] = mcp
		if name := strings.TrimSpace(mcp.Name); name != "" {
			if _, exists := mcps[name]; !exists {
				mcps[name] = mcp
			}
		}
	}

	out := make([]semanticAgentDefinition, 0, len(catalog.SemanticAgents))
	for _, profile := range catalog.SemanticAgents {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			continue
		}
		item := semanticAgentDefinition{
			profile:      profile,
			opencodeName: firstNonEmpty(strings.TrimSpace(profile.OpencodeName), profile.ID),
			prompt:       strings.TrimSpace(profile.Prompt),
			permission:   cloneAnyMap(profile.ToolPermissions),
		}
		profile.Skills = nil
		profile.SkillDefinitions = nil
		for _, id := range profile.SkillIDs {
			skill, ok := skills[strings.TrimSpace(id)]
			if !ok {
				continue
			}
			name := firstNonEmpty(strings.TrimSpace(skill.ID), strings.TrimSpace(skill.Name))
			profile.Skills = append(profile.Skills, name)
			item.skills = append(item.skills, semanticAgentSkill{
				name:         name,
				description:  strings.TrimSpace(skill.Description),
				content:      strings.TrimSpace(skill.Content),
				packageFiles: cleanSkillPackageFiles(skill.PackageFiles),
			})
		}
		profile.RecommendedMCPServers = nil
		profile.RecommendedMCPConfigs = nil
		profile.MCPDependencies = nil
		for _, id := range profile.MCPIDs {
			mcp, ok := mcps[strings.TrimSpace(id)]
			if !ok {
				continue
			}
			profile.RecommendedMCPServers = append(profile.RecommendedMCPServers, mcp.Name)
			profile.MCPDependencies = append(profile.MCPDependencies, model.SemanticAgentMCPDependency{
				ID:                mcp.ID,
				Name:              mcp.Name,
				Title:             mcp.Title,
				Type:              mcp.Type,
				SourceURL:         mcp.SourceURL,
				Config:            mcp.Config,
				Install:           mcp.Install,
				LaunchReady:       mcp.LaunchReady,
				LaunchBlockReason: mcp.LaunchBlockReason,
			})
			if mcp.LaunchReady {
				profile.RecommendedMCPConfigs = append(profile.RecommendedMCPConfigs, mcp.Config)
			}
		}
		item.profile = profile
		out = append(out, item)
	}
	if len(out) == 0 {
		panic("Launcher 内置 Agent 能力目录为空")
	}
	return out
}

func readBuiltinSemanticCatalog() ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(builtinSemanticCatalogGZIP))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
