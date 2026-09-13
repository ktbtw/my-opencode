package selfupdate

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"launcher/internal/binarymeta"
)

func TestCheckResolvesRelativeLauncherAssetURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/launcher/version" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body := VersionInfo{
			Version: "9.9.9",
			Channel: "latest",
			Downloads: []Asset{{
				Platform:  currentPlatform(),
				Filename:  "launcher.zip",
				URL:       "/api/launcher/download?artifact=launcher.zip",
				Available: true,
			}},
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	result, err := Check(context.Background(), Options{
		CurrentVersion: "0.1.0",
		BaseURL:        server.URL,
	})
	if err != nil {
		t.Fatalf("check self update: %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
	want := server.URL + "/api/launcher/download?artifact=launcher.zip"
	if result.Asset == nil || result.Asset.URL != want {
		t.Fatalf("expected resolved URL %s, got %+v", want, result.Asset)
	}
}

func TestUpdateDryRunDoesNotRequireRuntimeDir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(VersionInfo{
			Version: "9.9.9",
			Downloads: []Asset{{
				Platform:  currentPlatform(),
				Filename:  "launcher.zip",
				URL:       "/launcher.zip",
				Available: true,
			}},
		})
	}))
	defer server.Close()

	result, err := Update(context.Background(), Options{
		CurrentVersion: "0.1.0",
		BaseURL:        server.URL,
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("dry run update: %v", err)
	}
	if result.Downloaded || result.Applied {
		t.Fatalf("dry run should not download or apply: %+v", result)
	}
}

func TestCheckDoesNotDowngradeLauncher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(VersionInfo{
			Version: "0.1.60",
			Downloads: []Asset{{
				Platform:  currentPlatform(),
				Filename:  "launcher.zip",
				URL:       "/launcher.zip",
				Available: true,
			}},
		})
	}))
	defer server.Close()

	result, err := Check(context.Background(), Options{
		CurrentVersion: "0.1.61",
		BaseURL:        server.URL,
	})
	if err != nil {
		t.Fatalf("check self update: %v", err)
	}
	if result.UpdateAvailable {
		t.Fatalf("did not expect downgrade to be available: %+v", result)
	}
}

func TestShouldUpdateComparesDottedVersions(t *testing.T) {
	tests := []struct {
		current string
		target  string
		want    bool
	}{
		{current: "0.1.60", target: "0.1.61", want: true},
		{current: "0.1.61", target: "0.1.60", want: false},
		{current: "0.1.61", target: "0.1.61", want: false},
		{current: "dev", target: "0.1.61", want: true},
	}
	for _, tt := range tests {
		if got := shouldUpdate(tt.current, tt.target); got != tt.want {
			t.Fatalf("shouldUpdate(%q, %q) = %v, want %v", tt.current, tt.target, got, tt.want)
		}
	}
}

func TestRestartPendingMarkerIsConsumedOnce(t *testing.T) {
	runtimeDir := t.TempDir()
	if ConsumeRestartPending(runtimeDir) {
		t.Fatal("expected missing marker to be false")
	}
	if err := MarkRestartPending(runtimeDir); err != nil {
		t.Fatalf("mark restart pending: %v", err)
	}
	if !ConsumeRestartPending(runtimeDir) {
		t.Fatal("expected marker to be consumed")
	}
	if ConsumeRestartPending(runtimeDir) {
		t.Fatal("expected marker to be consumed only once")
	}
}

func TestRestartMarkerFromStatusPath(t *testing.T) {
	status := filepath.Join("runtime", "self-updates", "apply-status.json")
	want := filepath.Join("runtime", "self-updates", "restart-pending")
	if got := restartMarkerFromStatusPath(status); got != want {
		t.Fatalf("expected restart marker %s, got %s", want, got)
	}
}

func TestMatchAssetPrefersGUIArchiveForGUILauncher(t *testing.T) {
	platform := currentPlatform()
	assets := []Asset{
		{Platform: platform, Filename: "launcher-windows-x64.zip"},
		{Platform: platform, Filename: "chat-codex-launcher-windows-x64.zip"},
	}
	asset, err := matchAssetForExecutable(assets, "/opt/bin/chat-codex-launcher.exe")
	if err != nil {
		t.Fatalf("match GUI asset: %v", err)
	}
	if asset.Filename != "chat-codex-launcher-windows-x64.zip" {
		t.Fatalf("expected GUI launcher asset, got %s", asset.Filename)
	}
}

func TestMatchAssetPrefersCLIArchiveForCLILauncher(t *testing.T) {
	platform := currentPlatform()
	assets := []Asset{
		{Platform: platform, Filename: "launcher-windows-x64.zip"},
		{Platform: platform, Filename: "chat-codex-launcher-windows-x64.zip"},
	}
	asset, err := matchAssetForExecutable(assets, "/opt/bin/launcher.exe")
	if err != nil {
		t.Fatalf("match CLI asset: %v", err)
	}
	if asset.Filename != "launcher-windows-x64.zip" {
		t.Fatalf("expected CLI launcher asset, got %s", asset.Filename)
	}
}

func TestMatchAssetIgnoresStandaloneTunnelHelper(t *testing.T) {
	platform := currentPlatform()
	assets := []Asset{
		{Platform: platform, Filename: "chat-codex-tunnel-" + platform},
		{Platform: platform, Filename: "launcher-" + platform + ".zip"},
	}
	asset, err := matchAssetForExecutable(assets, "/opt/bin/launcher")
	if err != nil {
		t.Fatalf("match launcher asset: %v", err)
	}
	if strings.HasPrefix(asset.Filename, "chat-codex-tunnel-") {
		t.Fatalf("standalone Tunnel helper was selected as launcher update: %s", asset.Filename)
	}
}

func TestUpdateTargetExecutableUsesStableGUIInstall(t *testing.T) {
	runtimeDir := filepath.Join("runtime", "launcher")
	guiCurrent := filepath.Join("downloads", binarymeta.GUILauncherBinaryName())
	want := filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if got := updateTargetExecutable(runtimeDir, guiCurrent); got != want {
		t.Fatalf("expected stable GUI target %s, got %s", want, got)
	}
	cliCurrent := filepath.Join("downloads", binarymeta.ExecutableName("launcher"))
	if got := updateTargetExecutable(runtimeDir, cliCurrent); got != cliCurrent {
		t.Fatalf("CLI target changed unexpectedly: %s", got)
	}
}

func TestPrepareApplyPayloadSurvivesSelfUpdateCleanup(t *testing.T) {
	runtimeDir := t.TempDir()
	source := filepath.Join(runtimeDir, "self-updates", "0.1.149", binarymeta.GUILauncherBinaryName())
	target := filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := prepareApplyPayload(source, target, "0.1.149")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(payload)
	if filepath.Dir(payload) != filepath.Dir(target) {
		t.Fatalf("payload must be owned beside stable target: %s", payload)
	}
	if err := os.RemoveAll(filepath.Join(runtimeDir, "self-updates")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(payload); err != nil || string(got) != "new launcher" {
		t.Fatalf("owned payload did not survive staged cleanup: %q err=%v", got, err)
	}
}

func TestUnpackZipRequiresLauncherExecutable(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "launcher.zip")
	writer, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(writer)
	file, err := zipWriter.Create(binaryNameForTest())
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := file.Write([]byte("launcher")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	name, err := unpackZip(archive, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatalf("unpack zip: %v", err)
	}
	if name != binaryNameForTest() {
		t.Fatalf("expected %s, got %s", binaryNameForTest(), name)
	}
}

func TestUnpackZipAcceptsGUILauncherExecutable(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "launcher.zip")
	writer, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(writer)
	file, err := zipWriter.Create(binarymetaGUILauncherNameForTest())
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := file.Write([]byte("launcher")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	helper, err := zipWriter.Create(filepath.Join("bundle", "bin", tunnelHelperNameForTest()))
	if err != nil {
		t.Fatalf("create helper zip entry: %v", err)
	}
	if _, err := helper.Write([]byte("tunnel-helper")); err != nil {
		t.Fatalf("write helper zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	name, err := unpackZip(archive, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatalf("unpack zip: %v", err)
	}
	if name != binarymetaGUILauncherNameForTest() {
		t.Fatalf("expected %s, got %s", binarymetaGUILauncherNameForTest(), name)
	}
	helperData, err := os.ReadFile(filepath.Join(dir, "out", tunnelHelperNameForTest()))
	if err != nil {
		t.Fatalf("read extracted tunnel helper: %v", err)
	}
	if string(helperData) != "tunnel-helper" {
		t.Fatalf("unexpected tunnel helper contents: %q", helperData)
	}
}

func TestInstallStagedTunnelHelper(t *testing.T) {
	workDir := t.TempDir()
	runtimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, tunnelHelperNameForTest()), []byte("new-helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installStagedTunnelHelper(workDir, runtimeDir); err != nil {
		t.Fatalf("install staged helper: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(runtimeDir, "bin", tunnelHelperNameForTest()))
	if err != nil {
		t.Fatalf("read installed helper: %v", err)
	}
	if string(data) != "new-helper" {
		t.Fatalf("unexpected installed helper contents: %q", data)
	}
}

func binaryNameForTest() string {
	if runtime.GOOS == "windows" {
		return "launcher.exe"
	}
	return "launcher"
}

func binarymetaGUILauncherNameForTest() string {
	if runtime.GOOS == "windows" {
		return "chat-codex-launcher.exe"
	}
	return "chat-codex-launcher"
}

func tunnelHelperNameForTest() string {
	if runtime.GOOS == "windows" {
		return "chat-codex-tunnel.exe"
	}
	return "chat-codex-tunnel"
}
