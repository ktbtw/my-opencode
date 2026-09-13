//go:build windows

package main

import (
	"os/exec"
	"testing"
)

func TestConfigureGUICommandDoesNotHideWindow(t *testing.T) {
	cmd := exec.Command("chat-codex-launcher.exe")
	configureGUICommand(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatalf("GUI handoff should use the interactive desktop, got SysProcAttr=%+v", cmd.SysProcAttr)
	}
}
