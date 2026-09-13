package selfupdate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateLockRejectsConcurrentOwner(t *testing.T) {
	runtimeDir := t.TempDir()
	lock, err := acquireUpdateLock(runtimeDir)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer lock.Release()

	if _, err := acquireUpdateLock(runtimeDir); err != ErrUpdateInProgress {
		t.Fatalf("expected ErrUpdateInProgress, got %v", err)
	}
}

func TestUpdateInProgressReportsActiveCrossProcessLock(t *testing.T) {
	runtimeDir := t.TempDir()
	lock, err := acquireUpdateLock(runtimeDir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer lock.Release()

	inProgress, err := UpdateInProgress(runtimeDir)
	if err != nil {
		t.Fatalf("check update transaction: %v", err)
	}
	if !inProgress {
		t.Fatal("expected active update transaction")
	}
}

func TestApplyStatusAppliesToVersion(t *testing.T) {
	tests := []struct {
		name    string
		status  ApplyStatus
		current string
		want    bool
	}{
		{name: "completed current", status: ApplyStatus{Version: "0.1.150", State: "completed"}, current: "0.1.150", want: true},
		{name: "completed older", status: ApplyStatus{Version: "0.1.149", State: "completed"}, current: "0.1.150", want: false},
		{name: "failed future", status: ApplyStatus{Version: "0.1.151", State: "failed"}, current: "0.1.150", want: true},
		{name: "failed older", status: ApplyStatus{Version: "0.1.149", State: "failed"}, current: "0.1.150", want: false},
		{name: "legacy empty version", status: ApplyStatus{State: "failed"}, current: "0.1.150", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ApplyStatusAppliesToVersion(test.status, test.current); got != test.want {
				t.Fatalf("ApplyStatusAppliesToVersion() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestReplaceExecutableStagesBesideTargetAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "launcher.new")
	target := filepath.Join(targetDir, "launcher")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	replacement, err := replaceExecutable(source, target)
	if err != nil {
		t.Fatalf("replace executable: %v", err)
	}
	if filepath.Dir(replacement.Staged) != targetDir {
		t.Fatalf("staged file should be beside target: %+v", replacement)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Fatalf("expected new target, got %q", got)
	}
	if got, _ := os.ReadFile(replacement.Backup); string(got) != "old" {
		t.Fatalf("expected old backup, got %q", got)
	}
	if err := rollbackExecutable(replacement); err != nil {
		t.Fatalf("rollback executable: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Fatalf("expected restored target, got %q", got)
	}
}

func TestWaitLauncherHealthyRequiresExpectedVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "running",
			"launcher_version": "9.9.9",
		})
	}))
	defer server.Close()

	if err := waitLauncherHealthy(server.URL, "9.9.9", time.Second); err != nil {
		t.Fatalf("wait healthy: %v", err)
	}
	if err := waitLauncherHealthy(server.URL, "9.9.8", 100*time.Millisecond); err == nil {
		t.Fatal("expected version mismatch")
	}
}

func TestCleanupRemovesTerminalUpdateArtifacts(t *testing.T) {
	runtimeDir := t.TempDir()
	root := filepath.Join(runtimeDir, "self-updates")
	if err := os.MkdirAll(filepath.Join(root, "updater"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "apply-status.json"),
		filepath.Join(root, "restart-pending"),
		filepath.Join(root, "updater", "old.exe"),
		filepath.Join(root, "0.1.1", "launcher.exe"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Cleanup(runtimeDir); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "apply-status.json")); err != nil {
		t.Fatalf("expected apply status to remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "updater")); !os.IsNotExist(err) {
		t.Fatalf("expected updater directory removed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "restart-pending")); !os.IsNotExist(err) {
		t.Fatalf("expected stale restart marker removed, got %v", err)
	}
}

func TestCleanupSkipsActiveUpdateTransaction(t *testing.T) {
	runtimeDir := t.TempDir()
	root := filepath.Join(runtimeDir, "self-updates")
	lock, err := acquireUpdateLock(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	protected := filepath.Join(root, "0.1.149", "chat-codex-launcher.exe")
	if err := os.MkdirAll(filepath.Dir(protected), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protected, []byte("new launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(runtimeDir); err != nil {
		t.Fatalf("cleanup active transaction: %v", err)
	}
	if got, err := os.ReadFile(protected); err != nil || string(got) != "new launcher" {
		t.Fatalf("active update payload was removed: %q err=%v", got, err)
	}
}
