package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"launcher/internal/fileutil"
	"launcher/internal/model"
)

const envConfigFileName = "env.json"

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type envConfigFile struct {
	Global map[string]string `json:"global_environment,omitempty"`
}

func loadEnvConfig(runtimeDir string) (map[string]string, error) {
	path := filepath.Join(runtimeDir, envConfigFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	var cfg envConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return normalizeEnvMap(cfg.Global)
}

func saveEnvConfig(runtimeDir string, env map[string]string) error {
	cleaned, err := normalizeEnvMap(env)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(envConfigFile{Global: cleaned}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(runtimeDir, envConfigFileName), append(data, '\n'), 0o600)
}

func normalizeEnvMap(input map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if !envKeyPattern.MatchString(key) {
			return nil, errors.New("环境变量名只能包含字母、数字和下划线，且不能以数字开头")
		}
		if isReservedAgentEnvKey(key) {
			return nil, errors.New("不能覆盖系统保留的环境变量：" + key)
		}
		out[key] = strings.TrimSpace(value)
	}
	return out, nil
}

func envInfoFromAgents(runtimeDir string, agents []model.Agent) (*model.DeviceEnvConfigInfo, error) {
	globalEnv, err := loadEnvConfig(runtimeDir)
	if err != nil {
		return nil, err
	}
	agentsInfo := make([]model.DeviceAgentEnvInfo, 0, len(agents))
	for _, agent := range agents {
		cfg, err := readAgentConfig(runtimeDir, agent.AgentID)
		if err != nil {
			agentsInfo = append(agentsInfo, model.DeviceAgentEnvInfo{
				AgentID:    agent.AgentID,
				Name:       agent.Name,
				ProjectDir: agent.ProjectDir,
			})
			continue
		}
		agentsInfo = append(agentsInfo, model.DeviceAgentEnvInfo{
			AgentID:     agent.AgentID,
			Name:        firstNonEmpty(agent.Name, cfg.Name),
			ProjectDir:  firstNonEmpty(agent.ProjectDir, cfg.ProjectDir),
			Environment: cloneEnv(cfg.UserEnv),
		})
	}
	revision := environmentRevision(globalEnv, agentsInfo)
	sort.Slice(agentsInfo, func(i, j int) bool {
		return agentsInfo[i].Name < agentsInfo[j].Name
	})
	return &model.DeviceEnvConfigInfo{GlobalEnvironment: globalEnv, Agents: agentsInfo, Revision: revision}, nil
}

func environmentRevision(global map[string]string, agents []model.DeviceAgentEnvInfo) string {
	agentEnv := make(map[string]map[string]string, len(agents))
	for _, agent := range agents {
		agentEnv[agent.AgentID] = cloneEnv(agent.Environment)
	}
	payload, _ := json.Marshal(struct {
		Global map[string]string            `json:"global"`
		Agents map[string]map[string]string `json:"agents"`
	}{Global: cloneEnv(global), Agents: agentEnv})
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest[:])
}

func isReservedAgentEnvKey(key string) bool {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "OPENCODE_RELAY_URL",
		"OPENCODE_RELAY_OPERATOR_KEY",
		"OPENCODE_RELAY_AGENT_ID",
		"OPENCODE_RELAY_MACHINE_ID",
		"OPENCODE_RELAY_PROJECT_ID",
		"OPENCODE_RELAY_PROJECT_ROOT",
		"OPENCODE_PROJECT_ROOT":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
