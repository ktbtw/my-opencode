//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestDetachCommandRunsUpdaterAndRestartWithoutWindow(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "exit", "0")
	detachCommand(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected Windows process attributes")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("expected updater command window to be hidden")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("expected CREATE_NO_WINDOW flag, got %#x", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("expected detached process group, got %#x", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&createBreakawayFromJob == 0 {
		t.Fatalf("expected CREATE_BREAKAWAY_FROM_JOB flag, got %#x", cmd.SysProcAttr.CreationFlags)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("run detached Windows command: %v", err)
	}
}

func TestStartScheduledTaskCommandReturnsRunningProcess(t *testing.T) {
	cmdPath, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := startScheduledTaskCommand(cmdPath, []string{"/C", "ping -n 20 127.0.0.1 >NUL"})
	if err != nil {
		t.Fatalf("start scheduled task command: %v", err)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find scheduled process %d: %v", pid, err)
	}
	t.Cleanup(func() { _ = process.Kill() })
	if exists, err := processExists(pid); err != nil || !exists {
		t.Fatalf("scheduled process %d is not running: exists=%v err=%v", pid, exists, err)
	}
	time.Sleep(100 * time.Millisecond)
}
