package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwitchWritesCurrentVersionFile(t *testing.T) {
	root := t.TempDir()
	versionDir := filepath.Join(root, "versions", "1.2.3")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}

	if err := Switch(root, "1.2.3"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	current, err := Current(root)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current != "1.2.3" {
		t.Fatalf("expected current version 1.2.3, got %s", current)
	}

	data, err := os.ReadFile(filepath.Join(root, currentVersionFile))
	if err != nil {
		t.Fatalf("read current version file: %v", err)
	}
	if string(data) != "1.2.3\n" {
		t.Fatalf("unexpected current version file content: %q", string(data))
	}
}
