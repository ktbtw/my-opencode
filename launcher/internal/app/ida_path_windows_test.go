//go:build windows

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareIDARuntimePathPreservesASCIIPath(t *testing.T) {
	idaDir := filepath.Join(t.TempDir(), "ida", "9.2")
	if err := os.MkdirAll(idaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := prepareIDARuntimePath(idaDir)
	if err != nil {
		t.Fatal(err)
	}
	if !sameWindowsPath(got, idaDir) {
		t.Fatalf("ASCII IDA path changed: got=%q want=%q", got, idaDir)
	}
}

func TestPrepareIDARuntimePathCreatesASCIIJunction(t *testing.T) {
	base := t.TempDir()
	idaDir := filepath.Join(base, "三闻鱼", "ida", "9.2")
	if err := os.MkdirAll(idaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(idaDir, "idalib.dll")
	if err := os.WriteFile(marker, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	programData := filepath.Join(base, "program-data")
	t.Setenv("PROGRAMDATA", programData)
	t.Setenv("PUBLIC", "")

	got, err := prepareIDARuntimePath(idaDir)
	if err != nil {
		t.Fatal(err)
	}
	if !isASCIIPath(got) {
		t.Fatalf("prepared IDA path is not ASCII: %q", got)
	}
	if sameWindowsPath(got, idaDir) {
		t.Fatalf("non-ASCII IDA path was not aliased: %q", got)
	}
	data, err := os.ReadFile(filepath.Join(got, "idalib.dll"))
	if err != nil {
		t.Fatalf("read marker through junction: %v", err)
	}
	if string(data) != "fixture" {
		t.Fatalf("unexpected marker through junction: %q", data)
	}

	again, err := prepareIDARuntimePath(idaDir)
	if err != nil {
		t.Fatalf("reuse junction: %v", err)
	}
	if !sameWindowsPath(again, got) {
		t.Fatalf("junction path changed: first=%q second=%q", got, again)
	}
}

func TestManagedRuntimeInstallDirAcceptsValidatedIDAJunction(t *testing.T) {
	base := t.TempDir()
	runtimeDir := filepath.Join(base, "三闻鱼", "runtime")
	idaDir := filepath.Join(runtimeDir, "runtimes", "ida", "9.2")
	if err := os.MkdirAll(idaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"idalib.dll", "idapro.hexlic", "ida.exe"} {
		if err := os.WriteFile(filepath.Join(idaDir, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	programData := filepath.Join(base, "program-data")
	t.Setenv("PROGRAMDATA", programData)
	t.Setenv("PUBLIC", "")

	alias, err := prepareIDARuntimePath(idaDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := installedRuntimeManifest{RuntimeID: "ida", Version: "9.2", InstallDir: alias}
	if !isManagedRuntimeInstallDir(runtimeDir, manifest) {
		t.Fatalf("validated IDA junction was rejected: %s", alias)
	}

	other := manifest
	other.RuntimeID = "python"
	if isManagedRuntimeInstallDir(runtimeDir, other) {
		t.Fatal("external compatibility path was accepted for a non-IDA runtime")
	}

	wrongAlias := filepath.Join(base, "other", filepath.Base(alias))
	if err := ensureWindowsDirectoryJunction(wrongAlias, idaDir); err != nil {
		t.Fatal(err)
	}
	manifest.InstallDir = wrongAlias
	if isManagedRuntimeInstallDir(runtimeDir, manifest) {
		t.Fatal("IDA junction outside the compatibility root was accepted")
	}
}
