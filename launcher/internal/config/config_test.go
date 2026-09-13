package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMachineIDIsRandomAndPersisted(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("LAUNCHER_MACHINE_ID", "")

	first := ensureMachineID(runtimeDir)
	second := ensureMachineID(runtimeDir)

	if first == "" || !strings.HasPrefix(first, "m_") {
		t.Fatalf("expected generated machine id with m_ prefix, got %q", first)
	}
	if first != second {
		t.Fatalf("expected persisted machine id, got first=%q second=%q", first, second)
	}
	data, err := os.ReadFile(filepath.Join(runtimeDir, "machine_id"))
	if err != nil {
		t.Fatalf("read machine id file: %v", err)
	}
	if strings.TrimSpace(string(data)) != first {
		t.Fatalf("expected file to contain %q, got %q", first, strings.TrimSpace(string(data)))
	}
}

func TestMachineIDEnvironmentOverride(t *testing.T) {
	t.Setenv("LAUNCHER_MACHINE_ID", "m_custom")
	if got := ensureMachineID(t.TempDir()); got != "m_custom" {
		t.Fatalf("expected env machine id, got %q", got)
	}
}

func TestSelfUpdateConfigDefaultsEnabled(t *testing.T) {
	t.Setenv("LAUNCHER_RUNTIME_DIR", t.TempDir())
	t.Setenv("LAUNCHER_SELF_UPDATE_ENABLED", "")
	t.Setenv("LAUNCHER_SELF_UPDATE_INITIAL_DELAY_SECONDS", "")
	t.Setenv("LAUNCHER_SELF_UPDATE_INTERVAL_SECONDS", "")
	t.Setenv("LAUNCHER_SELF_UPDATE_TIMEOUT_SECONDS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.SelfUpdate.Enabled {
		t.Fatal("expected self update to be enabled by default")
	}
	if cfg.SelfUpdate.InitialDelay != 20*time.Second {
		t.Fatalf("expected default initial delay 20s, got %s", cfg.SelfUpdate.InitialDelay)
	}
	if cfg.SelfUpdate.Interval != 30*time.Minute {
		t.Fatalf("expected default interval 30m, got %s", cfg.SelfUpdate.Interval)
	}
	if cfg.SelfUpdate.Timeout != 5*time.Minute {
		t.Fatalf("expected default timeout 5m, got %s", cfg.SelfUpdate.Timeout)
	}
	if !cfg.SelfUpdate.Restart {
		t.Fatal("expected restart enabled by default")
	}
}

func TestSelfUpdateConfigHonorsEnvironment(t *testing.T) {
	t.Setenv("LAUNCHER_RUNTIME_DIR", t.TempDir())
	t.Setenv("LAUNCHER_UPDATE_BASE_URL", "https://example.com/codex/")
	t.Setenv("LAUNCHER_SELF_UPDATE_ENABLED", "0")
	t.Setenv("LAUNCHER_SELF_UPDATE_INITIAL_DELAY_SECONDS", "1")
	t.Setenv("LAUNCHER_SELF_UPDATE_INTERVAL_SECONDS", "2")
	t.Setenv("LAUNCHER_SELF_UPDATE_TIMEOUT_SECONDS", "3")
	t.Setenv("LAUNCHER_SELF_UPDATE_RESTART", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.SelfUpdate.Enabled {
		t.Fatal("expected self update disabled by env")
	}
	if cfg.SelfUpdate.BaseURL != "https://example.com/codex" {
		t.Fatalf("expected trimmed base url, got %s", cfg.SelfUpdate.BaseURL)
	}
	if cfg.SelfUpdate.InitialDelay != time.Second {
		t.Fatalf("expected initial delay 1s, got %s", cfg.SelfUpdate.InitialDelay)
	}
	if cfg.SelfUpdate.Interval != 2*time.Second {
		t.Fatalf("expected interval 2s, got %s", cfg.SelfUpdate.Interval)
	}
	if cfg.SelfUpdate.Timeout != 3*time.Second {
		t.Fatalf("expected timeout 3s, got %s", cfg.SelfUpdate.Timeout)
	}
	if cfg.SelfUpdate.Restart {
		t.Fatal("expected restart disabled by env")
	}
}
