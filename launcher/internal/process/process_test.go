package process

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStopTerminatesProcess(t *testing.T) {
	cmd := sleepyCommand(t)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleepy command: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	if err := Stop(cmd.Process.Pid); err != nil {
		t.Fatalf("stop process: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForExit(ctx, cmd.Process.Pid, 100*time.Millisecond); err != nil {
		t.Fatalf("wait for exit: %v", err)
	}
	select {
	case err := <-done:
		if err != nil && !isExpectedStopWaitError(err) {
			t.Fatalf("wait command: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for command reap")
	}
}

func isExpectedStopWaitError(err error) bool {
	if err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		return strings.Contains(err.Error(), "exit status")
	}
	return err.Error() == "signal: terminated"
}

func TestWaitForExitReturnsForExitedProcess(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", "exit 0")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start short command: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait short command: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := WaitForExit(ctx, pid, 50*time.Millisecond); err != nil {
		t.Fatalf("wait for exited process: %v", err)
	}
}

func TestWaitForPortClosedReturnsAfterListenerStops(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = listener.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForPortClosed(ctx, port, 50*time.Millisecond); err != nil {
		t.Fatalf("wait for port closed: %v", err)
	}
}

func TestWaitForReadyAcceptsTCPListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := WaitForReady(ctx, port, 20*time.Millisecond); err != nil {
		t.Fatalf("wait for ready: %v", err)
	}
}

func TestFindListeningPIDReturnsCurrentProcessForListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	pid, err := FindListeningPID(port)
	if err != nil {
		t.Fatalf("find listening pid: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestConsoleMirrorPrefixesEachLine(t *testing.T) {
	var out bytes.Buffer
	mirror := newConsoleMirror(&out, "agent_demo", "stderr")
	if _, err := mirror.Write([]byte("first line\nsecond line\n")); err != nil {
		t.Fatalf("write mirror: %v", err)
	}
	got := out.String()
	want := "[agent:agent_demo][stderr] first line\n[agent:agent_demo][stderr] second line\n"
	if got != want {
		t.Fatalf("unexpected mirrored output:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestConsoleMirrorBuffersPartialLine(t *testing.T) {
	var out bytes.Buffer
	mirror := newConsoleMirror(&out, "agent_demo", "stdout")
	if _, err := mirror.Write([]byte("partial")); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output before newline, got %q", out.String())
	}
	if _, err := mirror.Write([]byte(" line\n")); err != nil {
		t.Fatalf("write remainder: %v", err)
	}
	got := out.String()
	want := "[agent:agent_demo][stdout] partial line\n"
	if got != want {
		t.Fatalf("unexpected mirrored output:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestMergeEnvOverridesExistingValue(t *testing.T) {
	base := []string{"PATH=/usr/bin", "FOO=old"}
	patch := map[string]string{"PATH": "/managed/bin:/usr/bin", "FOO": "new", "BAR": "added"}

	env := mergeEnv(base, patch)
	values := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if got := values["PATH"]; got != "/managed/bin:/usr/bin" {
		t.Fatalf("expected PATH override, got %q", got)
	}
	if got := values["FOO"]; got != "new" {
		t.Fatalf("expected FOO override, got %q", got)
	}
	if got := values["BAR"]; got != "added" {
		t.Fatalf("expected BAR addition, got %q", got)
	}
}

func sleepyCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", "ping -n 30 127.0.0.1 >NUL")
	}
	return exec.Command("/bin/sh", "-c", "sleep 30")
}
