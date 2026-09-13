package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func LauncherRuntimeDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LAUNCHER_RUNTIME_DIR")); value != "" {
		if filepath.IsAbs(value) {
			return value, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if value := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); value != "" && filepath.IsAbs(value) {
			return filepath.Join(value, "my-opencode-launcher"), nil
		}
	}
	return filepath.Join(home, ".my-opencode-launcher"), nil
}

func OpenCodeConfigDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); value != "" {
		if filepath.IsAbs(value) {
			return filepath.Join(value, "opencode"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode"), nil
}
