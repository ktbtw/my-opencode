//go:build windows

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"launcher/internal/config"
)

func TestCopyBackgroundExecutableStopsLockedListenerBeforeRetry(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	runtimeDir := t.TempDir()
	target := filepath.Join(runtimeDir, "bin", "chat-codex-launcher.exe")
	source := filepath.Join(runtimeDir, "incoming", "chat-codex-launcher.exe")
	if err := copyTestExecutable(current, target, false); err != nil {
		t.Fatalf("create running target: %v", err)
	}
	if err := copyTestExecutable(current, source, true); err != nil {
		t.Fatalf("create different source: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cmd := exec.Command(target, "-test.run=TestLockedLauncherHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_WANT_LOCKED_LAUNCHER_HELPER=1",
		fmt.Sprintf("LOCKED_LAUNCHER_PORT=%d", port),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("open helper stdout: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start locked launcher helper: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != "READY" {
		t.Fatalf("wait for helper readiness: line=%q err=%v", ready, err)
	}

	app := NewGUIApp()
	cfg := config.Config{RuntimeDir: runtimeDir, ListenAddr: fmt.Sprintf("127.0.0.1:%d", port)}
	if err := app.copyBackgroundExecutableWithRecovery(cfg, source, target); err != nil {
		t.Fatalf("replace locked launcher with recovery: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("locked launcher helper was not stopped")
	}

	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read replaced target: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("locked launcher target was not replaced after process exit")
	}
}

func TestLockedLauncherHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LOCKED_LAUNCHER_HELPER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("LOCKED_LAUNCHER_PORT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("READY")
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(0)
		}
		_ = conn.Close()
	}
}

func copyTestExecutable(source string, target string, makeDifferent bool) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if makeDifferent {
		data = append(data, []byte("incoming-version")...)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o755)
}
