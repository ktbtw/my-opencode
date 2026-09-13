package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"launcher/internal/model"
)

type idaTestExitError struct {
	code int
}

func (e idaTestExitError) Error() string {
	return "exit"
}

func (e idaTestExitError) ExitCode() int {
	return e.code
}

func TestRuntimeTaskGroupsInstallIDALast(t *testing.T) {
	tasks := []runtimePreflightTask{
		{runtimeID: "ida", item: model.RuntimeCatalogItem{InstallStrategy: "ida_installer"}},
		{runtimeID: "python", item: model.RuntimeCatalogItem{InstallStrategy: "uv_python"}},
		{runtimeID: "uv", item: model.RuntimeCatalogItem{InstallStrategy: "archive"}},
		{runtimeID: "java", item: model.RuntimeCatalogItem{InstallStrategy: "archive"}},
	}
	groups := runtimeTaskGroups(tasks)
	got := make([]string, 0, len(groups))
	for _, group := range groups {
		got = append(got, group.name)
	}
	want := []string{"uv", "archive", "uv_python", "ida_installer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime task order: got %v want %v", got, want)
	}
}

func TestFindIDABinaryDirSupportsWindowsAndMacLayouts(t *testing.T) {
	root := t.TempDir()
	windowsDir := filepath.Join(root, "windows", "IDA Professional 9.2")
	macDir := filepath.Join(root, "mac", "IDA Professional 9.2.app", "Contents", "MacOS")
	for _, fixture := range []struct {
		dir     string
		library string
	}{
		{dir: windowsDir, library: "idalib.dll"},
		{dir: macDir, library: "libidalib.dylib"},
	} {
		if err := os.MkdirAll(fixture.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.dir, fixture.library), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := findIDABinaryDir(filepath.Dir(fixture.dir))
		if err != nil {
			t.Fatal(err)
		}
		if got != fixture.dir {
			t.Fatalf("IDA binary dir: got %q want %q", got, fixture.dir)
		}
	}
}

func TestIDALicenseArgsUseVersionManifest(t *testing.T) {
	version := model.RuntimeVersion{InstallManifest: map[string]any{
		"license_args": []any{"--license", "--start-date", "2026-07-27"},
		"patch_args":   []any{"--patch", "--apply"},
	}}
	want := []string{"--license", "--start-date", "2026-07-27"}
	if got := idaLicenseArgs(version); !reflect.DeepEqual(got, want) {
		t.Fatalf("license args: got %v want %v", got, want)
	}
	patchWant := []string{"--patch", "--apply"}
	if got := idaPatchArgs(version); !reflect.DeepEqual(got, patchWant) {
		t.Fatalf("patch args: got %v want %v", got, patchWant)
	}
}

func TestIDAInstallerTargetUsesVersionRoot(t *testing.T) {
	targetRoot := filepath.Join("managed", "runtimes", "ida", "9.2")
	if got := idaInstallerTarget(targetRoot); got != targetRoot {
		t.Fatalf("IDA installer target: got %q want %q", got, targetRoot)
	}
}

func TestIDAInstallerArgsSelectPythonOnWindows(t *testing.T) {
	target := filepath.Join("managed", "runtimes", "ida", "9.2")
	windowsWant := []string{
		"--mode", "unattended",
		"--unattendedmodeui", "none",
		"--prefix", target,
		"--install_python", "1",
	}
	if got := idaInstallerArgs("windows", target); !reflect.DeepEqual(got, windowsWant) {
		t.Fatalf("Windows IDA installer args: got %v want %v", got, windowsWant)
	}
	macWant := windowsWant[:6]
	if got := idaInstallerArgs("darwin", target); !reflect.DeepEqual(got, macWant) {
		t.Fatalf("macOS IDA installer args: got %v want %v", got, macWant)
	}
}

func TestIDAWindowsElevationActionUsesStableJobID(t *testing.T) {
	action := idaWindowsElevationAction(" preflight_1 ")
	if action.ID != "ida-uac:preflight_1" || action.Kind != "windows_uac" {
		t.Fatalf("unexpected IDA elevation action: %+v", action)
	}
	if action.Title == "" || action.Message == "" || len(action.Instructions) < 2 || action.RequestedAt.IsZero() {
		t.Fatalf("incomplete IDA elevation action: %+v", action)
	}
}

func TestIDAEULAKeyUsesVersionManifest(t *testing.T) {
	version := model.RuntimeVersion{InstallManifest: map[string]any{
		"eula_registry_key": "EULA 90",
	}}
	if got := idaEULAKey(version); got != "EULA 90" {
		t.Fatalf("EULA key: got %q", got)
	}
}

func TestInitializeIDAEULARetriesFirstExit255(t *testing.T) {
	prepareCalls := 0
	probeCalls := 0
	output, err := initializeIDAEULAWithRetry(
		context.Background(),
		func() (string, error) {
			prepareCalls++
			return "registry ready", nil
		},
		func(context.Context) (string, error) {
			probeCalls++
			if probeCalls == 1 {
				return "probe started", idaTestExitError{code: 255}
			}
			return "probe complete", nil
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepareCalls != 2 || probeCalls != 2 {
		t.Fatalf("unexpected attempts: prepare=%d probe=%d", prepareCalls, probeCalls)
	}
	for _, expected := range []string{
		"初始化尝试 1/2",
		"probe started",
		"退出码 255",
		"初始化尝试 2/2",
		"probe complete",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("retry output missing %q:\n%s", expected, output)
		}
	}
}

func TestInitializeIDAEULADoesNotRetryOtherExitCode(t *testing.T) {
	probeCalls := 0
	expectedErr := idaTestExitError{code: 1}
	output, err := initializeIDAEULAWithRetry(
		context.Background(),
		func() (string, error) { return "registry ready", nil },
		func(context.Context) (string, error) {
			probeCalls++
			return "import failed", expectedErr
		},
		0,
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("unexpected probe attempts: %d", probeCalls)
	}
	if strings.Contains(output, "初始化尝试 2/2") {
		t.Fatalf("unexpected retry output:\n%s", output)
	}
}

func TestInitializeIDAEULAStopsWhenRegistryPreparationFails(t *testing.T) {
	expectedErr := errors.New("registry failed")
	probeCalls := 0
	output, err := initializeIDAEULAWithRetry(
		context.Background(),
		func() (string, error) { return "registry output", expectedErr },
		func(context.Context) (string, error) {
			probeCalls++
			return "", nil
		},
		0,
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeCalls != 0 {
		t.Fatalf("probe ran after preparation failure: %d", probeCalls)
	}
	if !strings.Contains(output, "registry output") {
		t.Fatalf("missing registry diagnostics: %s", output)
	}
}

func TestVerifyIDARuntimeManifest(t *testing.T) {
	original := runtimePlatformForPreflight
	runtimePlatformForPreflight = func() (string, string) { return "windows", "amd64" }
	t.Cleanup(func() { runtimePlatformForPreflight = original })

	installDir := t.TempDir()
	for name, mode := range map[string]os.FileMode{
		"idalib.dll":    0o644,
		"idapro.hexlic": 0o644,
		"ida.exe":       0o755,
	} {
		if err := os.WriteFile(filepath.Join(installDir, name), []byte("fixture"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyIDARuntimeManifest(installedRuntimeManifest{RuntimeID: "ida", InstallDir: installDir}); err != nil {
		t.Fatal(err)
	}
}
