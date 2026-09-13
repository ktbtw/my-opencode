package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"launcher/internal/fileutil"
	"launcher/internal/model"
)

func loadAgents(runtimeDir string) ([]model.Agent, error) {
	path := filepath.Join(runtimeDir, "agents.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []model.Agent{}, nil
	}
	if err != nil {
		return nil, err
	}
	var agents []model.Agent
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, err
	}
	normalizeAgentEnabledDefaults(agents)
	return agents, nil
}

func normalizeAgentEnabledDefaults(agents []model.Agent) {
	for i := range agents {
		if !strings.EqualFold(agents[i].Status, "disabled") {
			agents[i].Enabled = true
		}
	}
}

func saveAgents(runtimeDir string, agents []model.Agent) error {
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(runtimeDir, "agents.json"), append(data, '\n'), 0o644)
}
