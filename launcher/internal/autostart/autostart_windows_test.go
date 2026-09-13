//go:build windows

package autostart

import "testing"

func TestWindowsRegistryCommandRunsWithoutWindow(t *testing.T) {
	cmd := windowsRegistryCommand("query", windowsRunKey, "/v", windowsValueName)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected Windows process attributes")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("expected registry command window to be hidden")
	}
	if cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatalf("expected CREATE_NO_WINDOW flag, got %#x", cmd.SysProcAttr.CreationFlags)
	}
}
