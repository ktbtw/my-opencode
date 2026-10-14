package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"launcher/internal/fileutil"
	"launcher/internal/model"
)

const promptTemplatesManifest = "manifest.json"

// promptTemplatesDir 是提示词槽位文件的落盘目录：<RuntimeDir>/prompts/<agent_id>/。
func promptTemplatesDir(runtimeDir, agentID string) string {
	return filepath.Join(runtimeDir, "prompts", strings.TrimSpace(agentID))
}

type promptTemplatesManifestFile struct {
	Revision string   `json:"revision"`
	Slots    []string `json:"slots"`
}

// SaveAgentPromptTemplates 把控制台解析后的提示词槽位原子写入本地文件。
// 空内容表示该槽位回落内置默认：删除对应文件，避免把空文本喂给模型。
func (s *service) SaveAgentPromptTemplates(input model.DeviceAgentPromptTemplatesInput) (string, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return "", fmt.Errorf("agent_id 不能为空")
	}
	dir := promptTemplatesDir(s.cfg.RuntimeDir, agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	slots := make([]string, 0, len(input.Templates))
	for slotID, content := range input.Templates {
		slotID = strings.TrimSpace(slotID)
		if slotID == "" || strings.ContainsAny(slotID, `/\`) {
			continue
		}
		slots = append(slots, slotID)
		path := filepath.Join(dir, slotID+".txt")
		if strings.TrimSpace(content) == "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return "", err
			}
			continue
		}
		if err := fileutil.AtomicWriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	sort.Strings(slots)
	revision := strings.TrimSpace(input.Revision)
	manifest, err := json.Marshal(promptTemplatesManifestFile{Revision: revision, Slots: slots})
	if err != nil {
		return "", err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(dir, promptTemplatesManifest), manifest, 0o644); err != nil {
		return "", err
	}
	return revision, nil
}

// PromptSlotPaths 返回该 Agent 需要声明到 opencode 配置里的槽位文件映射（slot_id → 绝对路径）。
// 槽位清单来自最近一次下发（manifest.json）：即使文件暂时为空（回落内置默认），
// 也先把路径声明好，这样后续推送的内容无需重启 Agent 即可生效。
func (s *service) PromptSlotPaths(agentID string) map[string]string {
	dir := promptTemplatesDir(s.cfg.RuntimeDir, agentID)
	raw, err := os.ReadFile(filepath.Join(dir, promptTemplatesManifest))
	if err != nil {
		return nil
	}
	var manifest promptTemplatesManifestFile
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil
	}
	out := map[string]string{}
	for _, slotID := range manifest.Slots {
		slotID = strings.TrimSpace(slotID)
		if slotID == "" {
			continue
		}
		out[slotID] = filepath.Join(dir, slotID+".txt")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
