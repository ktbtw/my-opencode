package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launcher/internal/binarymeta"
	"launcher/internal/config"
)

// handoffToInstalledGUI keeps downloaded desktop copies as launchers for the
// canonical GUI under the launcher runtime directory. Self-update replaces the
// canonical file, so old desktop copies must never create their own GUI.
func handoffToInstalledGUI() (bool, error) {
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	current, err := os.Executable()
	if err != nil {
		return false, err
	}
	target := installedGUIExecutable(cfg)
	if sameExecutable(current, target) {
		return false, nil
	}

	if info, statErr := os.Stat(target); statErr == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("稳定 GUI 路径不是普通文件: %s", target)
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if err := copyExecutableIfChanged(current, target); err != nil {
			return false, fmt.Errorf("安装桌面 GUI 到稳定路径失败: %w", err)
		}
	} else {
		return false, statErr
	}

	cmd := exec.Command(target, os.Args[1:]...)
	configureGUICommand(cmd)
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("启动稳定 GUI 失败: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return false, fmt.Errorf("释放稳定 GUI 进程失败: %w", err)
	}
	return true, nil
}

// handoffBackgroundToStable prevents an old installed app or desktop copy
// from claiming the control port after a launcher self-update. Only the
// canonical runtime binary may own the background service.
func handoffBackgroundToStable() (bool, error) {
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	current, err := os.Executable()
	if err != nil {
		return false, err
	}
	target := installedGUIExecutable(cfg)
	if sameExecutable(current, target) {
		return false, nil
	}

	if info, statErr := os.Stat(target); statErr == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("稳定后台路径不是普通文件: %s", target)
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if err := copyExecutableIfChanged(current, target); err != nil {
			return false, fmt.Errorf("安装稳定后台 launcher 失败: %w", err)
		}
	} else {
		return false, statErr
	}

	cmd := exec.Command(target, "--background")
	configureBackgroundCommand(cmd)
	cmd.Env = append(os.Environ(),
		"LAUNCHER_RUNTIME_DIR="+cfg.RuntimeDir,
		"LAUNCHER_LISTEN_ADDR="+cfg.ListenAddr,
	)
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("启动稳定后台 launcher 失败: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return false, fmt.Errorf("释放稳定后台 launcher 进程失败: %w", err)
	}
	return true, nil
}

func migrateLegacyRuntimeScript(cfg config.Config) error {
	path := filepath.Join(strings.TrimSpace(cfg.RuntimeDir), "run-launcher.sh")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "exec ") || !strings.Contains(strings.ReplaceAll(trimmed, "\\", "/"), "/bin/launcher") {
			continue
		}
		lines[index] = "exec " + fmt.Sprintf("%q", installedGUIExecutable(cfg)) + " --background"
		changed = true
	}
	if !changed {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func stableGUIPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
}
