//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateWindowsLauncherShortcutTargetsStableExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "chat-codex-launcher.exe")
	shortcut := filepath.Join(dir, "Desktop", "码控.lnk")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := createWindowsLauncherShortcut(target, shortcut); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(shortcut)
	if err != nil {
		t.Fatalf("shortcut was not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("shortcut file is empty")
	}
	script := "$shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut(" + powerShellQuote(shortcut) + "); [Console]::Write($shortcut.TargetPath)"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	configureBackgroundCommand(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read shortcut target: %v: %s", err, output)
	}
	if !strings.EqualFold(strings.TrimSpace(string(output)), target) {
		t.Fatalf("shortcut target = %q, want %q", strings.TrimSpace(string(output)), target)
	}
}
