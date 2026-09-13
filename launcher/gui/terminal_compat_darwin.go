//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launcher/internal/config"
)

func terminalCompatSupported() bool {
	return true
}

func terminalCompatStartBackground(cfg config.Config, executable string) error {
	scriptPath, err := terminalCompatScriptPath(cfg)
	if err != nil {
		return err
	}
	if err := writeTerminalCompatScript(scriptPath, cfg, executable); err != nil {
		return err
	}
	return exec.Command("/usr/bin/open", "-gj", "-a", "Terminal", scriptPath).Start()
}

func terminalCompatAutostartOptions(cfg config.Config, executable string) (string, []string, error) {
	scriptPath, err := terminalCompatScriptPath(cfg)
	if err != nil {
		return "", nil, err
	}
	if err := writeTerminalCompatScript(scriptPath, cfg, executable); err != nil {
		return "", nil, err
	}
	return "/usr/bin/open", []string{"-gj", "-a", "Terminal", scriptPath}, nil
}

func terminalCompatScriptPath(cfg config.Config) (string, error) {
	runtimeDir := strings.TrimSpace(cfg.RuntimeDir)
	if runtimeDir == "" {
		return "", fmt.Errorf("launcher runtime dir 为空")
	}
	return filepath.Join(runtimeDir, "gui", "start-terminal-compat.command"), nil
}

func writeTerminalCompatScript(path string, cfg config.Config, executable string) error {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return fmt.Errorf("launcher 可执行文件路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	closeScript := filepath.Join(filepath.Dir(path), "close-terminal-compat.applescript")
	if err := writeTerminalCompatCloseScript(closeScript); err != nil {
		return err
	}
	logPath := filepath.Join(cfg.RuntimeDir, "logs", "launcher-terminal-compat.log")
	content := fmt.Sprintf(`#!/bin/zsh
export LAUNCHER_RUNTIME_DIR=%q
export LAUNCHER_LISTEN_ADDR=%q
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${PATH}"
mkdir -p %q
close_terminal_window() {
  (/bin/sleep 0.45; /usr/bin/osascript %q) >/dev/null 2>&1 &!
}
if /usr/sbin/lsof -nP -iTCP:%s -sTCP:LISTEN >/dev/null 2>&1; then
  close_terminal_window
  exit 0
fi
nohup %q --background >> %q 2>&1 &
close_terminal_window
exit 0
`, cfg.RuntimeDir, cfg.ListenAddr, filepath.Dir(logPath), closeScript, listenPort(cfg.ListenAddr), executable, logPath)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeTerminalCompatCloseScript(path string) error {
	content := `with timeout of 2 seconds
  tell application "Terminal"
    repeat with terminalWindow in windows
      try
        if name of terminalWindow contains "start-terminal-compat.command" then
          close terminalWindow saving no
        end if
      end try
    end repeat
  end tell
end timeout
`
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
