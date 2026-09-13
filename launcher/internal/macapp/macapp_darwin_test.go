//go:build darwin

package macapp

import "testing"

func TestAppBundlePathForExecutable(t *testing.T) {
	got, ok := appBundlePathForExecutable("/Applications/码控.app/Contents/MacOS/chat-codex-launcher")
	if !ok {
		t.Fatal("expected app bundle path")
	}
	if got != "/Applications/码控.app" {
		t.Fatalf("unexpected app bundle path: %s", got)
	}
}

func TestInstalledExecutablePath(t *testing.T) {
	want := "/Applications/码控.app/Contents/MacOS/chat-codex-launcher"
	if got := InstalledExecutablePath(); got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
