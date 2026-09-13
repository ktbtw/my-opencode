package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"launcher/internal/fileutil"
)

type DeviceState struct {
	Status           string            `json:"status"`
	CurrentVersion   string            `json:"current_version,omitempty"`
	TargetVersion    string            `json:"target_version,omitempty"`
	PreviousVersion  string            `json:"previous_version,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	AgentCount       int               `json:"agent_count"`
	UpgradeLocked    bool              `json:"upgrade_locked"`
	UpgradeStage     string            `json:"upgrade_stage,omitempty"`
	UpgradeProgress  int               `json:"upgrade_progress,omitempty"`
	UpgradeMessage   string            `json:"upgrade_message,omitempty"`
	UpgradeStartedAt time.Time         `json:"upgrade_started_at,omitempty"`
	UpgradeUpdatedAt time.Time         `json:"upgrade_updated_at,omitempty"`
	UpgradeLogs      []UpgradeLogEntry `json:"upgrade_logs,omitempty"`
	StartedAt        time.Time         `json:"started_at,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at,omitempty"`
}

type UpgradeLogEntry struct {
	Time    time.Time `json:"time"`
	Stage   string    `json:"stage,omitempty"`
	Message string    `json:"message"`
}

func Load(runtimeDir string) (DeviceState, error) {
	path := filepath.Join(runtimeDir, "state.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DeviceState{Status: "idle"}, nil
	}
	if err != nil {
		return DeviceState{}, err
	}
	var st DeviceState
	if err := json.Unmarshal(data, &st); err != nil {
		return DeviceState{}, err
	}
	return st, nil
}

func Save(runtimeDir string, st DeviceState) error {
	st.UpdatedAt = time.Now().UTC()
	if st.StartedAt.IsZero() {
		st.StartedAt = st.UpdatedAt
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(runtimeDir, "state.json"), append(data, '\n'), 0o644)
}
