package binarymeta

import (
	"path/filepath"
	"runtime"
	"strings"
)

func DefaultBinaryName() string {
	return ExecutableName("opencode")
}

func LauncherBinaryName() string {
	return ExecutableName("launcher")
}

func GUILauncherBinaryName() string {
	return ExecutableName("chat-codex-launcher")
}

func ExecutableName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "opencode"
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func MatchesExecutableName(name string) bool {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" {
		return false
	}
	return strings.EqualFold(base, ExecutableName("opencode"))
}

func MatchesLauncherExecutableName(name string) bool {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" {
		return false
	}
	return strings.EqualFold(base, LauncherBinaryName()) ||
		strings.EqualFold(base, GUILauncherBinaryName())
}
