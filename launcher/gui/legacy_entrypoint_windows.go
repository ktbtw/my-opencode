//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
	"launcher/internal/binarymeta"
)

func syncLegacyGUIEntrypoints(source string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	var migrationErrs []error
	var legacyCandidates []string
	for _, desktop := range windowsDesktopDirectories() {
		if info, err := os.Stat(desktop); err != nil || !info.IsDir() {
			continue
		}
		shortcut := filepath.Join(desktop, "码控.lnk")
		if err := createWindowsLauncherShortcut(source, shortcut); err != nil {
			migrationErrs = append(migrationErrs, err)
			continue
		}
		legacyCandidates = append(legacyCandidates, filepath.Join(desktop, binarymeta.GUILauncherBinaryName()))
	}
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		startMenu := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "码控.lnk")
		if err := createWindowsLauncherShortcut(source, startMenu); err != nil {
			migrationErrs = append(migrationErrs, err)
		}
	}
	if len(legacyCandidates) > 0 {
		if err := removeLegacyGUIEntrypointsWithRetry(source, legacyCandidates, 60*time.Second); err != nil {
			migrationErrs = append(migrationErrs, err)
		}
	}
	return errors.Join(migrationErrs...)
}

func windowsDesktopDirectories() []string {
	var candidates []string
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
	if err == nil {
		if value, _, valueErr := key.GetStringValue("Desktop"); valueErr == nil {
			if expanded, expandErr := registry.ExpandString(value); expandErr == nil {
				value = expanded
			}
			candidates = append(candidates, value)
		}
		_ = key.Close()
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		candidates = append(candidates, filepath.Join(home, "Desktop"), filepath.Join(home, "OneDrive", "Desktop"))
	}
	if oneDrive := strings.TrimSpace(os.Getenv("OneDrive")); oneDrive != "" {
		candidates = append(candidates, filepath.Join(oneDrive, "Desktop"))
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if candidate == "." || candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func createWindowsLauncherShortcut(target, shortcutPath string) error {
	target = strings.TrimSpace(target)
	shortcutPath = strings.TrimSpace(shortcutPath)
	if target == "" || shortcutPath == "" {
		return errors.New("Launcher 快捷方式路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(shortcutPath), 0o755); err != nil {
		return err
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut(%s)
$shortcut.TargetPath = %s
$shortcut.WorkingDirectory = %s
$shortcut.IconLocation = %s
$shortcut.Description = '码控 Launcher'
$shortcut.Save()`,
		powerShellQuote(shortcutPath),
		powerShellQuote(target),
		powerShellQuote(filepath.Dir(target)),
		powerShellQuote(target+",0"),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	configureBackgroundCommand(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建 Launcher 快捷方式 %s 失败: %w: %s", shortcutPath, err, strings.TrimSpace(string(output)))
	}
	return nil
}
