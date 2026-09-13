//go:build windows

package backgroundcmd

import (
	"os/exec"
	"testing"
)

func TestConfigureHidesWindowsCommand(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "exit", "0")
	Configure(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected Windows process attributes")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("expected command window to be hidden")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("expected CREATE_NO_WINDOW flag, got %#x", cmd.SysProcAttr.CreationFlags)
	}
}
