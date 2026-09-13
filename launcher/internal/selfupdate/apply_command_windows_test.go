//go:build windows

package selfupdate

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitParentExitWaitsForWindowsProcess(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "ping -n 30 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start parent fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	done := make(chan error, 1)
	go func() {
		done <- waitParentExit(cmd.Process.Pid, 5*time.Second)
	}()

	select {
	case err := <-done:
		t.Fatalf("wait returned while parent was still running: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("stop parent fixture: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed parent fixture to return an exit error")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait for parent exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait did not observe parent exit")
	}
}

func TestTerminateParentProcessRequiresMatchingExecutable(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "ping -n 30 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if err := terminateParentProcess(cmd.Process.Pid, filepath.Join(t.TempDir(), "other.exe")); err == nil {
		t.Fatal("expected executable mismatch")
	}
	if exists, err := processExists(cmd.Process.Pid); err != nil || !exists {
		t.Fatalf("mismatched process should remain running: exists=%v err=%v", exists, err)
	}
}

func TestTerminateParentProcessStopsMatchingExecutable(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "ping -n 30 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	executable, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := terminateParentProcess(cmd.Process.Pid, executable); err != nil {
		t.Fatalf("terminate matching process: %v", err)
	}
	if err := waitParentExit(cmd.Process.Pid, 3*time.Second); err != nil {
		t.Fatalf("wait for terminated process: %v", err)
	}
}

func TestWaitParentExitReturnsTimeoutOnWindows(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "ping -n 30 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start parent fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	if err := waitParentExit(cmd.Process.Pid, 100*time.Millisecond); err == nil {
		t.Fatal("expected parent wait timeout")
	}
}
