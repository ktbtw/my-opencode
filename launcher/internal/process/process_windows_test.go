//go:build windows

package process

import (
	"os"
	"testing"
)

func TestWindowsProcessExistsUsesNativeAPI(t *testing.T) {
	running, err := platformWindowsProcessExists(os.Getpid())
	if err != nil {
		t.Fatalf("check current process: %v", err)
	}
	if !running {
		t.Fatal("expected current process to be running")
	}
}

func TestWindowsProcessTreeIncludesRoot(t *testing.T) {
	processes, err := windowsProcessTree(uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("snapshot process tree: %v", err)
	}
	if len(processes) == 0 || processes[0] != uint32(os.Getpid()) {
		t.Fatalf("expected process tree to start with root %d, got %v", os.Getpid(), processes)
	}
}
