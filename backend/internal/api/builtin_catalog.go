package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"relay-server/internal/model"
)

const builtinSemanticCatalogVersion = 4

//go:embed builtin/catalog_v4.json
var builtinSemanticCatalogJSON []byte

type builtinSemanticCatalog struct {
	Version          int                          `json:"version"`
	Skills           []model.Skill                `json:"skills"`
	MCPCatalog       []model.MCPCatalogItem       `json:"mcp_catalog"`
	RuntimeCatalog   []model.RuntimeCatalogItem   `json:"runtime_catalog"`
	RuntimeVersions  []model.RuntimeVersion       `json:"runtime_versions"`
	RuntimeArtifacts []model.RuntimeArtifact      `json:"runtime_artifacts"`
	RuntimeMirrors   []model.RuntimeMirror        `json:"runtime_mirrors"`
	SemanticAgents   []model.SemanticAgentProfile `json:"semantic_agents"`
}

var (
	builtinCatalogOnce sync.Once
	builtinCatalogData builtinSemanticCatalog
	builtinCatalogErr  error
)

func loadBuiltinSemanticCatalog() (*builtinSemanticCatalog, error) {
	builtinCatalogOnce.Do(func() {
		if err := json.Unmarshal(builtinSemanticCatalogJSON, &builtinCatalogData); err != nil {
			builtinCatalogErr = fmt.Errorf("解析内置 Agent 能力目录失败: %w", err)
			return
		}
		if builtinCatalogData.Version != builtinSemanticCatalogVersion {
			builtinCatalogErr = fmt.Errorf(
				"内置 Agent 能力目录版本不匹配: got=%d want=%d",
				builtinCatalogData.Version,
				builtinSemanticCatalogVersion,
			)
		}
	})
	if builtinCatalogErr != nil {
		return nil, builtinCatalogErr
	}
	return &builtinCatalogData, nil
}

func mustBuiltinSemanticCatalog() *builtinSemanticCatalog {
	catalog, err := loadBuiltinSemanticCatalog()
	if err != nil {
		panic(err)
	}
	return catalog
}

func builtinSkills() []model.Skill {
	return append([]model.Skill(nil), mustBuiltinSemanticCatalog().Skills...)
}

func builtinMCPCatalogItems() []model.MCPCatalogItem {
	return append([]model.MCPCatalogItem(nil), mustBuiltinSemanticCatalog().MCPCatalog...)
}

func builtinRuntimeCatalogItems() []model.RuntimeCatalogItem {
	return append([]model.RuntimeCatalogItem(nil), mustBuiltinSemanticCatalog().RuntimeCatalog...)
}

func builtinRuntimeVersions() []model.RuntimeVersion {
	return append([]model.RuntimeVersion(nil), mustBuiltinSemanticCatalog().RuntimeVersions...)
}

func builtinRuntimeArtifacts() []model.RuntimeArtifact {
	return append([]model.RuntimeArtifact(nil), mustBuiltinSemanticCatalog().RuntimeArtifacts...)
}

func builtinRuntimeMirrors() []model.RuntimeMirror {
	return append([]model.RuntimeMirror(nil), mustBuiltinSemanticCatalog().RuntimeMirrors...)
}

func builtinSemanticAgentProfiles() []model.SemanticAgentProfile {
	return append([]model.SemanticAgentProfile(nil), mustBuiltinSemanticCatalog().SemanticAgents...)
}
