package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenCodeConfigDirUsesAbsoluteXDGConfigHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), "xdg-config"))

	got, err := OpenCodeConfigDir()
	if err != nil {
		t.Fatalf("config dir: %v", err)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestOpenCodeConfigDirFallsBackToDotConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "relative-dir")

	got, err := OpenCodeConfigDir()
	if err != nil {
		t.Fatalf("config dir: %v", err)
	}
	want := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestLauncherRuntimeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		local := filepath.Join(home, "AppData", "Local")
		t.Setenv("LOCALAPPDATA", local)
		got, err := LauncherRuntimeDir()
		if err != nil {
			t.Fatalf("runtime dir: %v", err)
		}
		want := filepath.Join(local, "my-opencode-launcher")
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
		return
	}
	got, err := LauncherRuntimeDir()
	if err != nil {
		t.Fatalf("runtime dir: %v", err)
	}
	want := filepath.Join(home, ".my-opencode-launcher")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
