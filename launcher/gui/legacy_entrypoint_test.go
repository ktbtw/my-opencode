package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveLegacyGUIEntrypoints(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "stable", "chat-codex-launcher.exe")
	desktop := filepath.Join(dir, "Desktop", "chat-codex-launcher.exe")
	oneDriveDesktop := filepath.Join(dir, "OneDrive", "Desktop", "chat-codex-launcher.exe")
	missing := filepath.Join(dir, "Missing", "chat-codex-launcher.exe")
	writeTestFile(t, source, []byte("stable launcher"))
	writeTestFile(t, desktop, []byte("old desktop launcher"))
	writeTestFile(t, oneDriveDesktop, []byte("old OneDrive launcher"))

	if err := removeLegacyGUIEntrypoints(source, []string{desktop, oneDriveDesktop, missing, desktop}); err != nil {
		t.Fatalf("remove legacy GUI entrypoints: %v", err)
	}
	for _, target := range []string{desktop, oneDriveDesktop} {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("legacy desktop executable remained at %s: %v", target, err)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing desktop entrypoint should not be created: %v", err)
	}
}

func TestRemoveLegacyGUIEntrypointsPreservesStableTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "stable", "chat-codex-launcher.exe")
	target := filepath.Join(dir, "Desktop", "chat-codex-launcher.exe")
	writeTestFile(t, source, []byte("same launcher"))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, target); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	if err := removeLegacyGUIEntrypoints(source, []string{target}); err != nil {
		t.Fatalf("remove stable GUI entrypoint: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat matching target: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("stable target is not a regular file")
	}
}

func writeTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}
