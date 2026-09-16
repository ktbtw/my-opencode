package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

type testAPIOptions struct {
	artifactFetcher     func(context.Context, *model.Task, model.Artifact, io.Writer) error
	aiConfigRequest     func(context.Context, int64, string, string, string, any) (model.DeviceAIConfigResultPayload, error)
	goalOptimizeRequest func(context.Context, int64, string, string, string, any) (model.GoalOptimizeResultPayload, error)
	mcpConfigRequest    func(context.Context, int64, string, string, string, any) (model.DeviceMCPConfigResultPayload, error)
	envConfigRequest    func(context.Context, int64, string, string, string, any) (model.DeviceEnvConfigResultPayload, error)
	launcherRequest     func(context.Context, int64, string, string, string, any) (model.DeviceLauncherResultPayload, error)
	directoryRequest    func(context.Context, int64, string, string, string, any) (model.DeviceDirectoriesResultPayload, error)
}

func newTestAPI(s *store.Memory, b *broker.Broker, a *auth.Manager, opts testAPIOptions) *API {
	return New(
		s,
		b,
		a,
		nil,
		opts.artifactFetcher,
		nil,
		nil,
		opts.aiConfigRequest,
		opts.goalOptimizeRequest,
		opts.mcpConfigRequest,
		opts.envConfigRequest,
		opts.launcherRequest,
		opts.directoryRequest,
	)
}

func routeContext(key, value string) *chi.Context {
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add(key, value)
	return ctx
}

func TestResolveAppDownloadFileSupportsCLIArtifacts(t *testing.T) {
	filePath, contentType, downloadName, ok := resolveAppDownloadFile("opencode-windows-x64.zip")
	if !ok {
		t.Fatal("expected cli artifact to be supported by app download endpoint")
	}
	if filePath != "/opt/chat-codex/apk/cli/opencode-windows-x64.zip" {
		t.Fatalf("unexpected file path: %s", filePath)
	}
	if contentType != "application/zip" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if downloadName != "opencode-windows-x64.zip" {
		t.Fatalf("unexpected download name: %s", downloadName)
	}
}

func TestAppDownloadUsesVersionedAPKFilename(t *testing.T) {
	tmpDir := t.TempDir()
	originalAPKDir := apkDir
	apkDir = tmpDir
	t.Cleanup(func() { apkDir = originalAPKDir })

	if err := os.WriteFile(filepath.Join(tmpDir, "app-release.apk"), []byte("apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "version.json"), []byte(`{"version":"1.6.31"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "https://example.com/api/app/download?v=178", nil)
	rr := httptest.NewRecorder()
	api.AppDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Disposition"); got != "attachment; filename=chat-codex-1.6.31.apk" {
		t.Fatalf("unexpected content disposition: %s", got)
	}
}

func TestResolveCLIDownloadFileRejectsUnknownArtifact(t *testing.T) {
	if _, _, _, ok := resolveCLIDownloadFile("unknown.zip"); ok {
		t.Fatal("expected unknown cli artifact to be rejected")
	}
}

func TestResolveRuntimeUVDownloadFileSupportsKnownArtifacts(t *testing.T) {
	filePath, contentType, downloadName, ok := resolveRuntimeUVDownloadFile("uv-aarch64-apple-darwin.tar.gz")
	if !ok {
		t.Fatal("expected uv artifact to be supported")
	}
	if filePath != "/opt/chat-codex/apk/runtime/uv/uv-aarch64-apple-darwin.tar.gz" {
		t.Fatalf("unexpected file path: %s", filePath)
	}
	if contentType != "application/gzip" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if downloadName != "uv-aarch64-apple-darwin.tar.gz" {
		t.Fatalf("unexpected download name: %s", downloadName)
	}
}

func TestResolveRuntimeUVDownloadFileRejectsPathTraversal(t *testing.T) {
	for _, artifact := range []string{"../uv-aarch64-apple-darwin.tar.gz", "unknown.tar.gz"} {
		if _, _, _, ok := resolveRuntimeUVDownloadFile(artifact); ok {
			t.Fatalf("expected runtime artifact %q to be rejected", artifact)
		}
	}
}

func TestResolveRuntimeNodeDownloadFileSupportsKnownArtifacts(t *testing.T) {
	filePath, contentType, downloadName, ok := resolveRuntimeNodeDownloadFile("node-v22.16.0-darwin-arm64.tar.gz")
	if !ok {
		t.Fatal("expected node artifact to be supported")
	}
	if filePath != "/opt/chat-codex/apk/runtime/node/node-v22.16.0-darwin-arm64.tar.gz" {
		t.Fatalf("unexpected file path: %s", filePath)
	}
	if contentType != "application/gzip" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if downloadName != "node-v22.16.0-darwin-arm64.tar.gz" {
		t.Fatalf("unexpected download name: %s", downloadName)
	}
}

func TestResolveRuntimeJavaDownloadFileSupportsKnownArtifacts(t *testing.T) {
	filePath, contentType, downloadName, ok := resolveRuntimeJavaDownloadFile("OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz")
	if !ok {
		t.Fatal("expected java artifact to be supported")
	}
	if filePath != "/opt/chat-codex/apk/runtime/java/OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz" {
		t.Fatalf("unexpected file path: %s", filePath)
	}
	if contentType != "application/gzip" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if downloadName != "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz" {
		t.Fatalf("unexpected download name: %s", downloadName)
	}
}

func TestResolveRuntimeGoDownloadFileSupportsKnownArtifacts(t *testing.T) {
	filePath, contentType, downloadName, ok := resolveRuntimeGoDownloadFile("go1.25.4.darwin-arm64.tar.gz")
	if !ok {
		t.Fatal("expected go artifact to be supported")
	}
	if filePath != "/opt/chat-codex/apk/runtime/go/go1.25.4.darwin-arm64.tar.gz" {
		t.Fatalf("unexpected file path: %s", filePath)
	}
	if contentType != "application/gzip" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if downloadName != "go1.25.4.darwin-arm64.tar.gz" {
		t.Fatalf("unexpected download name: %s", downloadName)
	}
}

func TestResolveRuntimeGoDownloadFileRejectsUnknownArtifact(t *testing.T) {
	for _, artifact := range []string{"../go1.25.4.darwin-arm64.tar.gz", "go1.26.0.darwin-arm64.tar.gz"} {
		if _, _, _, ok := resolveRuntimeGoDownloadFile(artifact); ok {
			t.Fatalf("expected runtime artifact %q to be rejected", artifact)
		}
	}
}

func TestResolveRuntimeGhidraDownloadFileSupportsKnownArtifact(t *testing.T) {
	artifact := "ghidra_12.1.2_PUBLIC_20260605.zip"
	filePath, contentType, downloadName, ok := resolveRuntimeGhidraDownloadFile(artifact)
	if !ok {
		t.Fatal("expected Ghidra artifact to be supported")
	}
	if filePath != "/opt/chat-codex/apk/runtime/ghidra/"+artifact {
		t.Fatalf("unexpected file path: %s", filePath)
	}
	if contentType != "application/zip" || downloadName != artifact {
		t.Fatalf("unexpected Ghidra download metadata: type=%s name=%s", contentType, downloadName)
	}
	if _, _, _, ok := resolveRuntimeGhidraDownloadFile("../" + artifact); ok {
		t.Fatal("expected Ghidra path traversal to be rejected")
	}
}

func TestResolveRuntimeIDADownloadFileSupportsKnownArtifacts(t *testing.T) {
	for _, artifact := range []string{"bgspa-ida92-win.zip", "bgspa-ida92-x64mac.zip", "bgspa-ida92-armmac.zip"} {
		filePath, contentType, downloadName, ok := resolveRuntimeIDADownloadFile(artifact)
		if !ok {
			t.Fatalf("expected IDA artifact %s to be supported", artifact)
		}
		if filePath != filepath.Join("/opt/chat-codex/apk/runtime/ida", artifact) {
			t.Fatalf("unexpected IDA file path: %s", filePath)
		}
		if contentType != "application/zip" || downloadName != artifact {
			t.Fatalf("unexpected IDA metadata: type=%s name=%s", contentType, downloadName)
		}
	}
	for _, artifact := range []string{"../bgspa-ida92-win.zip", "ida92-linux.zip"} {
		if _, _, _, ok := resolveRuntimeIDADownloadFile(artifact); ok {
			t.Fatalf("expected IDA artifact %q to be rejected", artifact)
		}
	}
}

func TestResolveRuntimeToolDownloadFileSupportsKnownArtifacts(t *testing.T) {
	tests := []struct {
		artifact    string
		contentType string
	}{
		{artifact: "jadx-1.5.5.zip", contentType: "application/zip"},
		{artifact: "apktool_3.0.2.jar", contentType: "application/java-archive"},
	}
	for _, test := range tests {
		filePath, contentType, downloadName, ok := resolveRuntimeToolDownloadFile(test.artifact)
		if !ok {
			t.Fatalf("expected tool artifact %s to be supported", test.artifact)
		}
		if filePath != "/opt/chat-codex/apk/runtime/tool/"+test.artifact {
			t.Fatalf("unexpected file path for %s: %s", test.artifact, filePath)
		}
		if contentType != test.contentType || downloadName != test.artifact {
			t.Fatalf("unexpected metadata for %s: type=%s name=%s", test.artifact, contentType, downloadName)
		}
	}
	for _, artifact := range []string{"../jadx-1.5.5.zip", "unknown.zip"} {
		if _, _, _, ok := resolveRuntimeToolDownloadFile(artifact); ok {
			t.Fatalf("expected tool artifact %q to be rejected", artifact)
		}
	}
}

func TestResolveRuntimeMCPDownloadFileSupportsKnownArtifacts(t *testing.T) {
	for _, test := range []struct {
		artifact    string
		contentType string
	}{
		{artifact: "custom-mcp.tar.gz", contentType: "application/gzip"},
		{artifact: "custom_mcp-1.0.0-py3-none-any.whl", contentType: "application/zip"},
	} {
		filePath, contentType, downloadName, ok := resolveRuntimeMCPDownloadFile(test.artifact)
		if !ok {
			t.Fatalf("expected mcp artifact %q to be supported", test.artifact)
		}
		if filePath != filepath.Join("/opt/chat-codex/apk/runtime/mcp", test.artifact) {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		if contentType != test.contentType {
			t.Fatalf("unexpected content type: %s", contentType)
		}
		if downloadName != test.artifact {
			t.Fatalf("unexpected download name: %s", downloadName)
		}
	}
}

func TestResolveRuntimeMCPDownloadFileRejectsUnknownArtifact(t *testing.T) {
	for _, artifact := range []string{"", "../jadx-mcp-server.tar.gz", "nested/jadx-mcp-server.tar.gz", "../idapro.whl", "nested/idapro.whl", "unknown.zip"} {
		if _, _, _, ok := resolveRuntimeMCPDownloadFile(artifact); ok {
			t.Fatalf("expected mcp artifact %q to be rejected", artifact)
		}
	}
}

func TestRuntimeUVDownloadServesArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	original := runtimeDir
	runtimeDir = tmpDir
	t.Cleanup(func() {
		runtimeDir = original
	})

	dir := filepath.Join(runtimeDir, "uv")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	path := filepath.Join(dir, "uv-aarch64-apple-darwin.tar.gz")
	if err := os.WriteFile(path, []byte("uv-test"), 0o644); err != nil {
		t.Fatalf("write uv artifact: %v", err)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/runtime/uv/download?artifact=uv-aarch64-apple-darwin.tar.gz", nil)
	rr := httptest.NewRecorder()

	api.RuntimeUVDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("unexpected content type: %s", got)
	}
	if got := rr.Body.String(); got != "uv-test" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestRuntimeJavaDownloadServesArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	original := runtimeDir
	runtimeDir = tmpDir
	t.Cleanup(func() {
		runtimeDir = original
	})

	dir := filepath.Join(runtimeDir, "java")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	path := filepath.Join(dir, "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz")
	if err := os.WriteFile(path, []byte("java-test"), 0o644); err != nil {
		t.Fatalf("write java artifact: %v", err)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/runtime/java/download?artifact=OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz", nil)
	rr := httptest.NewRecorder()

	api.RuntimeJavaDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("unexpected content type: %s", got)
	}
	if got := rr.Body.String(); got != "java-test" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestRuntimeMCPDownloadServesArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	original := runtimeDir
	runtimeDir = tmpDir
	t.Cleanup(func() {
		runtimeDir = original
	})

	dir := filepath.Join(runtimeDir, "mcp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	path := filepath.Join(dir, "jadx-mcp-server.tar.gz")
	if err := os.WriteFile(path, []byte("mcp-test"), 0o644); err != nil {
		t.Fatalf("write mcp artifact: %v", err)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/runtime/mcp/download?artifact=jadx-mcp-server.tar.gz", nil)
	rr := httptest.NewRecorder()

	api.RuntimeMCPDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("unexpected content type: %s", got)
	}
	if got := rr.Body.String(); got != "mcp-test" {
		t.Fatalf("unexpected body: %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runtime/mcp/download/jadx-mcp-server.tar.gz", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("artifact", "jadx-mcp-server.tar.gz")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rr = httptest.NewRecorder()

	api.RuntimeMCPDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for path artifact, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "mcp-test" {
		t.Fatalf("unexpected path artifact body: %q", got)
	}
}

func TestLauncherAssetsFromBodyMarksAvailableFromLauncherDir(t *testing.T) {
	tmpDir := t.TempDir()
	originalLauncherDir := launcherDir
	launcherDir = tmpDir
	t.Cleanup(func() {
		launcherDir = originalLauncherDir
	})

	if err := os.MkdirAll(launcherDir, 0o755); err != nil {
		t.Fatalf("mkdir launcher dir: %v", err)
	}
	path := filepath.Join(launcherDir, "launcher-windows-x64.zip")
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write launcher artifact: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})

	req := httptest.NewRequest("GET", "https://example.com/api/launcher/version", nil)
	assets := launcherAssetsFromBody(req, []any{
		map[string]any{
			"platform": "windows-x64",
			"filename": "launcher-windows-x64.zip",
			"url":      "/api/launcher/download?artifact=launcher-windows-x64.zip",
		},
	})
	if len(assets) != 1 {
		t.Fatalf("expected one asset, got %d", len(assets))
	}
	if !assets[0].Available {
		t.Fatal("expected launcher asset to be marked available")
	}
	if assets[0].URL != "https://example.com/api/launcher/download?artifact=launcher-windows-x64.zip" {
		t.Fatalf("unexpected asset url: %s", assets[0].URL)
	}
}

func TestAppVersionPublishesAvailableWindowsInstaller(t *testing.T) {
	tmpDir := t.TempDir()
	originalAPKDir := apkDir
	apkDir = tmpDir
	t.Cleanup(func() { apkDir = originalAPKDir })

	if err := os.WriteFile(filepath.Join(tmpDir, "app-release.apk"), []byte("apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "chat-codex-windows-x64-setup.exe"), []byte("exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "chat-codex-darwin-arm64.dmg"), []byte("dmg"), 0o644); err != nil {
		t.Fatal(err)
	}
	versionJSON := `{"version":"2.0.0","version_code":200,"download_url":"/api/app/download","downloads":[{"platform":"android","filename":"chat-codex.apk","url":"/api/app/download","sha256":"apk-sha"},{"platform":"windows-x64","filename":"chat-codex-windows-x64-setup.exe","url":"/api/app/download?artifact=chat-codex-windows-x64-setup.exe","sha256":"exe-sha"},{"platform":"darwin-arm64","filename":"chat-codex-darwin-arm64.dmg","url":"/api/app/download?artifact=chat-codex-darwin-arm64.dmg","sha256":"dmg-sha"}]}`
	if err := os.WriteFile(filepath.Join(tmpDir, "version.json"), []byte(versionJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "https://example.com/api/app/version", nil)
	rr := httptest.NewRecorder()
	api.AppVersion(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Downloads []cliVersionAsset `json:"downloads"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Downloads) != 3 {
		t.Fatalf("expected three app assets, got %+v", body.Downloads)
	}
	windowsAsset := body.Downloads[1]
	if windowsAsset.Platform != "windows-x64" || !windowsAsset.Available || windowsAsset.SHA256 != "exe-sha" {
		t.Fatalf("unexpected windows asset: %+v", windowsAsset)
	}
	if windowsAsset.URL != "https://example.com/api/app/download?artifact=chat-codex-windows-x64-setup.exe" {
		t.Fatalf("unexpected windows asset URL: %s", windowsAsset.URL)
	}
	macAsset := body.Downloads[2]
	if macAsset.Platform != "darwin-arm64" || !macAsset.Available || macAsset.SHA256 != "dmg-sha" {
		t.Fatalf("unexpected macOS asset: %+v", macAsset)
	}
}

func TestAppReleasesPublishesStructuredHistory(t *testing.T) {
	tmpDir := t.TempDir()
	oldDir := apkDir
	apkDir = tmpDir
	t.Cleanup(func() { apkDir = oldDir })
	versionJSON := `{"version":"2.0.0","version_code":200,"releases":[{"version":"2.0.0","version_code":200,"items":["search"]}]}`
	if err := os.WriteFile(filepath.Join(tmpDir, "version.json"), []byte(versionJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "https://example.com/api/app/releases", nil)
	rr := httptest.NewRecorder()
	api.AppReleases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Releases []map[string]any `json:"releases"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Releases) != 1 || body.Releases[0]["version"] != "2.0.0" {
		t.Fatalf("unexpected releases: %+v", body.Releases)
	}
}

func TestResolveAppDownloadFileSupportsWindowsInstaller(t *testing.T) {
	path, contentType, name, ok := resolveAppDownloadFile("chat-codex-windows-x64-setup.exe")
	if !ok || filepath.Base(path) != name || name != "chat-codex-windows-x64-setup.exe" {
		t.Fatalf("unexpected resolver result: path=%s type=%s name=%s ok=%v", path, contentType, name, ok)
	}
	if contentType != "application/vnd.microsoft.portable-executable" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
}

func TestResolveAppDownloadFileSupportsMacOSInstaller(t *testing.T) {
	path, contentType, name, ok := resolveAppDownloadFile("chat-codex-darwin-arm64.dmg")
	if !ok || filepath.Base(path) != name || name != "chat-codex-darwin-arm64.dmg" {
		t.Fatalf("unexpected resolver result: path=%s type=%s name=%s ok=%v", path, contentType, name, ok)
	}
	if contentType != "application/x-apple-diskimage" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
}

func TestListDevicesMergesLauncherManagedAgentTotals(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	launcherRequest := func(context.Context, int64, string, string, string, any) (model.DeviceLauncherResultPayload, error) {
		return model.DeviceLauncherResultPayload{State: &model.DeviceLauncherState{Agents: []model.Agent{
			{ID: "agent_running", Name: "Running", Enabled: true, Status: "running"},
			{ID: "agent_disabled", Name: "Disabled", Enabled: false, Status: "disabled"},
		}}}, nil
	}
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, launcherRequest, nil)
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(nil, model.HelloPayload{
		MachineID: "m_agent_totals",
		DeviceID:  "launcher:m_agent_totals",
		Hostname:  "Windows",
		Kind:      "launcher",
	}, operator.ID)
	b.Add(nil, model.HelloPayload{
		AgentID: "agent_running", MachineID: "m_agent_totals", Hostname: "Windows",
		Projects: []model.HelloProject{{ProjectID: "project", Root: "/work/project"}},
	}, operator.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	api.ListDevices(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var machines []model.Machine
	if err := json.Unmarshal(rr.Body.Bytes(), &machines); err != nil {
		t.Fatal(err)
	}
	if len(machines) != 1 || len(machines[0].Agents) != 2 {
		t.Fatalf("expected launcher managed agents in list response: %+v", machines)
	}
	states := map[string]bool{}
	names := map[string]string{}
	for _, agent := range machines[0].Agents {
		states[agent.ID] = agent.Enabled
		names[agent.ID] = agent.Name
	}
	if !states["agent_running"] || states["agent_disabled"] {
		t.Fatalf("expected running and disabled states to be preserved: %+v", machines[0].Agents)
	}
	if names["agent_running"] != "Running" {
		t.Fatalf("expected launcher display name to populate the device response: %+v", machines[0].Agents)
	}
}

func TestDeviceProfileAndManualOrdersAreAccountScoped(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := newTestAPI(mem, b, authManager, testAPIOptions{})
	operatorOne := model.Operator{ID: 1, Username: "one"}
	operatorTwo := model.Operator{ID: 2, Username: "two"}
	tokenOne, _ := authManager.Issue(operatorOne)
	tokenTwo, _ := authManager.Issue(operatorTwo)
	for _, machineID := range []string{"machine-a", "machine-b"} {
		b.Add(nil, model.HelloPayload{
			MachineID: machineID, DeviceID: "launcher:" + machineID, Hostname: "host-" + machineID, Kind: "launcher",
		}, operatorOne.ID)
	}
	b.Add(nil, model.HelloPayload{
		MachineID: "machine-a", DeviceID: "launcher:machine-a", Hostname: "other-host", Kind: "launcher",
	}, operatorTwo.ID)

	profileReq := httptest.NewRequest(http.MethodPatch, "/api/devices/machine-a/profile", strings.NewReader(`{"display_name":"办公室电脑"}`))
	profileReq.Header.Set("Authorization", "Bearer "+tokenOne)
	profileReq = profileReq.WithContext(context.WithValue(profileReq.Context(), chi.RouteCtxKey, routeContext("machineID", "machine-a")))
	profileResponse := httptest.NewRecorder()
	api.UpdateDeviceProfile(profileResponse, profileReq)
	if profileResponse.Code != http.StatusOK {
		t.Fatalf("rename failed: %d %s", profileResponse.Code, profileResponse.Body.String())
	}

	reorderReq := httptest.NewRequest(http.MethodPatch, "/api/devices/order", strings.NewReader(`{"machine_ids":["machine-b","machine-a"]}`))
	reorderReq.Header.Set("Authorization", "Bearer "+tokenOne)
	reorderResponse := httptest.NewRecorder()
	api.ReorderDevices(reorderResponse, reorderReq)
	if reorderResponse.Code != http.StatusOK {
		t.Fatalf("reorder failed: %d %s", reorderResponse.Code, reorderResponse.Body.String())
	}

	list := func(token string) []model.Machine {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		api.ListDevices(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("list failed: %d %s", response.Code, response.Body.String())
		}
		var machines []model.Machine
		if err := json.Unmarshal(response.Body.Bytes(), &machines); err != nil {
			t.Fatal(err)
		}
		return machines
	}
	oneMachines := list(tokenOne)
	if len(oneMachines) != 2 || oneMachines[0].MachineID != "machine-b" || oneMachines[1].DisplayName != "办公室电脑" {
		t.Fatalf("unexpected operator one devices: %+v", oneMachines)
	}
	twoMachines := list(tokenTwo)
	if len(twoMachines) != 1 || twoMachines[0].DisplayName != "" {
		t.Fatalf("device profile leaked across accounts: %+v", twoMachines)
	}
}

func TestUpdateDeviceProfileValidatesUnicodeLengthAndVisibility(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := newTestAPI(mem, b, authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "tester"}
	token, _ := authManager.Issue(operator)
	b.Add(nil, model.HelloPayload{
		MachineID: "machine-a", DeviceID: "launcher:machine-a", Hostname: "host-a", Kind: "launcher",
	}, operator.ID)

	request := httptest.NewRequest(http.MethodPatch, "/api/devices/machine-a/profile", strings.NewReader(`{"display_name":"`+strings.Repeat("设", 41)+`"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext("machineID", "machine-a")))
	response := httptest.NewRecorder()
	api.UpdateDeviceProfile(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected unicode length validation, got %d: %s", response.Code, response.Body.String())
	}

	missing := httptest.NewRequest(http.MethodPatch, "/api/devices/missing/profile", strings.NewReader(`{"display_name":"name"}`))
	missing.Header.Set("Authorization", "Bearer "+token)
	missing = missing.WithContext(context.WithValue(missing.Context(), chi.RouteCtxKey, routeContext("machineID", "missing")))
	missingResponse := httptest.NewRecorder()
	api.UpdateDeviceProfile(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected hidden device to return 404, got %d", missingResponse.Code)
	}
}

func TestAgentOrderPersistsWhileTransientStateChanges(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, broker.New(), auth.NewManager(time.Hour), testAPIOptions{})
	if _, err := mem.ReorderAgentPreferences(1, "machine", []string{"agent-z", "agent-a"}); err != nil {
		t.Fatal(err)
	}
	machines := []model.Machine{{
		MachineID: "machine",
		Hostname:  "host",
		Agents: []model.Agent{
			{ID: "agent-a", Status: "running", CurrentTask: "task"},
			{ID: "agent-z", Status: "offline"},
			{ID: "agent-new", Status: "online"},
		},
	}}
	ordered, err := api.applyDevicePreferences(1, machines)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{ordered[0].Agents[0].ID, ordered[0].Agents[1].ID, ordered[0].Agents[2].ID}
	want := []string{"agent-z", "agent-a", "agent-new"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("unexpected agent order: got %v want %v", got, want)
	}
	machines[0].Agents[0].Status = "offline"
	machines[0].Agents[0].CurrentTask = ""
	machines[0].Agents[1].Status = "running"
	machines[0].Agents[1].CurrentTask = "task-two"
	ordered, err = api.applyDevicePreferences(1, machines)
	if err != nil {
		t.Fatal(err)
	}
	got = []string{ordered[0].Agents[0].ID, ordered[0].Agents[1].ID, ordered[0].Agents[2].ID}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("transient state changed agent order: got %v want %v", got, want)
	}
}

func TestGetDeviceLauncherDiagnosticsDispatchesReadOnlyRequest(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(nil, model.HelloPayload{
		MachineID: "m_diagnostics", DeviceID: "launcher:m_diagnostics", Kind: "launcher",
	}, operator.ID)
	var seenType string
	var seenPayload map[string]any
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, operatorID int64, launcherID, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			if operatorID != operator.ID || launcherID != "launcher:m_diagnostics" {
				t.Fatalf("unexpected diagnostics target: operator=%d launcher=%s", operatorID, launcherID)
			}
			seenType = envType
			blob, _ := json.Marshal(payload)
			_ = json.Unmarshal(blob, &seenPayload)
			return model.DeviceLauncherResultPayload{
				Success:     true,
				Diagnostics: map[string]any{"platform": "windows", "launcher_version": "0.1.131"},
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/devices/m_diagnostics/launcher/diagnostics?agent_id=agent_reverse&log_lines=321", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_diagnostics")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.GetDeviceLauncherDiagnostics(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if seenType != "device.launcher.diagnostics" || seenPayload["agent_id"] != "agent_reverse" || seenPayload["log_lines"] != float64(321) || seenPayload["include_logs"] != true {
		t.Fatalf("unexpected diagnostics dispatch: type=%s payload=%+v", seenType, seenPayload)
	}
	if !strings.Contains(rr.Body.String(), `"platform":"windows"`) {
		t.Fatalf("diagnostics result was not returned: %s", rr.Body.String())
	}
}

func TestGetDeviceLauncherStorageDispatchesRequest(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(nil, model.HelloPayload{
		MachineID:    "m_storage",
		DeviceID:     "launcher:m_storage",
		Kind:         "launcher",
		Capabilities: []string{launcherStorageCapability},
	}, operator.ID)

	var seenType, seenMachine string
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, operatorID int64, launcherID, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			if operatorID != operator.ID || launcherID != "launcher:m_storage" {
				t.Fatalf("unexpected storage target: operator=%d launcher=%s", operatorID, launcherID)
			}
			seenType = envType
			blob, _ := json.Marshal(payload)
			var decoded map[string]any
			_ = json.Unmarshal(blob, &decoded)
			seenMachine, _ = decoded["machine_id"].(string)
			return model.DeviceLauncherResultPayload{
				Success: true,
				Storage: map[string]any{
					"total_bytes": 12503856527,
					"categories": []map[string]any{
						{"key": "caches", "label": "依赖缓存", "bytes": 4195806303, "clearable": true},
						{"key": "runtimes", "label": "运行时组件", "bytes": 2543324789, "clearable": false},
					},
				},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_storage/launcher/storage", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_storage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.GetDeviceLauncherStorage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if seenType != "device.launcher.storage" {
		t.Fatalf("unexpected dispatch type: %s", seenType)
	}
	if seenMachine != "m_storage" {
		t.Fatalf("unexpected machine_id in payload: %q", seenMachine)
	}
	if !strings.Contains(rr.Body.String(), "12503856527") {
		t.Fatalf("storage result was not returned: %s", rr.Body.String())
	}
}

func TestClearDeviceLauncherStorageSendsNormalizedKeys(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(nil, model.HelloPayload{
		MachineID:    "m_storage_clear",
		DeviceID:     "launcher:m_storage_clear",
		Kind:         "launcher",
		Capabilities: []string{launcherStorageCapability},
	}, operator.ID)

	var seenType string
	var seenKeys []string
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _ string, _ string, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			seenType = envType
			blob, _ := json.Marshal(payload)
			var decoded struct {
				Keys []string `json:"keys"`
			}
			_ = json.Unmarshal(blob, &decoded)
			seenKeys = decoded.Keys
			return model.DeviceLauncherResultPayload{
				Success: true,
				Storage: map[string]any{"freed_bytes": 4195806303},
			}, nil
		},
	})

	// 重复键与空白键应在下发前被清理。
	body := strings.NewReader(`{"keys":["caches"," caches ","logs","  ","versions"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_storage_clear/launcher/storage/clear", body)
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_storage_clear")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.ClearDeviceLauncherStorage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if seenType != "device.launcher.storage_clear" {
		t.Fatalf("unexpected dispatch type: %s", seenType)
	}
	want := []string{"caches", "logs", "versions"}
	if len(seenKeys) != len(want) {
		t.Fatalf("期望 %d 个键，实际 %v", len(want), seenKeys)
	}
	for i, key := range want {
		if seenKeys[i] != key {
			t.Fatalf("键顺序或内容不符，期望 %v，实际 %v", want, seenKeys)
		}
	}
	if !strings.Contains(rr.Body.String(), "4195806303") {
		t.Fatalf("清理结果未返回: %s", rr.Body.String())
	}
}

func TestClearDeviceLauncherStorageRejectsOfflineDevice(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	api := newTestAPI(mem, b, authManager, testAPIOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_offline/launcher/storage/clear", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_offline")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.ClearDeviceLauncherStorage(rr, req)

	// 设备不在线时不能下发清理，避免请求悬空。
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetDeviceLauncherStorageRejectsLauncherWithoutCapability(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	// 老版本 launcher 不带 device_storage_v1 能力。
	b.Add(nil, model.HelloPayload{
		MachineID: "m_old", DeviceID: "launcher:m_old", Kind: "launcher",
	}, operator.ID)

	dispatched := false
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _ string, _ string, _ string, _ any) (model.DeviceLauncherResultPayload, error) {
			dispatched = true
			return model.DeviceLauncherResultPayload{Success: true}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_old/launcher/storage", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_old")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.GetDeviceLauncherStorage(rr, req)

	// 必须快速失败，而不是把请求下发出去等 90 秒超时。
	if dispatched {
		t.Fatal("不支持该能力的 launcher 不应收到存储查询指令")
	}
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "版本过低") {
		t.Fatalf("错误文案应提示更新 launcher: %s", rr.Body.String())
	}
}

func TestNormalizeStorageKeys(t *testing.T) {
	if got := normalizeStorageKeys(nil); len(got) != 0 {
		t.Fatalf("nil 应返回空切片，实际 %v", got)
	}
	got := normalizeStorageKeys([]string{"caches", "", "caches", " logs "})
	if len(got) != 2 || got[0] != "caches" || got[1] != "logs" {
		t.Fatalf("去重与裁剪结果不符: %v", got)
	}
}

func TestLauncherDownloadsDoNotExposeLinuxArtifact(t *testing.T) {
	if _, _, _, ok := resolveLauncherDownloadFile("launcher-linux-x64.tar.gz"); ok {
		t.Fatal("expected linux launcher artifact to be rejected")
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	downloadReq := httptest.NewRequest(http.MethodGet, "/api/launcher/download?artifact=launcher-linux-x64.tar.gz", nil)
	downloadRR := httptest.NewRecorder()
	api.LauncherDownload(downloadRR, downloadReq)
	if downloadRR.Code != http.StatusNotFound {
		t.Fatalf("expected linux launcher download to return 404, got %d", downloadRR.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/launcher/downloads", nil)
	listRR := httptest.NewRecorder()
	api.LauncherDownloads(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected launcher downloads to return 200, got %d", listRR.Code)
	}
	if strings.Contains(listRR.Body.String(), "launcher-linux-x64.tar.gz") {
		t.Fatalf("launcher downloads should not expose linux artifact: %s", listRR.Body.String())
	}
	if !strings.Contains(listRR.Body.String(), "chat-codex-launcher-windows-x64.zip") {
		t.Fatalf("launcher downloads should expose gui artifact: %s", listRR.Body.String())
	}
	if strings.Contains(listRR.Body.String(), `"filename":"launcher-windows-x64.zip"`) ||
		strings.Contains(listRR.Body.String(), "artifact=launcher-windows-x64.zip") {
		t.Fatalf("launcher downloads should not expose headless artifact: %s", listRR.Body.String())
	}

	req := httptest.NewRequest("GET", "https://example.com/api/launcher/version", nil)
	assets := launcherAssetsFromBody(req, []any{
		map[string]any{
			"platform": "linux-x64",
			"filename": "launcher-linux-x64.tar.gz",
			"url":      "/api/launcher/download?artifact=launcher-linux-x64.tar.gz",
		},
		map[string]any{
			"platform": "darwin-arm64",
			"filename": "launcher-darwin-arm64.zip",
			"url":      "/api/launcher/download?artifact=launcher-darwin-arm64.zip",
		},
	})
	if len(assets) != 1 {
		t.Fatalf("expected one visible launcher asset, got %d", len(assets))
	}
	if assets[0].Platform != "darwin-arm64" {
		t.Fatalf("unexpected launcher asset platform: %s", assets[0].Platform)
	}
}

func TestLauncherTunnelArtifactsAreDownloadableButHiddenFromDownloadsPage(t *testing.T) {
	tmpDir := t.TempDir()
	originalLauncherDir := launcherDir
	launcherDir = tmpDir
	t.Cleanup(func() { launcherDir = originalLauncherDir })
	filename := "chat-codex-tunnel-linux-x64"
	if err := os.WriteFile(filepath.Join(tmpDir, filename), []byte("tunnel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, contentType, _, ok := resolveLauncherDownloadFile(filename); !ok || contentType != "application/octet-stream" {
		t.Fatalf("expected standalone Tunnel helper to resolve, ok=%v content_type=%q", ok, contentType)
	}

	req := httptest.NewRequest("GET", "https://example.com/api/launcher/version", nil)
	assets := launcherAssetsFromBody(req, []any{map[string]any{
		"platform": "tunnel-linux-x64",
		"filename": filename,
		"url":      "/api/launcher/download?artifact=" + filename,
		"sha256":   strings.Repeat("a", 64),
	}})
	if len(assets) != 1 || !assets[0].Available {
		t.Fatalf("expected Linux Tunnel helper in version metadata: %+v", assets)
	}

	api := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	listRR := httptest.NewRecorder()
	api.LauncherDownloads(listRR, httptest.NewRequest(http.MethodGet, "/api/launcher/downloads", nil))
	if strings.Contains(listRR.Body.String(), "chat-codex-tunnel-") {
		t.Fatalf("Launcher downloads page exposed helper artifacts: %s", listRR.Body.String())
	}
}

func TestListDevicesIncludesOfflineAgentOnOnlineMachine(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	task := mem.CreateTask("agent_chat_codex", "m_online", "chat-codex", "/tmp/chat-codex", "", nil, nil, operator.ID)
	if _, ok := mem.Fail(task.ID, "ses_offline_1", "任务执行失败"); !ok {
		t.Fatal("expected task fail to create offline session")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	api.ListDevices(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var devices []model.Machine
	if err := json.Unmarshal(rr.Body.Bytes(), &devices); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if !devices[0].LauncherOnline {
		t.Fatal("expected launcher to remain online")
	}
	if len(devices[0].Agents) != 1 {
		t.Fatalf("expected offline agent to be merged into online machine, got %d agents", len(devices[0].Agents))
	}
	agent := devices[0].Agents[0]
	if agent.ID != "agent_chat_codex" {
		t.Fatalf("unexpected agent id: %s", agent.ID)
	}
	if agent.Status != "offline" {
		t.Fatalf("expected merged agent status offline, got %s", agent.Status)
	}
	if !agent.Enabled {
		t.Fatal("expected offline history agent to remain enabled by default")
	}
	if len(agent.Projects) != 1 || agent.Projects[0].ProjectID != "chat-codex" {
		t.Fatalf("unexpected agent projects: %+v", agent.Projects)
	}
}

func TestCancelTaskMarksCancellingBeforeDeviceConfirmation(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	connected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:   "agent_chat_codex",
			MachineID: "m_online",
			Hostname:  "MacBook",
			Version:   "opencode-test",
			Projects: []model.HelloProject{{
				ProjectID: "chat-codex", Root: "/tmp/chat-codex", ScopeID: "scope-task-memory", BindingEpoch: 1,
			}},
		}, operator.ID)
		close(connected)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.CloseNow()
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("websocket was not registered with broker")
	}

	task := mem.CreateTask("agent_chat_codex", "m_online", "chat-codex", "/tmp/chat-codex", "", nil, nil, operator.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID+"/cancel", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	api.CancelTask(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var cancelled model.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &cancelled); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cancelled.Status != model.TaskCancelling {
		t.Fatalf("expected cancelling response, got %s", cancelled.Status)
	}
	stored, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatal("expected task to remain stored")
	}
	if stored.Status != model.TaskCancelling {
		t.Fatalf("expected stored task cancelling, got %s", stored.Status)
	}
	events := mem.Events(task.ID)
	if len(events) == 0 || events[len(events)-1].Type != "cancelling" {
		t.Fatalf("expected cancelling event, got %+v", events)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read cancel envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode cancel envelope: %v", err)
	}
	if env.Type != "task.cancel" {
		t.Fatalf("expected task.cancel envelope, got %s", env.Type)
	}
	var payload model.CancelPayload
	raw, _ := json.Marshal(env.Payload)
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode cancel payload: %v", err)
	}
	if payload.TaskID != task.ID {
		t.Fatalf("expected cancel task %s, got %s", task.ID, payload.TaskID)
	}
}

func TestDeviceDetailUsesLauncherManagedAgentStatus(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()

	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, _ any) (model.DeviceLauncherResultPayload, error) {
			if envType != "device.launcher.get" {
				t.Fatalf("unexpected launcher env type %s", envType)
			}
			return model.DeviceLauncherResultPayload{
				Action:  "get",
				Success: true,
				State: &model.DeviceLauncherState{
					Agents: []model.Agent{{
						ID:         "agent_chat_codex",
						Enabled:    true,
						Status:     "restarting",
						Version:    "opencode-new",
						ProjectDir: "/tmp/chat-codex",
						Projects:   []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
					}},
				},
			}, nil
		},
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)
	b.Add(nil, model.HelloPayload{
		AgentID:   "agent_chat_codex",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Version:   "opencode-old",
		Projects:  []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
	}, operator.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_online", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.GetDevice(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var device model.Machine
	if err := json.Unmarshal(rr.Body.Bytes(), &device); err != nil {
		t.Fatalf("decode device: %v", err)
	}
	if len(device.Agents) != 1 {
		t.Fatalf("expected one agent, got %+v", device.Agents)
	}
	if device.Agents[0].Status != "restarting" {
		t.Fatalf("expected restarting status from launcher, got %+v", device.Agents[0])
	}
	if device.Agents[0].Version != "opencode-new" {
		t.Fatalf("expected launcher version to override stale websocket version, got %+v", device.Agents[0])
	}
}

func TestCreateTaskInjectsGlobalAndProjectPromptsIntoSystemOnly(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:   "agent_chat_codex",
			MachineID: "m_online",
			Hostname:  "MacBook",
			Version:   "opencode-test",
			Projects:  []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
		}, operator.ID)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := mem.SetDeviceSetting(operator.ID, userSettingsAgentID, globalPromptSettingKey, "global rule"); err != nil {
		t.Fatalf("set global prompt: %v", err)
	}
	if err := mem.SetDeviceSetting(operator.ID, "agent_chat_codex", projectPromptSettingKey, "project rule"); err != nil {
		t.Fatalf("set project prompt: %v", err)
	}
	if err := mem.UpsertProjectScope(ctx, model.ProjectScope{
		ID: "scope-task-memory", OperatorID: operator.ID, MachineID: "m_online",
		Status: model.ProjectScopeActive, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
		OperatorID: operator.ID, MachineID: "m_online", AgentID: "agent_chat_codex",
		ScopeID: "scope-task-memory", BindingEpoch: 1, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := mem.ReconcileProjectMemories(ctx, operator.ID, "scope-task-memory", 1, []model.ProjectMemoryCandidate{{
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "request:handling",
		Statement: "Apply accepted shared context during handling", Confidence: 1,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Sources:      []model.ProjectMemorySource{{TaskID: "source-task", MessageIDs: []string{"source-message"}}},
	}}, nil, nil)
	if err != nil || len(accepted.AcceptedIDs) != 1 {
		t.Fatalf("accept project memory: result=%+v err=%v", accepted, err)
	}

	body := `{
		"agent_id":"agent_chat_codex",
		"project_id":"chat-codex",
		"parts":[{"type":"text","text":"user request"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	api.CreateTask(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var created model.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if len(created.Parts) != 1 || created.Parts[0].Text != "user request" {
		t.Fatalf("expected stored task to keep original prompt, got %+v", created.Parts)
	}
	if created.Metadata["project_memory_scope_id"] != "scope-task-memory" ||
		created.Metadata["project_memory_revision"] != "2" {
		t.Fatalf("expected task-stable memory metadata, got %+v", created.Metadata)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read dispatched envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var payload model.RunPayload
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Parts) != 1 {
		t.Fatalf("expected one dispatched part, got %+v", payload.Parts)
	}
	if payload.Parts[0].Text != "user request" {
		t.Fatalf("expected dispatched text to keep original prompt, got %q", payload.Parts[0].Text)
	}
	for _, want := range []string{"【全局提示词】", "global rule", "【项目级提示词】", "project rule", "<project_memory>", "Apply accepted shared context during handling"} {
		if !strings.Contains(payload.System, want) {
			t.Fatalf("expected dispatched system to contain %q, got %q", want, payload.System)
		}
	}
	if strings.Contains(payload.System, "user request") {
		t.Fatalf("expected dispatched system not to contain user request, got %q", payload.System)
	}

	disabledAPI := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
	disabledReq := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	disabledReq.Header.Set("Authorization", "Bearer "+token)
	disabledReq.Header.Set("Content-Type", "application/json")
	disabledRR := httptest.NewRecorder()
	disabledAPI.CreateTask(disabledRR, disabledReq)
	if disabledRR.Code != http.StatusAccepted {
		t.Fatalf("disabled feature task returned %d: %s", disabledRR.Code, disabledRR.Body.String())
	}
	_, disabledData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read disabled feature dispatch: %v", err)
	}
	var disabledEnvelope model.Envelope
	if err := json.Unmarshal(disabledData, &disabledEnvelope); err != nil {
		t.Fatal(err)
	}
	disabledRaw, err := json.Marshal(disabledEnvelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var disabledPayload model.RunPayload
	if err := json.Unmarshal(disabledRaw, &disabledPayload); err != nil {
		t.Fatal(err)
	}
	expectedBase := buildTaskSystemPrompt("global rule", "project rule")
	if disabledPayload.System != expectedBase {
		t.Fatalf("disabled feature changed original prompt:\nwant=%q\n got=%q", expectedBase, disabledPayload.System)
	}
	if strings.Contains(disabledPayload.System, "<project_memory>") || strings.Contains(disabledPayload.System, "accepted project memory") {
		t.Fatalf("disabled feature injected project memory: %q", disabledPayload.System)
	}
}

func TestCreateModelTestDispatchesDedicatedRunAndStreamsEvents(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:   "agent_chat_codex",
			MachineID: "m_online",
			Hostname:  "MacBook",
			Version:   "opencode-test",
			Projects:  []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
		}, operator.ID)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if _, ok := b.Get("agent_chat_codex", operator.ID); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := b.Get("agent_chat_codex", operator.ID); !ok {
		t.Fatal("agent was not registered")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/model-tests", strings.NewReader(`{
		"agent_id":"agent_chat_codex",
		"project_id":"chat-codex",
		"model":"自己的号/gpt-5.5",
		"variant":"xhigh"
	}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.CreateModelTest(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var created modelTestJob
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode model test: %v", err)
	}
	if created.TestID == "" || created.Status != "dispatched" {
		t.Fatalf("unexpected created model test: %+v", created)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read dispatched envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Type != "model.test.run" {
		t.Fatalf("expected model.test.run envelope, got %s", env.Type)
	}
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var payload model.ModelTestRunPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.TestID != created.TestID || payload.ProviderID != "自己的号" || payload.ModelID != "gpt-5.5" || payload.Variant != "xhigh" {
		t.Fatalf("unexpected model test payload: %+v", payload)
	}

	HandleModelTestEvent("model.test.started", model.ModelTestEventPayload{
		TestID:    created.TestID,
		SessionID: "ses_test",
	})
	HandleModelTestEvent("model.test.completed", model.ModelTestEventPayload{
		TestID:     created.TestID,
		SessionID:  "ses_test",
		Content:    "连接正常",
		TextLength: 4,
	})

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/model-tests/"+created.TestID+"/events", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+token)
	eventsRR := httptest.NewRecorder()
	eventsCtx := chi.NewRouteContext()
	eventsCtx.URLParams.Add("testID", created.TestID)
	eventsReq = eventsReq.WithContext(context.WithValue(eventsReq.Context(), chi.RouteCtxKey, eventsCtx))
	api.ModelTestEvents(eventsRR, eventsReq)
	if eventsRR.Code != http.StatusOK {
		t.Fatalf("unexpected events status: %d body=%s", eventsRR.Code, eventsRR.Body.String())
	}
	body := eventsRR.Body.String()
	if !strings.Contains(body, "event: started") || !strings.Contains(body, "event: completed") || !strings.Contains(body, "ses_test") {
		t.Fatalf("expected started and completed SSE events, got %s", body)
	}
}

func TestTaskAgentHistoryUsesAgentThenFallsBackToMysql(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	operator := model.Operator{ID: 1, Username: "history-reader"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "ses_local", nil, nil, operator.ID)
	stamp := time.Unix(100, 0).UTC()
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "mysql", Field: "text", SentAt: stamp})

	t.Run("agent_local", func(t *testing.T) {
		api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
		api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
			return model.SessionHistoryResultPayload{
				TaskID:     task.ID,
				SessionID:  "ses_local",
				Source:     "agent_local",
				Events:     []model.Event{{TaskID: task.ID, Type: "delta", Field: "text", Content: "from-agent", SentAt: stamp}},
				Complete:   true,
				HasMore:    false,
				NextCursor: "",
			}, nil
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/agent-history?limit=200", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		api.TaskAgentHistory(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["source"] != "agent_local" {
			t.Fatalf("unexpected payload: %s", rr.Body.String())
		}
		events := payload["events"].([]any)
		if len(events) != 1 || events[0].(map[string]any)["content"] != "from-agent" {
			t.Fatalf("expected agent events, got %s", rr.Body.String())
		}
	})

	t.Run("mysql_legacy when agent returns empty history", func(t *testing.T) {
		api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
		api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
			return model.SessionHistoryResultPayload{
				TaskID:    task.ID,
				SessionID: "ses_local",
				Source:    "agent_local",
				Events:    nil,
				Complete:  true,
			}, nil
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/agent-history", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		api.TaskAgentHistory(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"source":"mysql_legacy"`) || !strings.Contains(rr.Body.String(), "mysql") {
			t.Fatalf("expected empty agent history to fall back to mysql, got %s", rr.Body.String())
		}
	})

	t.Run("appends task terminal event to agent history", func(t *testing.T) {
		done := mem.CreateTask("agent", "machine", "project", "/tmp", "ses_done", nil, nil, operator.ID)
		mem.Complete(done.ID, "ses_done", "final answer", nil)
		api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
		api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
			return model.SessionHistoryResultPayload{
				TaskID:    done.ID,
				SessionID: "ses_done",
				Source:    "agent_local",
				Events:    []model.Event{{TaskID: done.ID, Type: "delta", Field: "text", Content: "hello", Sequence: 1, SentAt: stamp}},
				Complete:  true,
			}, nil
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+done.ID+"/agent-history", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", done.ID)))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		api.TaskAgentHistory(rr, req)
		if !strings.Contains(rr.Body.String(), `"type":"completed"`) || !strings.Contains(rr.Body.String(), "final answer") {
			t.Fatalf("expected appended completed event, got %s", rr.Body.String())
		}
	})

	t.Run("does not append terminal event while agent history still has more pages", func(t *testing.T) {
		done := mem.CreateTask("agent", "machine", "project", "/tmp", "ses_more", nil, nil, operator.ID)
		mem.Complete(done.ID, "ses_more", "final answer", nil)
		api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
		api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
			return model.SessionHistoryResultPayload{
				TaskID:     done.ID,
				SessionID:  "ses_more",
				Source:     "agent_local",
				Events:     []model.Event{{TaskID: done.ID, Type: "tool_updated", Sequence: 1, SentAt: stamp, Tool: &model.ToolCall{ID: "t1", CallID: "c1", Tool: "read", Status: "running"}}},
				HasMore:    true,
				NextCursor: "2",
			}, nil
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+done.ID+"/agent-history", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", done.ID)))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		api.TaskAgentHistory(rr, req)
		if strings.Contains(rr.Body.String(), `"type":"completed"`) {
			t.Fatalf("did not expect completed on a partial history page: %s", rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"status":"running"`) {
			t.Fatalf("expected running tool on partial page, got %s", rr.Body.String())
		}
	})

	t.Run("mysql_legacy when agent history times out", func(t *testing.T) {
		api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
		api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
			return model.SessionHistoryResultPayload{}, fmt.Errorf("session history timeout")
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/agent-history", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		api.TaskAgentHistory(rr, req)
		if !strings.Contains(rr.Body.String(), `"source":"mysql_legacy"`) || !strings.Contains(rr.Body.String(), "mysql") {
			t.Fatalf("expected timeout to fall back to mysql, got %s", rr.Body.String())
		}
	})

	t.Run("mysql_legacy when agent offline", func(t *testing.T) {
		api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
		api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
			return model.SessionHistoryResultPayload{}, broker.ErrOffline
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/agent-history", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		api.TaskAgentHistory(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"source":"mysql_legacy"`) || !strings.Contains(rr.Body.String(), "mysql") {
			t.Fatalf("expected mysql fallback, got %s", rr.Body.String())
		}
	})
}

func TestTaskEventPagePaginatesDisplayEventsWithStableCursor(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "event-reader"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, operator.ID)
	stamp := time.Unix(100, 0).UTC()
	for i := 0; i < 205; i++ {
		mem.AddEvent(task.ID, model.Event{
			TaskID: task.ID, Type: "delta", Content: fmt.Sprintf("%03d", i),
			SentAt: stamp,
		})
	}
	// Non-display events are excluded by the dedicated query contract.
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "subagent_state", SentAt: stamp})

	request := func(cursor string) *http.Request {
		query := "?limit=200"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/event-pages"+query, nil)
		ctx := routeContext("taskID", task.ID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}
	type response struct {
		Events     []model.Event `json:"events"`
		NextCursor string        `json:"next_cursor"`
		HasMore    bool          `json:"has_more"`
	}
	first := httptest.NewRecorder()
	api.TaskEventPage(first, request(""))
	if first.Code != http.StatusOK {
		t.Fatalf("first page status=%d body=%s", first.Code, first.Body.String())
	}
	var firstPage response
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Events) != 200 || firstPage.Events[0].Content != "000" || firstPage.Events[199].Content != "199" || firstPage.NextCursor == "" || !firstPage.HasMore {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}
	second := httptest.NewRecorder()
	api.TaskEventPage(second, request(firstPage.NextCursor))
	if second.Code != http.StatusOK {
		t.Fatalf("second page status=%d body=%s", second.Code, second.Body.String())
	}
	var secondPage response
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Events) != 5 || secondPage.Events[0].Content != "200" || secondPage.Events[4].Content != "204" || secondPage.HasMore || secondPage.NextCursor == "" {
		t.Fatalf("unexpected second page: %+v", secondPage)
	}
	bad := httptest.NewRecorder()
	api.TaskEventPage(bad, request("not-a-cursor"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid cursor to be rejected, got %d", bad.Code)
	}
}

func TestTaskEventsCursorCatchesUpAndDeduplicatesSameTimestamp(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "sse-reader"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, operator.ID)
	stamp := time.Unix(200, 0).UTC()
	for _, content := range []string{"first", "second", "third"} {
		mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: content, SentAt: stamp})
	}
	cursor := encodeEventCursor(store.EventCursor{SentAt: stamp, ID: 1})
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/events", nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Task-Event-Cursor", cursor)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.TaskEvents(rr, req)
		close(done)
	}()
	// Let the initial cursor page be written, then publish a terminal event.
	time.Sleep(30 * time.Millisecond)
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", Content: "done", SentAt: stamp})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not terminate after terminal event")
	}
	body := rr.Body.String()
	if strings.Contains(body, "first") || !strings.Contains(body, "second") || !strings.Contains(body, "third") || !strings.Contains(body, "done") {
		t.Fatalf("unexpected cursor SSE body: %s", body)
	}
}

func TestTaskEventsCatchUpSendsCompletedToolWhenDeltaSharesTruncatedSecond(t *testing.T) {
	archive := &mixedIDEventArchive{nextID: 1_000_000}
	mem := store.NewMemory(archive)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "sse-reader"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, operator.ID)
	stamp := time.Unix(500, 0).UTC()
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Field: "text", Content: "before", SentAt: stamp})
	mem.AddEvent(task.ID, model.Event{
		TaskID: task.ID, Type: "tool_updated", SentAt: stamp,
		Tool: &model.ToolCall{ID: "prt_mcp", CallID: "call_mcp", Tool: "mcp_call", Status: "running"},
	})
	mem.AddEvent(task.ID, model.Event{
		TaskID: task.ID, Type: "tool_updated", SentAt: stamp,
		Tool: &model.ToolCall{ID: "prt_mcp", CallID: "call_mcp", Tool: "mcp_call", Status: "completed", Output: "ok"},
	})
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Field: "text", Content: "after", SentAt: stamp})

	cursor := encodeEventCursor(store.EventCursor{SentAt: stamp, ID: 1})
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/events", nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", "1")
	req.Header.Set("X-Task-Event-Cursor", cursor)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.TaskEvents(rr, req)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", Content: "done", SentAt: stamp})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not terminate after terminal event")
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("catch-up dropped running tool: %s", body)
	}
	if !strings.Contains(body, `"status":"completed"`) || !strings.Contains(body, `"output":"ok"`) {
		t.Fatalf("catch-up dropped completed tool that backend stored: %s", body)
	}
	if !strings.Contains(body, `"content":"after"`) {
		t.Fatalf("catch-up dropped later delta: %s", body)
	}
}

func TestTaskEventsSendsCompletedWhenHistoryWindowMissesTerminal(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "sse-reader"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, operator.ID)
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "partial", SentAt: time.Unix(200, 0).UTC()})
	mem.Complete(task.ID, "session", "final answer", nil)
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", Content: "final answer", SentAt: time.Unix(201, 0).UTC()})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/events", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.TaskEvents(rr, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not terminate after synthesizing completed")
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"type":"completed"`) || !strings.Contains(body, "final answer") {
		t.Fatalf("expected synthetic completed event, got %s", body)
	}
}

type mixedIDEventArchive struct {
	store.TaskArchive
	mu     sync.Mutex
	events []model.Event
	nextID int64
}

func (a *mixedIDEventArchive) UpsertTask(*model.Task) error { return nil }

func (a *mixedIDEventArchive) GetTask(string) (*model.Task, error) { return nil, nil }

func (a *mixedIDEventArchive) AppendEvent(event model.Event) error {
	return a.AppendEventWithID(&event)
}

func (a *mixedIDEventArchive) AppendEventWithID(event *model.Event) error {
	if event == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if event.ID <= 0 {
		if a.nextID == 0 {
			a.nextID = 1_000_000
		}
		a.nextID++
		event.ID = a.nextID
	}
	event.SentAt = event.SentAt.UTC().Truncate(time.Second)
	copy := *event
	if event.Tool != nil {
		tool := *event.Tool
		copy.Tool = &tool
	}
	a.events = append(a.events, copy)
	return nil
}

func (a *mixedIDEventArchive) ListEventPage(taskID string, after *store.EventCursor, limit int, eventTypes []string, nodeID string) (store.EventPage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	items := make([]model.Event, 0, limit+1)
	for _, event := range a.events {
		if event.TaskID != taskID {
			continue
		}
		if after != nil && (event.SentAt.Before(after.SentAt) || (event.SentAt.Equal(after.SentAt) && event.ID <= after.ID)) {
			continue
		}
		if len(eventTypes) > 0 {
			matched := false
			for _, eventType := range eventTypes {
				if event.Type == eventType {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		items = append(items, event)
	}
	page := store.EventPage{HasMore: len(items) > limit}
	if len(items) > limit {
		items = items[:limit]
	}
	page.Items = items
	if len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = &store.EventCursor{SentAt: last.SentAt, ID: last.ID}
	}
	return page, nil
}

type pagedTaskEventArchive struct {
	store.TaskArchive
	task          *model.Task
	events        []model.Event
	listPageCalls int
}

func (a *pagedTaskEventArchive) UpsertTask(task *model.Task) error {
	if task != nil {
		copy := *task
		a.task = &copy
	}
	return nil
}

func (a *pagedTaskEventArchive) GetTask(id string) (*model.Task, error) {
	if a.task == nil || a.task.ID != id {
		return nil, nil
	}
	copy := *a.task
	return &copy, nil
}

func (a *pagedTaskEventArchive) AppendEvent(event model.Event) error {
	if event.ID <= 0 {
		event.ID = int64(len(a.events) + 1)
	}
	a.events = append(a.events, event)
	return nil
}

func (a *pagedTaskEventArchive) ListEventPage(taskID string, after *store.EventCursor, limit int, eventTypes []string, nodeID string) (store.EventPage, error) {
	a.listPageCalls++
	items := make([]model.Event, 0, limit+1)
	for _, event := range a.events {
		if event.TaskID != taskID {
			continue
		}
		if after != nil && (event.SentAt.Before(after.SentAt) || (event.SentAt.Equal(after.SentAt) && event.ID <= after.ID)) {
			continue
		}
		if len(eventTypes) > 0 {
			matched := false
			for _, eventType := range eventTypes {
				if event.Type == eventType {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		items = append(items, event)
	}
	page := store.EventPage{HasMore: len(items) > limit}
	if len(items) > limit {
		items = items[:limit]
	}
	page.Items = items
	if len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = &store.EventCursor{SentAt: last.SentAt, ID: last.ID}
	}
	return page, nil
}

func TestTaskEventsCursorReadsMultiplePagesFromDurableArchive(t *testing.T) {
	t.Setenv("ARCHIVE_HIGH_FREQ_EVENTS", "1")
	archive := &pagedTaskEventArchive{}
	mem := store.NewMemory(archive)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "archive-reader"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, operator.ID)
	stamp := time.Unix(300, 0).UTC()
	for index := 0; index < 450; index++ {
		mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: fmt.Sprintf("%03d", index), SentAt: stamp})
	}
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", Content: "done", SentAt: stamp})
	cursor := encodeEventCursor(store.EventCursor{SentAt: stamp, ID: 1})
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/events", nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID))
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Task-Event-Cursor", cursor)
	rr := httptest.NewRecorder()
	api.TaskEvents(rr, req)
	body := rr.Body.String()
	if strings.Contains(body, "\"content\":\"000\"") || !strings.Contains(body, "\"content\":\"449\"") || !strings.Contains(body, "\"content\":\"done\"") {
		t.Fatalf("cursor history did not include all pages: body length=%d", len(body))
	}
	if archive.listPageCalls < 3 {
		t.Fatalf("SSE catch-up was not paged: listPageCalls=%d", archive.listPageCalls)
	}
}

func TestCreateTaskAcceptsFilePartWithArbitraryExtension(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:   "agent_chat_codex",
			MachineID: "m_online",
			Hostname:  "MacBook",
			Version:   "opencode-test",
			Projects:  []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
		}, operator.ID)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	fileURL := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte("exe bytes"))
	body := fmt.Sprintf(`{
		"agent_id":"agent_chat_codex",
		"project_id":"chat-codex",
		"parts":[{"type":"file","filename":"payload.exe","mime":"application/octet-stream","url":%q}]
	}`, fileURL)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	api.CreateTask(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var created model.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if len(created.Parts) != 1 || created.Parts[0].Filename != "payload.exe" || created.Parts[0].URL != fileURL {
		t.Fatalf("expected stored task to keep arbitrary file part, got %+v", created.Parts)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read dispatched envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var payload model.RunPayload
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Parts) != 1 || payload.Parts[0].Filename != "payload.exe" || payload.Parts[0].URL != fileURL {
		t.Fatalf("expected dispatched task to keep arbitrary file part, got %+v", payload.Parts)
	}
}

func TestListTasksPagesSessionHistoryBeforeCursor(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	for i := 0; i < 8; i++ {
		mem.CreateTask(
			"agent_chat_codex",
			"m_online",
			"chat-codex",
			"/tmp/chat-codex",
			"session_page",
			[]model.Part{{Type: "text", Text: fmt.Sprintf("message %d", i)}},
			nil,
			operator.ID,
		)
		time.Sleep(time.Millisecond)
	}

	firstReq := httptest.NewRequest(
		http.MethodGet,
		"/api/tasks?session_id=session_page&limit=5&sort=created_desc",
		nil,
	)
	firstReq.Header.Set("Authorization", "Bearer "+token)
	firstRR := httptest.NewRecorder()
	api.ListTasks(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("unexpected first page status: %d body=%s", firstRR.Code, firstRR.Body.String())
	}
	var firstPage []model.Task
	if err := json.Unmarshal(firstRR.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage) != 5 {
		t.Fatalf("expected first page to contain 5 tasks, got %d", len(firstPage))
	}
	if firstPage[0].Parts[0].Text != "message 7" || firstPage[4].Parts[0].Text != "message 3" {
		t.Fatalf("unexpected first page order: %+v", firstPage)
	}

	cursor := firstPage[len(firstPage)-1]
	secondURL := fmt.Sprintf(
		"/api/tasks?session_id=session_page&limit=5&before_created_at=%s&before_task_id=%s",
		url.QueryEscape(cursor.CreatedAt.Format(time.RFC3339Nano)),
		url.QueryEscape(cursor.ID),
	)
	secondReq := httptest.NewRequest(http.MethodGet, secondURL, nil)
	secondReq.Header.Set("Authorization", "Bearer "+token)
	secondRR := httptest.NewRecorder()
	api.ListTasks(secondRR, secondReq)
	if secondRR.Code != http.StatusOK {
		t.Fatalf("unexpected second page status: %d body=%s", secondRR.Code, secondRR.Body.String())
	}
	var secondPage []model.Task
	if err := json.Unmarshal(secondRR.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if len(secondPage) != 3 {
		t.Fatalf("expected second page to contain remaining 3 tasks, got %d", len(secondPage))
	}
	if secondPage[0].Parts[0].Text != "message 2" || secondPage[2].Parts[0].Text != "message 0" {
		t.Fatalf("unexpected second page order: %+v", secondPage)
	}
}

func TestCreateTaskNormalizesGoalDefaultMaxIterations(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:   "agent_chat_codex",
			MachineID: "m_online",
			Hostname:  "MacBook",
			Version:   "opencode-test",
			Projects:  []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
		}, operator.ID)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	body := `{
		"agent_id":"agent_chat_codex",
		"project_id":"chat-codex",
		"parts":[{"type":"text","text":"开始目标"}],
		"metadata":{"goal":"完成默认轮次测试"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	api.CreateTask(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var created model.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if created.Metadata["goal_max_iterations"] != "30" {
		t.Fatalf("expected goal_max_iterations=30, got %+v", created.Metadata)
	}
	if created.Metadata["goal_iteration"] != "0" {
		t.Fatalf("expected initial goal_iteration=0, got %+v", created.Metadata)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read dispatched envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var payload model.RunPayload
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Metadata["goal_max_iterations"] != "30" {
		t.Fatalf("expected dispatched goal_max_iterations=30, got %+v", payload.Metadata)
	}
}

func TestNormalizeAcceptsEmptyPartsForManualCompaction(t *testing.T) {
	parts, err := normalize(createTaskReq{
		SessionID: "session_compact",
		Metadata:  map[string]string{"task_command": "compact"},
	})
	if err != nil {
		t.Fatalf("normalize manual compaction: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected no prompt parts, got %#v", parts)
	}
	if _, err := normalize(createTaskReq{}); err == nil {
		t.Fatal("expected a normal empty task to remain invalid")
	}
}

func TestOptimizeGoalDispatchesRequest(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	var seen model.GoalOptimizePayload
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, agentID, requestID, envType string, payload any) (model.GoalOptimizeResultPayload, error) {
			if agentID != "agent_chat_codex" {
				t.Fatalf("unexpected agent id %s", agentID)
			}
			if !strings.HasPrefix(requestID, "req_goal_optimize_") {
				t.Fatalf("unexpected request id %s", requestID)
			}
			if envType != "goal.optimize" {
				t.Fatalf("unexpected env type %s", envType)
			}
			var ok bool
			seen, ok = payload.(model.GoalOptimizePayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			return model.GoalOptimizeResultPayload{
				Success:       true,
				OriginalGoal:  seen.Goal,
				OptimizedGoal: "优化后的长期目标",
			}, nil
		},
		nil,
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "agent_chat_codex",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Version:   "opencode-test",
		Projects:  []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
	}, operator.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/goal/optimize", strings.NewReader(`{
		"agent_id":"agent_chat_codex",
		"project_id":"chat-codex",
		"goal":"修复 Goal 保存流程",
		"model":"超级便宜/gpt-5.4",
		"variant":"high",
		"max_iterations":45
	}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	api.OptimizeGoal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if seen.ProjectID != "chat-codex" || seen.ProviderID != "超级便宜" || seen.ModelID != "gpt-5.4" {
		t.Fatalf("unexpected optimize payload %+v", seen)
	}
	if seen.Variant != "high" || seen.MaxIterations != 45 || seen.Goal != "修复 Goal 保存流程" {
		t.Fatalf("unexpected optimize details %+v", seen)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["goal"] != "优化后的长期目标" || body["model"] != "超级便宜/gpt-5.4" {
		t.Fatalf("unexpected response %+v", body)
	}
}

func TestListDevicesFiltersDeletedAgents(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	task := mem.CreateTask("agent_chat_codex", "m_online", "chat-codex", "/tmp/chat-codex", "", nil, nil, operator.ID)
	if _, ok := mem.Fail(task.ID, "ses_offline_1", "任务执行失败"); !ok {
		t.Fatal("expected task fail to create offline session")
	}
	if err := mem.SetDeviceSetting(operator.ID, "agent_chat_codex", deviceDeletedAgentKey, "true"); err != nil {
		t.Fatalf("mark agent deleted: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	api.ListDevices(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var devices []model.Machine
	if err := json.Unmarshal(rr.Body.Bytes(), &devices); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if len(devices[0].Agents) != 0 {
		t.Fatalf("expected deleted agent to be hidden, got %d agents", len(devices[0].Agents))
	}
}

func TestRemoveDeviceAgentTreatsAgentNotFoundAsDeleted(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, _ string, _ any) (model.DeviceLauncherResultPayload, error) {
			return model.DeviceLauncherResultPayload{
				Action:  "remove_agent",
				Success: false,
				Error:   "agent not found",
			}, errors.New("agent not found")
		},
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)
	if err := mem.SetDeviceSetting(operator.ID, "agent_chat_codex", "project_prompt", "keep"); err != nil {
		t.Fatalf("seed device setting: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/remove", strings.NewReader(`{"agent_id":"agent_chat_codex"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.RemoveDeviceAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	settings, err := mem.GetDeviceSettings(operator.ID, "agent_chat_codex")
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if !strings.EqualFold(settings[deviceDeletedAgentKey], "true") {
		t.Fatalf("expected agent to be marked deleted, got settings=%v", settings)
	}
	if _, exists := settings["project_prompt"]; exists {
		t.Fatalf("expected project settings to be cleared, got settings=%v", settings)
	}
}

func TestSetDeviceAgentEnabledForwardsLauncherRequest(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			if envType != "device.launcher.set_agent_enabled" {
				t.Fatalf("unexpected env type %s", envType)
			}
			req, ok := payload.(setDeviceAgentEnabledReq)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if req.AgentID != "agent_chat_codex" || req.Enabled {
				t.Fatalf("unexpected payload %+v", req)
			}
			return model.DeviceLauncherResultPayload{
				Action:  "set_agent_enabled",
				Success: true,
			}, nil
		},
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/enabled", strings.NewReader(`{"agent_id":"agent_chat_codex","enabled":false}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.SetDeviceAgentEnabled(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRenameDeviceAgentForwardsLauncherRequest(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			if envType != "device.launcher.rename_agent" {
				t.Fatalf("unexpected env type %s", envType)
			}
			req, ok := payload.(renameDeviceAgentReq)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if req.AgentID != "agent_chat_codex" || req.Name != "逆向专家安卓" {
				t.Fatalf("unexpected payload %+v", req)
			}
			return model.DeviceLauncherResultPayload{Action: "rename_agent", Success: true}, nil
		},
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID: "launcher:m_online", MachineID: "m_online", Hostname: "MacBook", Kind: "launcher", Version: "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/rename", strings.NewReader(`{"agent_id":"agent_chat_codex","name":" 逆向专家安卓 "}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	api.RenameDeviceAgent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeviceAgentFilesForwardDirectoryRequests(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	var seen []string
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, deviceID, _ string, envType string, payload any) (model.DeviceDirectoriesResultPayload, error) {
			if deviceID != "launcher:m_online" {
				t.Fatalf("unexpected device id %s", deviceID)
			}
			req, ok := payload.(model.DeviceProjectFilesPayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if req.MachineID != "m_online" || req.AgentID != "agent_chat_codex" {
				t.Fatalf("unexpected payload %+v", req)
			}
			seen = append(seen, envType)
			switch envType {
			case "device.project_files.list":
				if req.Path != "src" {
					t.Fatalf("unexpected list path %q", req.Path)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:      "list",
					CurrentPath: "src",
					Entries: []model.DeviceDirectory{{
						Path:  "src/main.dart",
						Name:  "main.dart",
						Kind:  "文件",
						IsDir: false,
						Size:  12,
					}},
					Success: true,
				}, nil
			case "device.project_files.upload":
				if req.Path != "src/upload.txt" || req.Content != base64.StdEncoding.EncodeToString([]byte("hello")) || req.Encoding != "base64" {
					t.Fatalf("unexpected upload payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "upload",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Size: 5},
					Success: true,
				}, nil
			case "device.project_files.download":
				if req.Path != "src/upload.txt" {
					t.Fatalf("unexpected download path %q", req.Path)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "download",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Content: base64.StdEncoding.EncodeToString([]byte("hello")), Encoding: "base64", Size: 5},
					Success: true,
				}, nil
			case "device.project_files.download_create":
				if req.Path != "src/upload.txt" {
					t.Fatalf("unexpected download create path %q", req.Path)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "download_create",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Size: 5, SHA256: "hash"},
					Success: true,
				}, nil
			case "device.project_files.download_chunk":
				if req.Path != "src/upload.txt" || req.ChunkIndex != 1 || req.Offset != 3 || req.Length != 2 {
					t.Fatalf("unexpected download chunk payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "download_chunk",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Content: "bG8=", Encoding: "base64", Size: 2},
					Success: true,
				}, nil
			case "device.project_files.upload_create":
				if req.Path != "src/upload.txt" || req.UploadID != "upload-test" || req.Size != 5 || req.TotalChunks != 2 {
					t.Fatalf("unexpected upload create payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "upload_create",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Size: 5},
					Success: true,
				}, nil
			case "device.project_files.upload_chunk":
				if req.Path != "src/upload.txt" || req.UploadID != "upload-test" || req.ChunkIndex != 1 || req.Offset != 3 || req.Content != "bG8=" || req.SHA256 == "" {
					t.Fatalf("unexpected upload chunk payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "upload_chunk",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Size: 2},
					Success: true,
				}, nil
			case "device.project_files.upload_complete":
				if req.Path != "src/upload.txt" || req.UploadID != "upload-test" || req.Size != 5 || req.SHA256 == "" {
					t.Fatalf("unexpected upload complete payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "upload_complete",
					File:    &model.DeviceFile{Path: req.Path, Name: "upload.txt", Size: 5},
					Success: true,
				}, nil
			case "device.project_files.create_file":
				if req.Path != "src/new.txt" {
					t.Fatalf("unexpected create file payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "create_file",
					File:    &model.DeviceFile{Path: req.Path, Name: "new.txt", Kind: "文件", IsDir: false},
					Success: true,
				}, nil
			case "device.project_files.mkdir":
				if req.Path != "src/new-folder" {
					t.Fatalf("unexpected mkdir payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "mkdir",
					File:    &model.DeviceFile{Path: req.Path, Name: "new-folder", Kind: "目录", IsDir: true},
					Success: true,
				}, nil
			case "device.project_files.delete":
				if req.Path != "src/old.txt" || req.IsDir {
					t.Fatalf("unexpected delete payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "delete",
					File:    &model.DeviceFile{Path: req.Path, Name: "old.txt", Kind: "文件", IsDir: false},
					Success: true,
				}, nil
			case "device.project_files.rename":
				if req.Path != "src/old.txt" || req.Name != "new.txt" {
					t.Fatalf("unexpected rename payload %+v", req)
				}
				return model.DeviceDirectoriesResultPayload{
					Action:  "rename",
					File:    &model.DeviceFile{Path: "src/new.txt", Name: "new.txt", Kind: "文件", IsDir: false},
					Success: true,
				}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
			}
			return model.DeviceDirectoriesResultPayload{}, nil
		},
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	tests := []struct {
		method string
		url    string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{method: http.MethodGet, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files?path=src", call: api.ListDeviceAgentFiles},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/upload", body: `{"path":"src/upload.txt","content":"aGVsbG8=","encoding":"base64"}`, call: api.UploadDeviceAgentFile},
		{method: http.MethodGet, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/download?path=src%2Fupload.txt", call: api.DownloadDeviceAgentFile},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/upload/create", body: `{"path":"src/upload.txt","upload_id":"upload-test","size":5,"total_chunks":2}`, call: api.CreateDeviceAgentFileUpload},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/upload/chunk", body: `{"path":"src/upload.txt","upload_id":"upload-test","chunk_index":1,"offset":3,"content":"bG8=","encoding":"base64","sha256":"hash"}`, call: api.UploadDeviceAgentFileChunk},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/upload/complete", body: `{"path":"src/upload.txt","upload_id":"upload-test","size":5,"sha256":"hash"}`, call: api.CompleteDeviceAgentFileUpload},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/download/create", body: `{"path":"src/upload.txt"}`, call: api.CreateDeviceAgentFileDownload},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/download/chunk", body: `{"path":"src/upload.txt","chunk_index":1,"offset":3,"length":2}`, call: api.DownloadDeviceAgentFileChunk},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/create", body: `{"path":"src/new.txt"}`, call: api.CreateDeviceAgentEmptyFile},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/folders/create", body: `{"path":"src/new-folder"}`, call: api.CreateDeviceAgentFolder},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/delete", body: `{"path":"src/old.txt","is_dir":false}`, call: api.DeleteDeviceAgentFile},
		{method: http.MethodPost, url: "/api/devices/m_online/launcher/agents/agent_chat_codex/files/rename", body: `{"path":"src/old.txt","name":"new.txt"}`, call: api.RenameDeviceAgentFile},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.url, strings.NewReader(tt.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("machineID", "m_online")
		rctx.URLParams.Add("agentID", "agent_chat_codex")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		tt.call(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s unexpected status: %d body=%s", tt.url, rr.Code, rr.Body.String())
		}
	}
	if strings.Join(seen, ",") != "device.project_files.list,device.project_files.upload,device.project_files.download,device.project_files.upload_create,device.project_files.upload_chunk,device.project_files.upload_complete,device.project_files.download_create,device.project_files.download_chunk,device.project_files.create_file,device.project_files.mkdir,device.project_files.delete,device.project_files.rename" {
		t.Fatalf("unexpected calls: %v", seen)
	}
}

func TestDeviceDirectoryFilesForwardWithoutAgent(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	var seen []string
	api := New(
		mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		func(_ context.Context, _ int64, deviceID, _ string, envType string, payload any) (model.DeviceDirectoriesResultPayload, error) {
			if deviceID != "launcher:m_online" {
				t.Fatalf("unexpected device id %s", deviceID)
			}
			req, ok := payload.(model.DeviceDirectoryFilesPayload)
			if !ok || req.MachineID != "m_online" || req.Path == "" || req.AllowAll {
				t.Fatalf("unexpected payload %#v", payload)
			}
			seen = append(seen, envType)
			return model.DeviceDirectoriesResultPayload{
				Action:  strings.TrimPrefix(envType, "device.directory_files."),
				File:    &model.DeviceFile{Path: req.Path, Name: filepath.Base(req.Path), IsDir: req.IsDir},
				Success: true,
			}, nil
		},
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID: "launcher:m_online", MachineID: "m_online", Hostname: "MacBook", Kind: "launcher",
	}, operator.ID)

	tests := []struct {
		url  string
		body string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"/api/devices/m_online/directories/files/upload/create", `{"path":"/tmp/upload.txt","upload_id":"up-1","size":5,"total_chunks":1}`, api.CreateDeviceDirectoryFileUpload},
		{"/api/devices/m_online/directories/files/upload/chunk", `{"path":"/tmp/upload.txt","upload_id":"up-1","chunk_index":0,"content":"aGVsbG8=","sha256":"hash"}`, api.UploadDeviceDirectoryFileChunk},
		{"/api/devices/m_online/directories/files/upload/complete", `{"path":"/tmp/upload.txt","upload_id":"up-1","size":5,"sha256":"hash"}`, api.CompleteDeviceDirectoryFileUpload},
		{"/api/devices/m_online/directories/files/create", `{"path":"/tmp/new.txt"}`, api.CreateDeviceDirectoryEmptyFile},
		{"/api/devices/m_online/directories/folders/create", `{"path":"/tmp/docs"}`, api.CreateDeviceDirectoryFolder},
		{"/api/devices/m_online/directories/files/delete", `{"path":"/tmp/docs","is_dir":true}`, api.DeleteDeviceDirectoryFile},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, tt.url, strings.NewReader(tt.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("machineID", "m_online")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		tt.call(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s unexpected status: %d body=%s", tt.url, rr.Code, rr.Body.String())
		}
	}
	want := "device.directory_files.upload_create,device.directory_files.upload_chunk,device.directory_files.upload_complete,device.directory_files.create_file,device.directory_files.mkdir,device.directory_files.delete"
	if strings.Join(seen, ",") != want {
		t.Fatalf("unexpected calls: %v", seen)
	}
}

func TestDeviceDirectoryFileMutationReturnsLauncherFailure(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		func(_ context.Context, _ int64, _ string, _ string, _ string, _ any) (model.DeviceDirectoriesResultPayload, error) {
			return model.DeviceDirectoriesResultPayload{
				Success: false,
				Error:   "目标是文件夹",
			}, nil
		},
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID: "launcher:m_online", MachineID: "m_online", Hostname: "MacBook", Kind: "launcher",
	}, operator.ID)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/m_online/directories/files/delete",
		strings.NewReader(`{"path":"/tmp/docs","is_dir":true}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.DeleteDeviceDirectoryFile(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "目标是文件夹") {
		t.Fatalf("launcher error was not returned: %s", rr.Body.String())
	}
}

func TestLauncherModelsIncludesImageModalities(t *testing.T) {
	payload := launcherModels(&model.DeviceAIConfigPreview{
		Provider: "demo",
		Model:    "gpt-image-1",
		Models: []model.DeviceAIModel{{
			ID:      "gpt-image-1",
			Name:    "GPT Image 1",
			Owned:   "demo",
			Context: 128000,
			Variants: map[string]any{
				"low":  map[string]any{},
				"high": map[string]any{},
			},
			Thinking: &model.DeviceAIThinking{
				Supported:           true,
				Source:              "provider",
				Control:             "effort",
				Protocol:            "openrouter",
				SupportedParameters: []string{"reasoning", "reasoning_effort"},
			},
			Modalities: &model.DeviceAIModalities{
				Input:  []string{"text"},
				Output: []string{"image"},
			},
		}},
	})
	all, ok := payload["all"].([]map[string]any)
	if !ok || len(all) != 1 {
		t.Fatalf("expected one provider payload, got %#v", payload["all"])
	}
	models, ok := all[0]["models"].(map[string]any)
	if !ok {
		t.Fatalf("expected provider models map, got %#v", all[0]["models"])
	}
	entry, ok := models["gpt-image-1"].(map[string]any)
	if !ok {
		t.Fatalf("expected model entry, got %#v", models["gpt-image-1"])
	}
	if image, ok := entry["image"].(bool); !ok || !image {
		t.Fatalf("expected image flag true, got %#v", entry["image"])
	}
	if contextLimit, ok := entry["context_limit"].(int64); !ok || contextLimit != 128000 {
		t.Fatalf("expected context limit, got %#v", entry["context_limit"])
	}
	variants, ok := entry["variants"].(map[string]any)
	if !ok || len(variants) != 2 {
		t.Fatalf("expected explicit variants, got %#v", entry["variants"])
	}
	thinking, ok := entry["thinking"].(*model.DeviceAIThinking)
	if !ok || thinking.Source != "provider" || thinking.Protocol != "openrouter" {
		t.Fatalf("expected thinking metadata, got %#v", entry["thinking"])
	}
	limit, ok := entry["limit"].(map[string]any)
	if !ok || limit["context"] != int64(128000) {
		t.Fatalf("expected limit.context, got %#v", entry["limit"])
	}
	modalities, ok := entry["modalities"].(map[string]any)
	if !ok {
		t.Fatalf("expected modalities map, got %#v", entry["modalities"])
	}
	output, ok := modalities["output"].([]string)
	if !ok || len(output) != 1 || output[0] != "image" {
		t.Fatalf("expected image output modalities, got %#v", modalities["output"])
	}
}

func TestLauncherModelsIncludesAllProviders(t *testing.T) {
	payload := launcherModels(&model.DeviceAIConfigPreview{
		Provider: "longent",
		Model:    "longent/gpt-5.4",
		Providers: []model.DeviceAIProvider{
			{
				ID:         "longent",
				BaseURL:    "https://api.example.com/v1",
				ConsoleURL: "https://console.example.com",
				Models: []model.DeviceAIModel{{
					ID:   "gpt-5.4",
					Name: "gpt-5.4",
				}},
			},
			{
				ID: "longent-grok",
				Models: []model.DeviceAIModel{{
					ID:   "grok-4",
					Name: "grok-4",
				}},
			},
		},
	})
	connected, ok := payload["connected"].([]string)
	if !ok || len(connected) != 2 {
		t.Fatalf("expected 2 connected providers, got %#v", payload["connected"])
	}
	all, ok := payload["all"].([]map[string]any)
	if !ok || len(all) != 2 {
		t.Fatalf("expected 2 provider payloads, got %#v", payload["all"])
	}
	if all[0]["id"] != "longent" || all[1]["id"] != "longent-grok" {
		t.Fatalf("expected both providers in payload, got %#v", all)
	}
	if all[0]["base_url"] != "https://api.example.com/v1" || all[0]["console_url"] != "https://console.example.com" {
		t.Fatalf("expected provider urls in payload, got %#v", all[0])
	}
	if all[0]["api_mode"] != "responses" || all[1]["api_mode"] != "chat" {
		t.Fatalf("expected missing provider api mode to follow model family, got %#v", all)
	}
	models, ok := all[1]["models"].(map[string]any)
	if !ok {
		t.Fatalf("expected second provider models map, got %#v", all[1]["models"])
	}
	if _, ok := models["grok-4"].(map[string]any); !ok {
		t.Fatalf("expected grok model entry, got %#v", models)
	}
	grokEntry := models["grok-4"].(map[string]any)
	if grokEntry["context_limit"] != int64(256000) {
		t.Fatalf("expected grok-4 inferred context, got %#v", grokEntry["context_limit"])
	}
	firstModels := all[0]["models"].(map[string]any)
	gptEntry := firstModels["gpt-5.4"].(map[string]any)
	if _, ok := gptEntry["variants"]; ok {
		t.Fatalf("expected gpt model without explicit variants to stay empty, got %#v", gptEntry["variants"])
	}
}

func TestLauncherModelsInfersKunContextForGrokProvider(t *testing.T) {
	payload := launcherModels(&model.DeviceAIConfigPreview{
		Provider: "订阅grok",
		Model:    "Kun",
		Providers: []model.DeviceAIProvider{{
			ID: "订阅grok",
			Models: []model.DeviceAIModel{
				{ID: "Kun", Name: "Kun"},
				{ID: "deepseek-v4.1-flash", Name: "deepseek-v4.1-flash"},
			},
		}},
	})
	all, ok := payload["all"].([]map[string]any)
	if !ok || len(all) != 1 {
		t.Fatalf("expected one provider payload, got %#v", payload["all"])
	}
	models, ok := all[0]["models"].(map[string]any)
	if !ok {
		t.Fatalf("expected provider models map, got %#v", all[0]["models"])
	}
	kun, ok := models["Kun"].(map[string]any)
	if !ok {
		t.Fatalf("expected Kun entry, got %#v", models)
	}
	if kun["context_limit"] != int64(500000) {
		t.Fatalf("expected inferred Kun context, got %#v", kun["context_limit"])
	}
	limit, ok := kun["limit"].(map[string]any)
	if !ok || limit["context"] != int64(500000) {
		t.Fatalf("expected inferred Kun limit.context, got %#v", kun["limit"])
	}
	deepseek, ok := models["deepseek-v4.1-flash"].(map[string]any)
	if !ok {
		t.Fatalf("expected deepseek entry, got %#v", models)
	}
	if _, exists := deepseek["context_limit"]; exists {
		t.Fatalf("expected unrelated model to stay empty, got %#v", deepseek["context_limit"])
	}
}

func TestNormalizeDeviceAIModelsInfersKunContext(t *testing.T) {
	got := normalizeDeviceAIModels("订阅grok", []model.DeviceAIModel{
		{ID: "Kun", Name: "Kun"},
		{ID: "deepseek-v4.1-flash", Name: "deepseek-v4.1-flash"},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %+v", got)
	}
	if got[0].Owned != "订阅grok" || got[0].Context != 500000 {
		t.Fatalf("expected inferred Kun context, got %+v", got[0])
	}
	if got[1].Context != 0 {
		t.Fatalf("expected unrelated model to stay empty, got %+v", got[1])
	}

	unrelated := normalizeDeviceAIModels("custom", []model.DeviceAIModel{{ID: "Kun", Name: "Kun"}})
	if len(unrelated) != 1 || unrelated[0].Context != 0 {
		t.Fatalf("expected unrelated Kun to stay empty, got %+v", unrelated)
	}
}

func TestGetDeviceAIConfigInfersKunContextBeforeReturning(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, _ any) (model.DeviceAIConfigResultPayload, error) {
			if envType != "device.ai_config.get" {
				t.Fatalf("unexpected ai config env type %s", envType)
			}
			return model.DeviceAIConfigResultPayload{
				Action:  "get",
				Success: true,
				Config: &model.DeviceAIConfigPreview{
					Provider: "订阅grok",
					Model:    "Kun",
					Providers: []model.DeviceAIProvider{{
						ID: "订阅grok",
						Models: []model.DeviceAIModel{
							{ID: "Kun", Name: "Kun"},
							{ID: "deepseek-v4.1-flash", Name: "deepseek-v4.1-flash"},
						},
					}},
				},
			}, nil
		},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/ai-config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{
		URLParams: chi.RouteParams{Keys: []string{"machineID"}, Values: []string{"m_online"}},
	}))
	rr := httptest.NewRecorder()
	api.GetDeviceAIConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var payload model.DeviceAIConfigResultPayload
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Config == nil || len(payload.Config.Providers) != 1 {
		t.Fatalf("expected grok provider in response, got %+v", payload.Config)
	}
	models := payload.Config.Providers[0].Models
	if len(models) != 2 || models[0].ID != "Kun" || models[0].Context != 500000 {
		t.Fatalf("expected inferred Kun context in get response, got %+v", models)
	}
	if models[1].Context != 0 {
		t.Fatalf("expected unrelated model to stay empty, got %+v", models[1])
	}

	cache, ok := b.GetModelCache(operator.ID, "m_online")
	if !ok || cache.Payload == nil {
		t.Fatalf("expected model cache after get")
	}
	all, ok := cache.Payload["all"].([]map[string]any)
	if !ok || len(all) != 1 {
		t.Fatalf("expected cached provider payload, got %#v", cache.Payload["all"])
	}
	cachedModels, ok := all[0]["models"].(map[string]any)
	if !ok {
		t.Fatalf("expected cached models map, got %#v", all[0]["models"])
	}
	kun, ok := cachedModels["Kun"].(map[string]any)
	if !ok || kun["context_limit"] != int64(500000) {
		t.Fatalf("expected cached Kun context, got %#v", cachedModels["Kun"])
	}
}

func TestLauncherModelsDerivesConsoleURLFromBaseURL(t *testing.T) {
	payload := launcherModels(&model.DeviceAIConfigPreview{
		Providers: []model.DeviceAIProvider{{
			ID:      "openrouter",
			BaseURL: "https://openrouter.ai/api/v1?token=ignored",
			Models: []model.DeviceAIModel{{
				ID: "stealth/ox-alpha",
			}},
		}},
	})

	all, ok := payload["all"].([]map[string]any)
	if !ok || len(all) != 1 {
		t.Fatalf("expected one provider payload, got %#v", payload["all"])
	}
	if got := all[0]["console_url"]; got != "https://openrouter.ai" {
		t.Fatalf("expected API origin as console URL, got %#v", got)
	}
}

func TestDeriveProviderConsoleURLKeepsExplicitURL(t *testing.T) {
	if got := deriveProviderConsoleURL(
		"https://console.example.com/account",
		"https://api.example.com/v1",
	); got != "https://console.example.com/account" {
		t.Fatalf("expected explicit console URL, got %q", got)
	}
}

func TestSaveDeviceAIConfigRefreshesMachineModelCache(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceAIConfigResultPayload, error) {
			if envType != "device.ai_config.save" && envType != "device.ai_config.get" {
				t.Fatalf("unexpected ai config env type %s", envType)
			}
			if envType == "device.ai_config.save" {
				req, ok := payload.(model.DeviceAIConfigSavePayload)
				if !ok {
					t.Fatalf("unexpected ai config payload %#v", payload)
				}
				if req.APIMode != "responses" || req.Config.APIMode != "responses" {
					t.Fatalf("expected responses api mode to be forwarded, got %+v", req)
				}
			}
			return model.DeviceAIConfigResultPayload{
				Action:  "save",
				Success: true,
				Config: &model.DeviceAIConfigPreview{
					Provider: "超级便宜",
					APIMode:  "responses",
					Model:    "超级便宜/gpt-5.4",
					Providers: []model.DeviceAIProvider{{
						ID:      "超级便宜",
						APIMode: "responses",
						Models: []model.DeviceAIModel{{
							ID:      "claude-opus-4-6",
							Name:    "claude-opus-4-6",
							Context: 200000,
							Variants: map[string]any{
								"low":    map[string]any{},
								"medium": map[string]any{},
								"high":   map[string]any{},
							},
						}},
					}},
				},
			}, nil
		},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	saveReq := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/ai-config/save", strings.NewReader(`{
		"provider":"超级便宜",
		"base_url":"https://example.com/v1",
		"api_mode":"responses",
		"model":"claude-opus-4-6",
		"models":[{
			"id":"claude-opus-4-6",
			"name":"claude-opus-4-6",
			"context_limit":200000,
			"variants":{"low":{},"medium":{},"high":{}}
		}],
		"force":true
	}`))
	saveReq.Header.Set("Authorization", "Bearer "+token)
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	saveCtx := chi.NewRouteContext()
	saveCtx.URLParams.Add("machineID", "m_online")
	saveReq = saveReq.WithContext(context.WithValue(saveReq.Context(), chi.RouteCtxKey, saveCtx))

	api.SaveDeviceAIConfig(saveRR, saveReq)

	if saveRR.Code != http.StatusOK {
		t.Fatalf("unexpected save status: %d body=%s", saveRR.Code, saveRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/models?machine_id=m_online", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRR := httptest.NewRecorder()
	api.ListModels(listRR, listReq)

	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected models status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(listRR.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	connected := payload["connected"].([]any)
	if len(connected) != 1 || connected[0] != "超级便宜" {
		t.Fatalf("expected refreshed provider in cache, got %#v", payload["connected"])
	}
	all := payload["all"].([]any)
	if len(all) != 1 {
		t.Fatalf("expected one provider, got %#v", payload["all"])
	}
	provider := all[0].(map[string]any)
	models := provider["models"].(map[string]any)
	entry, ok := models["claude-opus-4-6"].(map[string]any)
	if !ok {
		t.Fatalf("expected claude-opus-4-6 in refreshed models, got %#v", models)
	}
	variants, ok := entry["variants"].(map[string]any)
	if !ok || len(variants) != 3 {
		t.Fatalf("expected claude variants in refreshed models, got %#v", entry["variants"])
	}
}

func TestListModelsWithMachineIDRefreshesBeforeUsingCache(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	calls := 0
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, _ any) (model.DeviceAIConfigResultPayload, error) {
			calls++
			if envType != "device.ai_config.get" {
				t.Fatalf("unexpected ai config env type %s", envType)
			}
			return model.DeviceAIConfigResultPayload{
				Action:  "get",
				Success: true,
				Config: &model.DeviceAIConfigPreview{
					Provider: "性价比日卡",
					Model:    "性价比日卡/claude-opus-4-6",
					Providers: []model.DeviceAIProvider{{
						ID:      "性价比日卡",
						APIMode: "chat",
						Models: []model.DeviceAIModel{{
							ID:      "claude-opus-4-6",
							Name:    "claude-opus-4-6",
							Context: 200000,
							Variants: map[string]any{
								"low":    map[string]any{},
								"medium": map[string]any{},
								"high":   map[string]any{},
							},
						}},
					}},
				},
			}, nil
		},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)
	b.SetModelCache(operator.ID, "m_online", map[string]any{
		"connected": []string{"old"},
		"all": []map[string]any{{
			"id":     "old",
			"models": map[string]any{},
		}},
	})

	listReq := httptest.NewRequest(http.MethodGet, "/api/models?machine_id=m_online", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRR := httptest.NewRecorder()
	api.ListModels(listRR, listReq)

	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected models status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	if calls != 1 {
		t.Fatalf("expected live launcher refresh before cache, got calls=%d", calls)
	}
	var payload map[string]any
	if err := json.Unmarshal(listRR.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	all := payload["all"].([]any)
	provider := all[0].(map[string]any)
	if provider["id"] != "性价比日卡" {
		t.Fatalf("expected refreshed provider, got %#v", provider["id"])
	}
	models := provider["models"].(map[string]any)
	entry := models["claude-opus-4-6"].(map[string]any)
	variants, ok := entry["variants"].(map[string]any)
	if !ok || len(variants) != 3 {
		t.Fatalf("expected refreshed variants, got %#v", entry["variants"])
	}
}

func TestGetDeviceMCPConfigReturnsLauncherPayload(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, _ any) (model.DeviceMCPConfigResultPayload, error) {
			if envType != "device.mcp_config.get" {
				t.Fatalf("unexpected env type %s", envType)
			}
			return model.DeviceMCPConfigResultPayload{
				Action:  "get",
				Success: true,
				Config: &model.DeviceMCPConfigPreview{
					ConfigPath: "/home/test/.config/opencode/opencode.json",
					Servers: []model.DeviceMCPServer{{
						Name:    "jira",
						Type:    "remote",
						Enabled: true,
						URL:     "https://jira.example.com/mcp",
					}},
				},
			}, nil
		},
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/mcp-config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.GetDeviceMCPConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var payload model.DeviceMCPConfigResultPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Config == nil || len(payload.Config.Servers) != 1 {
		t.Fatalf("expected one mcp server, got %+v", payload.Config)
	}
	if payload.Config.Servers[0].Name != "jira" {
		t.Fatalf("unexpected server payload: %+v", payload.Config.Servers[0])
	}
}

func TestRuntimePreflightHTTPFlowCreatesDispatchAndPersistsStatus(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := mem.UpsertRuntimeCatalogItem(model.RuntimeCatalogItem{
		ID:              "node",
		Name:            "Node.js",
		RuntimeKind:     "archive",
		InstallStrategy: "archive",
		Enabled:         true,
		Versions: []model.RuntimeVersion{{
			ID:              "node-22",
			Version:         "22.16.0",
			DefaultSelected: true,
			Enabled:         true,
		}},
	}); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if _, err := mem.UpsertMCPCatalogItem(model.MCPCatalogItem{
		ID:          "frida-mcp",
		Name:        "frida-mcp",
		Title:       "Frida MCP",
		Description: "用于测试 MCP 预检项",
		Type:        "local",
		Recommended: true,
		Enabled:     true,
		LaunchReady: true,
		Config: model.DeviceMCPServer{
			Name:    "frida-mcp",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "frida-mcp"},
		},
	}); err != nil {
		t.Fatalf("seed mcp catalog: %v", err)
	}
	if _, err := api.upsertSemanticAgent(model.SemanticAgentProfile{
		ID:      "reverse-test",
		Name:    "逆向专家测试",
		Prompt:  "按逆向专家流程工作。",
		Enabled: true,
		MCPIDs:  []string{"frida-mcp"},
		RecommendedMCPServers: []string{
			"Frida MCP",
		},
		RuntimeRequirements: []model.SemanticAgentRuntime{{
			RuntimeID:         "node",
			VersionConstraint: ">=22 <27",
			Required:          true,
			Purpose:           "运行 MCP",
		}},
	}); err != nil {
		t.Fatalf("seed semantic agent: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:   "launcher:m_online",
			MachineID: "m_online",
			Hostname:  "MacBook",
			Kind:      "launcher",
			Version:   "launcher-test",
		}, operator.ID)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial launcher websocket: %v", err)
	}
	defer conn.CloseNow()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if _, ok := b.Get("launcher:m_online", operator.ID); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := b.Get("launcher:m_online", operator.ID); !ok {
		t.Fatal("launcher was not registered")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/agent_chat_codex/semantic-agent/preflight", strings.NewReader(`{
		"semantic_agent_id": "reverse-test",
		"apply_recommended_mcp": true,
		"auto_repair": true,
		"restart_after_apply": true
	}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	rctx.URLParams.Add("agentID", "agent_chat_codex")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	api.StartDeviceAgentSemanticPreflight(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected preflight status: %d body=%s", rr.Code, rr.Body.String())
	}
	var startResp struct {
		JobID string                  `json:"job_id"`
		Job   model.RuntimeInstallJob `json:"job"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("decode preflight response: %v", err)
	}
	if startResp.JobID == "" || startResp.Job.Status != "running" {
		t.Fatalf("expected running job, got %+v", startResp)
	}
	if !hasRuntimePreflightItem(startResp.Job.Items, "mcp", "frida-mcp") {
		t.Fatalf("expected initial mcp preflight item, got %+v", startResp.Job.Items)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read launcher dispatch: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode dispatch envelope: %v", err)
	}
	if env.Type != "device.launcher.agent_preflight.start" || env.RequestID != startResp.JobID {
		t.Fatalf("unexpected dispatch envelope: %+v", env)
	}
	var payload model.RuntimePreflightStartPayload
	rawPayload, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal dispatch payload: %v", err)
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		t.Fatalf("decode dispatch payload: %v", err)
	}
	if payload.SemanticAgentID != "reverse-test" || payload.SemanticAgent == nil || len(payload.Runtimes) != 1 {
		t.Fatalf("unexpected preflight payload: %+v", payload)
	}
	if len(payload.SemanticAgent.MCPDependencies) != 1 ||
		payload.SemanticAgent.MCPDependencies[0].ID != "frida-mcp" ||
		len(payload.SemanticAgent.MCPDependencies[0].Config.Command) != 3 {
		t.Fatalf("expected mcp dependency snapshot in preflight payload: %+v", payload.SemanticAgent.MCPDependencies)
	}

	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID:           startResp.JobID,
		MachineID:       "m_online",
		LauncherAgentID: "agent_chat_codex",
		SemanticAgentID: "reverse-test",
		Status:          "completed",
		CurrentStep:     "apply",
		ProgressPercent: 100,
		Items: []model.RuntimeInstallJobItem{{
			JobID:           startResp.JobID,
			ItemType:        "runtime",
			ItemID:          "node",
			Name:            "Node.js",
			Required:        true,
			Status:          "completed",
			ProgressPercent: 100,
		}},
		Events: []model.RuntimeInstallEvent{{
			ItemType:        "runtime",
			ItemID:          "node",
			Phase:           "verify",
			Status:          "success",
			Message:         "Node.js 已验证",
			ProgressPercent: 100,
		}},
	}); err != nil {
		t.Fatalf("handle status: %v", err)
	}

	jobReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/preflight-jobs/"+startResp.JobID, nil)
	jobReq.Header.Set("Authorization", "Bearer "+token)
	jobCtx := chi.NewRouteContext()
	jobCtx.URLParams.Add("jobID", startResp.JobID)
	jobReq = jobReq.WithContext(context.WithValue(jobReq.Context(), chi.RouteCtxKey, jobCtx))
	jobRR := httptest.NewRecorder()
	api.GetDeviceAgentSemanticPreflightJob(jobRR, jobReq)
	if jobRR.Code != http.StatusOK {
		t.Fatalf("unexpected job status: %d body=%s", jobRR.Code, jobRR.Body.String())
	}
	var jobResp struct {
		Job model.RuntimeInstallJob `json:"job"`
	}
	if err := json.Unmarshal(jobRR.Body.Bytes(), &jobResp); err != nil {
		t.Fatalf("decode job response: %v", err)
	}
	if jobResp.Job.Status != "completed" || jobResp.Job.ProgressPercent != 100 || len(jobResp.Job.Items) != 1 {
		t.Fatalf("unexpected stored job: %+v", jobResp.Job)
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/preflight-jobs/"+startResp.JobID+"/events", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+token)
	eventsCtx := chi.NewRouteContext()
	eventsCtx.URLParams.Add("jobID", startResp.JobID)
	eventsReq = eventsReq.WithContext(context.WithValue(eventsReq.Context(), chi.RouteCtxKey, eventsCtx))
	eventsRR := httptest.NewRecorder()
	api.GetDeviceAgentSemanticPreflightEvents(eventsRR, eventsReq)
	if eventsRR.Code != http.StatusOK {
		t.Fatalf("unexpected events status: %d body=%s", eventsRR.Code, eventsRR.Body.String())
	}
	var eventsResp struct {
		Items []model.RuntimeInstallEvent `json:"items"`
	}
	if err := json.Unmarshal(eventsRR.Body.Bytes(), &eventsResp); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(eventsResp.Items) < 2 || eventsResp.Items[len(eventsResp.Items)-1].Message != "Node.js 已验证" {
		t.Fatalf("unexpected events: %+v", eventsResp.Items)
	}
}

func TestSaveDeviceMCPConfigForwardsServers(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceMCPConfigResultPayload, error) {
			if envType != "device.mcp_config.save" {
				t.Fatalf("unexpected env type %s", envType)
			}
			info, ok := payload.(model.DeviceMCPConfigSavePayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if len(info.Servers) != 1 || info.Servers[0].Name != "docs" {
				t.Fatalf("unexpected forwarded servers %+v", info.Servers)
			}
			return model.DeviceMCPConfigResultPayload{
				Action:  "save",
				Success: true,
				Config: &model.DeviceMCPConfigPreview{
					Servers: info.Servers,
				},
			}, nil
		},
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/m_online/mcp-config/save",
		strings.NewReader(`{"servers":[{"name":"docs","type":"remote","enabled":true,"url":"https://docs.example.com/mcp"}]}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.SaveDeviceMCPConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeviceAgentMCPSelectionForwardsPayload(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceMCPConfigResultPayload, error) {
			info, ok := payload.(model.DeviceAgentMCPSelectionPayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if info.AgentID != "agent_chat_codex" {
				t.Fatalf("unexpected agent id %q", info.AgentID)
			}
			switch envType {
			case "device.mcp_config.agent_get":
				return model.DeviceMCPConfigResultPayload{
					Action:  "agent_get",
					Success: true,
					Selection: &model.DeviceAgentMCPSelection{
						AgentID:         info.AgentID,
						Mode:            "inherit",
						SelectedServers: []string{"docs"},
					},
				}, nil
			case "device.mcp_config.agent_save":
				if info.Mode != "custom" || len(info.Servers) != 2 || info.Servers[0] != "docs" || info.Servers[1] != "verify" {
					t.Fatalf("unexpected save payload %+v", info)
				}
				return model.DeviceMCPConfigResultPayload{
					Action:  "agent_save",
					Success: true,
					Selection: &model.DeviceAgentMCPSelection{
						AgentID:         info.AgentID,
						Mode:            info.Mode,
						SelectedServers: info.Servers,
					},
				}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
				return model.DeviceMCPConfigResultPayload{}, nil
			}
		},
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	getReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/agents/agent_chat_codex/mcp-selection", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getRR := httptest.NewRecorder()
	getCtx := chi.NewRouteContext()
	getCtx.URLParams.Add("machineID", "m_online")
	getCtx.URLParams.Add("agentID", "agent_chat_codex")
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), chi.RouteCtxKey, getCtx))

	api.GetDeviceAgentMCPSelection(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("unexpected get status: %d body=%s", getRR.Code, getRR.Body.String())
	}
	var getPayload model.DeviceMCPConfigResultPayload
	if err := json.Unmarshal(getRR.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getPayload.Selection == nil || getPayload.Selection.Mode != "inherit" {
		t.Fatalf("unexpected get selection %+v", getPayload.Selection)
	}

	saveReq := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/m_online/launcher/agents/agent_chat_codex/mcp-selection",
		strings.NewReader(`{"mode":"custom","servers":["docs","verify"]}`),
	)
	saveReq.Header.Set("Authorization", "Bearer "+token)
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	saveCtx := chi.NewRouteContext()
	saveCtx.URLParams.Add("machineID", "m_online")
	saveCtx.URLParams.Add("agentID", "agent_chat_codex")
	saveReq = saveReq.WithContext(context.WithValue(saveReq.Context(), chi.RouteCtxKey, saveCtx))

	api.SaveDeviceAgentMCPSelection(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("unexpected save status: %d body=%s", saveRR.Code, saveRR.Body.String())
	}
	var savePayload model.DeviceMCPConfigResultPayload
	if err := json.Unmarshal(saveRR.Body.Bytes(), &savePayload); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if savePayload.Selection == nil || savePayload.Selection.Mode != "custom" || len(savePayload.Selection.SelectedServers) != 2 {
		t.Fatalf("unexpected save selection %+v", savePayload.Selection)
	}
}

func TestDeviceAgentSkillSelectionUsesDeviceDefinitions(t *testing.T) {
	mem := store.NewMemory(nil)
	if _, err := mem.UpsertSkill(model.Skill{
		ID: "custom-review", Name: "custom-review", Description: "审查流程",
		Content: "# Review", Enabled: true,
	}); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			info, ok := payload.(model.DeviceAgentSkillSelectionPayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if envType == "device.launcher.skills_save" {
				if len(info.ExtraSkillIDs) != 1 || info.ExtraSkillIDs[0] != "custom-review" {
					t.Fatalf("unexpected selected ids %+v", info.ExtraSkillIDs)
				}
				if len(info.Skills) != 0 {
					t.Fatalf("expected no remote skill definitions, got %+v", info.Skills)
				}
			}
			return model.DeviceLauncherResultPayload{
				Action:  envType,
				Success: true,
				SkillSelection: &model.DeviceAgentSkillSelection{
					AgentID: "agent_chat_codex", ExtraSkills: info.ExtraSkillIDs,
					EffectiveSkills: info.ExtraSkillIDs,
					AvailableSkills: info.Skills,
				},
			}, nil
		},
	})
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID: "launcher:m_online", MachineID: "m_online", Hostname: "MacBook", Kind: "launcher",
	}, operator.ID)

	getReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/agents/agent_chat_codex/skills", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), chi.RouteCtxKey, routeContext("agentID", "agent_chat_codex")))
	getCtx := chi.RouteContext(getReq.Context())
	getCtx.URLParams.Add("machineID", "m_online")
	getRR := httptest.NewRecorder()
	api.GetDeviceAgentSkillSelection(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("unexpected get status: %d body=%s", getRR.Code, getRR.Body.String())
	}
	var getPayload model.DeviceLauncherResultPayload
	if err := json.Unmarshal(getRR.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getPayload.SkillSelection == nil {
		t.Fatalf("expected launcher skill selection response")
	}

	saveReq := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/agent_chat_codex/skills", strings.NewReader(`{"extra_skill_ids":["custom-review"]}`))
	saveReq.Header.Set("Authorization", "Bearer "+token)
	saveReq.Header.Set("Content-Type", "application/json")
	saveReq = saveReq.WithContext(context.WithValue(saveReq.Context(), chi.RouteCtxKey, routeContext("agentID", "agent_chat_codex")))
	saveCtx := chi.RouteContext(saveReq.Context())
	saveCtx.URLParams.Add("machineID", "m_online")
	saveRR := httptest.NewRecorder()
	api.SaveDeviceAgentSkillSelection(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("unexpected save status: %d body=%s", saveRR.Code, saveRR.Body.String())
	}
}

func TestDeviceAgentSemanticSelectionForwardsPayload(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			info, ok := payload.(model.DeviceAgentSemanticSelectionPayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if info.AgentID != "agent_chat_codex" {
				t.Fatalf("unexpected agent id %q", info.AgentID)
			}
			switch envType {
			case "device.launcher.semantic_agent_get":
				return model.DeviceLauncherResultPayload{
					Action:  "semantic_agent_get",
					Success: true,
					SemanticSelection: &model.DeviceAgentSemanticSelection{
						AgentID:         info.AgentID,
						SemanticAgentID: "coding-assistant",
					},
				}, nil
			case "device.launcher.semantic_agent_save":
				if info.SemanticAgentID != "reverse-android" || !info.ApplyRecommendedMCP {
					t.Fatalf("unexpected save payload %+v", info)
				}
				if info.SemanticAgent == nil || info.SemanticAgent.ID != "reverse-android" || strings.TrimSpace(info.SemanticAgent.Prompt) == "" {
					t.Fatalf("expected reverse expert profile snapshot, got %+v", info.SemanticAgent)
				}
				if len(info.SemanticAgent.SkillDefinitions) == 0 ||
					len(info.SemanticAgent.SkillIDs) == 0 ||
					len(info.SemanticAgent.RecommendedMCPServers) == 0 {
					t.Fatalf("expected built-in skill and MCP bindings, got %+v", info.SemanticAgent)
				}
				return model.DeviceLauncherResultPayload{
					Action:  "semantic_agent_save",
					Success: true,
					SemanticSelection: &model.DeviceAgentSemanticSelection{
						AgentID:         info.AgentID,
						SemanticAgentID: info.SemanticAgentID,
					},
				}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
				return model.DeviceLauncherResultPayload{}, nil
			}
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	getReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/agents/agent_chat_codex/semantic-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getRR := httptest.NewRecorder()
	getCtx := chi.NewRouteContext()
	getCtx.URLParams.Add("machineID", "m_online")
	getCtx.URLParams.Add("agentID", "agent_chat_codex")
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), chi.RouteCtxKey, getCtx))

	api.GetDeviceAgentSemanticSelection(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("unexpected get status: %d body=%s", getRR.Code, getRR.Body.String())
	}
	var getPayload model.DeviceLauncherResultPayload
	if err := json.Unmarshal(getRR.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getPayload.SemanticSelection == nil || getPayload.SemanticSelection.SemanticAgentID != "coding-assistant" {
		t.Fatalf("unexpected get selection %+v", getPayload.SemanticSelection)
	}
	if len(getPayload.SemanticSelection.AvailableAgents) != 5 {
		t.Fatalf("expected backend semantic agent catalog, got %+v", getPayload.SemanticSelection.AvailableAgents)
	}

	saveReq := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/m_online/launcher/agents/agent_chat_codex/semantic-agent",
		strings.NewReader(`{"semantic_agent_id":"reverse-android","apply_recommended_mcp":true}`),
	)
	saveReq.Header.Set("Authorization", "Bearer "+token)
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	saveCtx := chi.NewRouteContext()
	saveCtx.URLParams.Add("machineID", "m_online")
	saveCtx.URLParams.Add("agentID", "agent_chat_codex")
	saveReq = saveReq.WithContext(context.WithValue(saveReq.Context(), chi.RouteCtxKey, saveCtx))

	api.SaveDeviceAgentSemanticSelection(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("unexpected save status: %d body=%s", saveRR.Code, saveRR.Body.String())
	}
	var savePayload model.DeviceLauncherResultPayload
	if err := json.Unmarshal(saveRR.Body.Bytes(), &savePayload); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if savePayload.SemanticSelection == nil || savePayload.SemanticSelection.SemanticAgentID != "reverse-android" {
		t.Fatalf("unexpected save selection %+v", savePayload.SemanticSelection)
	}
	if savePayload.SemanticSelection.Profile == nil || savePayload.SemanticSelection.Profile.Name != "逆向专家-安卓" {
		t.Fatalf("expected enriched reverse profile, got %+v", savePayload.SemanticSelection.Profile)
	}
}

func TestRuntimePreflightStatusPersistsJobItemsAndEvents(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	now := time.Now().UTC()
	job, err := mem.UpsertRuntimeInstallJob(model.RuntimeInstallJob{
		ID:              "preflight_test",
		OperatorID:      operator.ID,
		MachineID:       "m_online",
		LauncherAgentID: "agent_chat_codex",
		SemanticAgentID: "reverse-expert",
		Status:          "running",
		CurrentStep:     "runtime",
		StartedAt:       now,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := mem.ReplaceRuntimeInstallJobItems(job.ID, []model.RuntimeInstallJobItem{{
		JobID:    job.ID,
		ItemType: "runtime",
		ItemID:   "python",
		Name:     "Python",
		Required: true,
		Status:   "pending",
	}}); err != nil {
		t.Fatalf("replace items: %v", err)
	}

	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID:           job.ID,
		MachineID:       "m_online",
		LauncherAgentID: "agent_chat_codex",
		SemanticAgentID: "reverse-expert",
		Status:          "completed",
		CurrentStep:     "completed",
		ProgressPercent: 100,
		Items: []model.RuntimeInstallJobItem{{
			JobID:           job.ID,
			ItemType:        "runtime",
			ItemID:          "python",
			Name:            "Python",
			Required:        true,
			Status:          "completed",
			ProgressPercent: 100,
		}},
		Events: []model.RuntimeInstallEvent{{
			ItemType:        "runtime",
			ItemID:          "python",
			Phase:           "verify",
			Status:          "success",
			Message:         "Python 已就绪",
			ProgressPercent: 100,
		}},
	}); err != nil {
		t.Fatalf("handle status: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/preflight-jobs/preflight_test", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getCtx := chi.NewRouteContext()
	getCtx.URLParams.Add("jobID", job.ID)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), chi.RouteCtxKey, getCtx))
	getRR := httptest.NewRecorder()

	api.GetDeviceAgentSemanticPreflightJob(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("unexpected get status: %d body=%s", getRR.Code, getRR.Body.String())
	}
	var getPayload struct {
		Job model.RuntimeInstallJob `json:"job"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if getPayload.Job.Status != "completed" || len(getPayload.Job.Items) != 1 || getPayload.Job.Items[0].Status != "completed" {
		t.Fatalf("unexpected job payload: %+v", getPayload.Job)
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/launcher/preflight-jobs/preflight_test/events", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+token)
	eventsCtx := chi.NewRouteContext()
	eventsCtx.URLParams.Add("jobID", job.ID)
	eventsReq = eventsReq.WithContext(context.WithValue(eventsReq.Context(), chi.RouteCtxKey, eventsCtx))
	eventsRR := httptest.NewRecorder()

	api.GetDeviceAgentSemanticPreflightEvents(eventsRR, eventsReq)
	if eventsRR.Code != http.StatusOK {
		t.Fatalf("unexpected events status: %d body=%s", eventsRR.Code, eventsRR.Body.String())
	}
	var eventsPayload struct {
		Items []model.RuntimeInstallEvent `json:"items"`
	}
	if err := json.Unmarshal(eventsRR.Body.Bytes(), &eventsPayload); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(eventsPayload.Items) != 1 || eventsPayload.Items[0].Message != "Python 已就绪" {
		t.Fatalf("unexpected events payload: %+v", eventsPayload.Items)
	}
}

func TestRuntimePreflightStatusDoesNotRegressRunningProgress(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	job, err := mem.UpsertRuntimeInstallJob(model.RuntimeInstallJob{
		ID:              "preflight_progress",
		OperatorID:      1,
		MachineID:       "m_online",
		LauncherAgentID: "agent_chat_codex",
		SemanticAgentID: "reverse-expert",
		Status:          "running",
		CurrentStep:     "runtime",
		ProgressPercent: 3,
		StartedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID:           job.ID,
		Status:          "running",
		CurrentStep:     "runtime",
		ProgressPercent: 2,
	}); err != nil {
		t.Fatalf("handle status: %v", err)
	}
	stored, ok := mem.GetRuntimeInstallJob(1, job.ID)
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.ProgressPercent != 3 {
		t.Fatalf("expected progress to stay at 3, got %d", stored.ProgressPercent)
	}
}

func TestRuntimePreflightStatusDoesNotReturnToPending(t *testing.T) {
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), auth.NewManager(time.Hour), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	job, err := mem.UpsertRuntimeInstallJob(model.RuntimeInstallJob{
		ID:              "preflight_pending_guard",
		OperatorID:      1,
		MachineID:       "m_online",
		LauncherAgentID: "agent_chat_codex",
		SemanticAgentID: "reverse-expert",
		Status:          "running",
		CurrentStep:     "mcp",
		ProgressPercent: 45,
		StartedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID: job.ID, Status: "pending", CurrentStep: "runtime", ProgressPercent: 0,
	}); err != nil {
		t.Fatalf("handle stale pending status: %v", err)
	}
	stored, _ := mem.GetRuntimeInstallJob(1, job.ID)
	if stored.Status != "running" || stored.CurrentStep != "mcp" || stored.ProgressPercent != 45 {
		t.Fatalf("running state returned to pending: %+v", stored)
	}
}

func TestRuntimePreflightStatusDoesNotRegressStepOrTerminalState(t *testing.T) {
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), auth.NewManager(time.Hour), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	job, err := mem.UpsertRuntimeInstallJob(model.RuntimeInstallJob{
		ID:              "preflight_terminal_guard",
		OperatorID:      1,
		MachineID:       "m_online",
		LauncherAgentID: "agent_chat_codex",
		SemanticAgentID: "reverse-expert",
		Status:          "running",
		CurrentStep:     "skill",
		ProgressPercent: 84,
		Error:           "old transient error",
		StartedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID: job.ID, Status: "running", CurrentStep: "mcp", ProgressPercent: 60,
	}); err != nil {
		t.Fatalf("handle stale running status: %v", err)
	}
	stored, _ := mem.GetRuntimeInstallJob(1, job.ID)
	if stored.CurrentStep != "skill" || stored.ProgressPercent != 84 {
		t.Fatalf("running state regressed: %+v", stored)
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID: job.ID, Status: "completed", CurrentStep: "completed", ProgressPercent: 100,
	}); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID: job.ID, Status: "running", CurrentStep: "runtime", ProgressPercent: 3, Error: "late report",
	}); err != nil {
		t.Fatalf("ignore late report: %v", err)
	}
	stored, _ = mem.GetRuntimeInstallJob(1, job.ID)
	if stored.Status != "completed" || stored.CurrentStep != "completed" || stored.ProgressPercent != 100 || stored.Error != "" {
		t.Fatalf("terminal state regressed: %+v", stored)
	}
}

func TestRuntimePreflightStatusPersistsAndClearsUserAction(t *testing.T) {
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), auth.NewManager(time.Hour), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	job, err := mem.UpsertRuntimeInstallJob(model.RuntimeInstallJob{
		ID:              "preflight_user_action",
		OperatorID:      1,
		MachineID:       "m_windows",
		LauncherAgentID: "agent_windows",
		SemanticAgentID: "reverse-windows",
		Status:          "running",
		CurrentStep:     "runtime",
		StartedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	action := &model.RuntimeUserAction{
		ID:           "ida-uac:" + job.ID,
		Kind:         "windows_uac",
		Title:        "IDA 安装需要管理员确认",
		Message:      "请在目标电脑确认。",
		Instructions: []string{"点击“是”"},
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID:              job.ID,
		Status:             "waiting_user_action",
		CurrentStep:        "runtime",
		ProgressPercent:    49,
		RequiresUserAction: true,
		UserAction:         action,
	}); err != nil {
		t.Fatalf("persist user action: %v", err)
	}
	stored, ok := mem.GetRuntimeInstallJob(1, job.ID)
	if !ok || !stored.RequiresUserAction || stored.UserAction == nil {
		t.Fatalf("expected persisted user action, got %+v", stored)
	}
	if stored.Status != "waiting_user_action" || stored.UserAction.ID != action.ID {
		t.Fatalf("unexpected user action state: %+v", stored)
	}
	if err := api.HandleRuntimePreflightStatus(model.RuntimePreflightStatusPayload{
		JobID:           job.ID,
		Status:          "running",
		CurrentStep:     "runtime",
		ProgressPercent: 49,
	}); err != nil {
		t.Fatalf("clear user action: %v", err)
	}
	stored, ok = mem.GetRuntimeInstallJob(1, job.ID)
	if !ok || stored.RequiresUserAction || stored.UserAction != nil {
		t.Fatalf("expected user action to be cleared, got %+v", stored)
	}
}

func TestRemoveDeviceMCPConfigForwardsName(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(
		mem,
		b,
		authManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceMCPConfigResultPayload, error) {
			if envType != "device.mcp_config.remove" {
				t.Fatalf("unexpected env type %s", envType)
			}
			info, ok := payload.(model.DeviceMCPConfigRemovePayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if info.Name != "docs" {
				t.Fatalf("unexpected forwarded name %q", info.Name)
			}
			return model.DeviceMCPConfigResultPayload{
				Action:  "remove",
				Success: true,
				Config:  &model.DeviceMCPConfigPreview{},
			}, nil
		},
		nil,
		nil,
		nil,
	)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/m_online/mcp-config/remove",
		strings.NewReader(`{"name":"docs"}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.RemoveDeviceMCPConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetDeviceEnvConfigReturnsLauncherPayload(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		envConfigRequest: func(_ context.Context, _ int64, deviceID, _ string, envType string, payload any) (model.DeviceEnvConfigResultPayload, error) {
			if deviceID != "launcher:m_online" {
				t.Fatalf("unexpected device id %s", deviceID)
			}
			if envType != "device.env_config.get" {
				t.Fatalf("unexpected env type %s", envType)
			}
			req, ok := payload.(model.DeviceEnvConfigGetPayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if req.MachineID != "m_online" {
				t.Fatalf("unexpected machine id %s", req.MachineID)
			}
			return model.DeviceEnvConfigResultPayload{
				Action:  "get",
				Success: true,
				Config: &model.DeviceEnvConfigPreview{
					GlobalEnvironment: map[string]string{"HTTP_PROXY": "http://127.0.0.1:7897"},
					Agents: []model.DeviceAgentEnvInfo{{
						AgentID:     "agent_chat_codex",
						Name:        "chat-codex",
						Environment: map[string]string{"OPENAI_API_KEY": "sk-test"},
					}},
				},
			}, nil
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_online/env-config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.GetDeviceEnvConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var result model.DeviceEnvConfigResultPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Config == nil || result.Config.GlobalEnvironment["HTTP_PROXY"] == "" {
		t.Fatalf("unexpected env config result %+v", result.Config)
	}
}

func TestSaveDeviceEnvConfigForwardsEnvironment(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		envConfigRequest: func(_ context.Context, _ int64, deviceID, _ string, envType string, payload any) (model.DeviceEnvConfigResultPayload, error) {
			if deviceID != "launcher:m_online" {
				t.Fatalf("unexpected device id %s", deviceID)
			}
			if envType != "device.env_config.save" {
				t.Fatalf("unexpected env type %s", envType)
			}
			req, ok := payload.(model.DeviceEnvConfigSavePayload)
			if !ok {
				t.Fatalf("unexpected payload type %#v", payload)
			}
			if req.MachineID != "m_online" || req.AgentID != "agent_chat_codex" || req.ExpectedRevision != "revision-1" {
				t.Fatalf("unexpected save payload %+v", req)
			}
			if req.GlobalEnvironment["HTTP_PROXY"] != "http://127.0.0.1:7897" || req.AgentEnvironment["OPENAI_API_KEY"] != "sk-test" {
				t.Fatalf("unexpected environment payload %+v", req)
			}
			return model.DeviceEnvConfigResultPayload{
				Action:  "save",
				Success: true,
				Config: &model.DeviceEnvConfigPreview{
					GlobalEnvironment: req.GlobalEnvironment,
					Agents: []model.DeviceAgentEnvInfo{{
						AgentID:     req.AgentID,
						Environment: req.AgentEnvironment,
					}},
				},
			}, nil
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_online",
		MachineID: "m_online",
		Hostname:  "MacBook",
		Kind:      "launcher",
		Version:   "launcher-0.1.0",
	}, operator.ID)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/m_online/env-config/save",
		strings.NewReader(`{"global_environment":{"HTTP_PROXY":"http://127.0.0.1:7897"},"agent_id":" agent_chat_codex ","agent_environment":{"OPENAI_API_KEY":"sk-test"},"expected_revision":" revision-1 "}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api.SaveDeviceEnvConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeviceAgentCompactionConfigRoutesForwardLauncherPayload(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	var calls []struct {
		envType string
		payload map[string]any
	}
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, agentID, requestID, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			if agentID != "launcher:m_online" || !strings.HasPrefix(requestID, "req_compaction_config_") {
				t.Fatalf("unexpected launcher request: agent=%s request=%s", agentID, requestID)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("encode launcher payload: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("decode launcher payload: %v", err)
			}
			calls = append(calls, struct {
				envType string
				payload map[string]any
			}{envType: envType, payload: decoded})
			threshold := 80
			if value, ok := decoded["threshold_percent"].(float64); ok {
				threshold = int(value)
			}
			return model.DeviceLauncherResultPayload{
				MachineID: "m_online",
				Action:    strings.TrimPrefix(envType, "device.compaction_config."),
				Success:   true,
				CompactionConfig: &model.DeviceAgentCompactionConfig{
					AgentID:                   decoded["agent_id"].(string),
					ThresholdPercent:          threshold,
					DefaultThresholdPercent:   80,
					EffectiveThresholdPercent: threshold,
				},
			}, nil
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{
		AgentID: "launcher:m_online", MachineID: "m_online", Hostname: "MacBook", Kind: "launcher",
		Capabilities: []string{launcherCompactionConfigCapability},
	}, operator.ID)

	withRoute := func(method, url, body string) *http.Request {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("machineID", "m_online")
		rctx.URLParams.Add("agentID", "agent_chat_codex")
		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	getResponse := httptest.NewRecorder()
	api.GetDeviceAgentCompactionConfig(getResponse, withRoute(http.MethodGet, "/api/devices/m_online/launcher/agents/agent_chat_codex/compaction-config", ""))
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get config failed: %d %s", getResponse.Code, getResponse.Body.String())
	}

	saveResponse := httptest.NewRecorder()
	api.SaveDeviceAgentCompactionConfig(saveResponse, withRoute(
		http.MethodPost,
		"/api/devices/m_online/launcher/agents/agent_chat_codex/compaction-config",
		`{"threshold_percent":65}`,
	))
	if saveResponse.Code != http.StatusOK {
		t.Fatalf("save config failed: %d %s", saveResponse.Code, saveResponse.Body.String())
	}
	if len(calls) != 2 || calls[0].envType != "device.compaction_config.get" || calls[1].envType != "device.compaction_config.save" {
		t.Fatalf("unexpected launcher calls: %+v", calls)
	}
	if calls[0].payload["agent_id"] != "agent_chat_codex" || calls[1].payload["threshold_percent"] != float64(65) {
		t.Fatalf("unexpected forwarded payloads: %+v", calls)
	}

	b.Add(nil, model.HelloPayload{
		AgentID: "launcher:m_online", MachineID: "m_online", Hostname: "MacBook", Kind: "launcher",
	}, operator.ID)
	unsupportedResponse := httptest.NewRecorder()
	api.GetDeviceAgentCompactionConfig(unsupportedResponse, withRoute(
		http.MethodGet,
		"/api/devices/m_online/launcher/agents/agent_chat_codex/compaction-config",
		"",
	))
	if unsupportedResponse.Code != http.StatusConflict || !strings.Contains(unsupportedResponse.Body.String(), "不支持") {
		t.Fatalf("expected unsupported launcher response, got %d %s", unsupportedResponse.Code, unsupportedResponse.Body.String())
	}
}

func TestMCPCatalogReturnsEnabledLibraryItemsOnly(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	userToken, err := authManager.Issue(model.Operator{ID: 2, Username: "tester"})
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/verify-mcp", strings.NewReader(`{
	  "id":"verify-mcp",
	  "title":"Verify MCP",
	  "name":"verify",
	  "description":"Verify 工具",
	  "type":"local",
	  "source":"MCP 库",
	  "credential_url":"https://www.xyapi.top/verfiy",
	  "recommended":true,
	  "enabled":true,
	  "tags":["推荐"],
	  "sort_order":1,
	  "config":{
	    "name":"verify",
	    "type":"local",
	    "enabled":true,
	    "command":["npx","-y","@ktbtw/verify-mcp"],
	    "environment":{"VERIFY_API_TOKEN":"vat_xxx"}
	  }
	}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq = withRouteParam(createReq, "id", "verify-mcp")
	createRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %d body=%s", createRR.Code, createRR.Body.String())
	}

	nonRecommendedReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/docs-mcp", strings.NewReader(`{
	  "id":"docs-mcp",
	  "title":"Docs MCP",
	  "name":"docs",
	  "description":"Docs 工具",
	  "type":"remote",
	  "source":"MCP 库",
	  "recommended":false,
	  "enabled":true,
	  "category":"开发文档",
	  "sort_order":2,
	  "config":{
	    "name":"docs",
	    "type":"remote",
	    "enabled":true,
	    "url":"https://docs.example.com/mcp"
	  }
	}`))
	nonRecommendedReq.Header.Set("Authorization", "Bearer "+adminToken)
	nonRecommendedReq = withRouteParam(nonRecommendedReq, "id", "docs-mcp")
	nonRecommendedRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(nonRecommendedRR, nonRecommendedReq)
	if nonRecommendedRR.Code != http.StatusOK {
		t.Fatalf("unexpected non-recommended create status: %d body=%s", nonRecommendedRR.Code, nonRecommendedRR.Body.String())
	}

	disabledReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/disabled-mcp", strings.NewReader(`{
	  "id":"disabled-mcp",
	  "title":"Disabled MCP",
	  "name":"disabled",
	  "description":"Disabled 工具",
	  "type":"local",
	  "source":"MCP 库",
	  "enabled":false,
	  "config":{
	    "name":"disabled",
	    "type":"local",
	    "enabled":false,
	    "command":["npx","-y","disabled-mcp"]
	  }
	}`))
	disabledReq.Header.Set("Authorization", "Bearer "+adminToken)
	disabledReq = withRouteParam(disabledReq, "id", "disabled-mcp")
	disabledRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(disabledRR, disabledReq)
	if disabledRR.Code != http.StatusOK {
		t.Fatalf("unexpected disabled create status: %d body=%s", disabledRR.Code, disabledRR.Body.String())
	}

	legacyReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/idalib-mcp", strings.NewReader(`{
	  "id":"idalib-mcp",
	  "title":"IDA MCP",
	  "name":"idalib-mcp",
	  "description":"legacy",
	  "type":"local",
	  "source":"内置逆向专家",
	  "recommended":true,
	  "enabled":true,
	  "config":{
	    "name":"idalib-mcp",
	    "type":"local",
	    "enabled":true,
	    "command":["uvx","idalib-mcp"]
	  }
	}`))
	legacyReq.Header.Set("Authorization", "Bearer "+adminToken)
	legacyReq = withRouteParam(legacyReq, "id", "idalib-mcp")
	legacyRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(legacyRR, legacyReq)
	if legacyRR.Code != http.StatusOK {
		t.Fatalf("unexpected legacy create status: %d body=%s", legacyRR.Code, legacyRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/mcp-catalog", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListAdminMCPCatalog(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected admin list status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.MCPCatalogItem `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if len(listPayload.Items) != len(defaultMCPCatalogItems())+2 {
		t.Fatalf("expected admin list to hide legacy item only, got %+v", listPayload.Items)
	}
	if hasMCPCatalogID(listPayload.Items, "idalib-mcp") ||
		!hasMCPCatalogID(listPayload.Items, "verify-mcp") ||
		!hasMCPCatalogID(listPayload.Items, "docs-mcp") ||
		!hasMCPCatalogID(listPayload.Items, "disabled-mcp") {
		t.Fatalf("unexpected admin MCP catalog membership: %+v", listPayload.Items)
	}

	catalogReq := httptest.NewRequest(http.MethodGet, "/api/mcp/catalog", nil)
	catalogReq.Header.Set("Authorization", "Bearer "+userToken)
	catalogRR := httptest.NewRecorder()
	api.ListMCPCatalog(catalogRR, catalogReq)
	if catalogRR.Code != http.StatusOK {
		t.Fatalf("unexpected catalog status: %d body=%s", catalogRR.Code, catalogRR.Body.String())
	}
	var catalog mcpCatalogResponse
	if err := json.Unmarshal(catalogRR.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Items) != len(defaultMCPCatalogItems())+1 {
		t.Fatalf("expected only enabled MCP library items, got %+v", catalog.Items)
	}
	var verifyItem, docsItem *mcpCatalogItem
	for index := range catalog.Items {
		switch catalog.Items[index].ID {
		case "catalog:verify-mcp":
			verifyItem = &catalog.Items[index]
		case "catalog:docs-mcp":
			docsItem = &catalog.Items[index]
		case "catalog:disabled-mcp", "catalog:idalib-mcp":
			t.Fatalf("disabled or legacy MCP leaked into public catalog: %+v", catalog.Items[index])
		}
	}
	if verifyItem == nil || verifyItem.CredentialURL == "" {
		t.Fatalf("expected verify MCP in public catalog, got %+v", catalog.Items)
	}
	if docsItem == nil || docsItem.Server.URL != "https://docs.example.com/mcp" {
		t.Fatalf("expected docs MCP in public catalog, got %+v", catalog.Items)
	}
}

func TestSemanticAgentAdminListUpdateAndToggle(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	userToken, err := authManager.Issue(model.Operator{ID: 2, Username: "tester"})
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/admin/semantic-agents", nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+userToken)
	forbiddenRR := httptest.NewRecorder()
	api.ListSemanticAgents(forbiddenRR, forbiddenReq)
	if forbiddenRR.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for non-admin, got %d", forbiddenRR.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/semantic-agents", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListSemanticAgents(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected list status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.SemanticAgentProfile `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listPayload.Items) != 6 {
		t.Fatalf("expected builtin semantic agents, got %+v", listPayload.Items)
	}
	var reverse model.SemanticAgentProfile
	for _, item := range listPayload.Items {
		if item.ID == "reverse-windows" {
			reverse = item
		}
	}
	if strings.TrimSpace(reverse.Prompt) == "" ||
		len(reverse.SkillIDs) == 0 ||
		len(reverse.SkillDefinitions) == 0 ||
		len(reverse.RecommendedMCPServers) == 0 {
		t.Fatalf("expected built-in reverse agent capabilities, got %+v", reverse)
	}

	reverse.Description = "已编辑的逆向专家描述"
	reverse.Prompt = "自定义逆向专家提示词"
	reverse.SkillDefinitions = []model.SemanticAgentSkill{{
		Name:        "reverse-custom",
		Description: "自定义 skill",
		Content:     "# 自定义 skill",
	}}
	reverse.Skills = nil
	reverse.MCPIDs = nil
	reverse.RecommendedMCPServers = nil
	reverse.ToolPermissions = map[string]any{"read": "allow", "bash": map[string]any{"*": "ask"}}
	updateBody, err := json.Marshal(reverse)
	if err != nil {
		t.Fatalf("marshal update: %v", err)
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/api/admin/semantic-agents/reverse-windows", strings.NewReader(string(updateBody)))
	updateReq.Header.Set("Authorization", "Bearer "+adminToken)
	updateReq = withRouteParam(updateReq, "id", "reverse-windows")
	updateRR := httptest.NewRecorder()
	api.UpdateSemanticAgent(updateRR, updateReq)
	if updateRR.Code != http.StatusOK {
		t.Fatalf("unexpected update status: %d body=%s", updateRR.Code, updateRR.Body.String())
	}
	var updated model.SemanticAgentProfile
	if err := json.Unmarshal(updateRR.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.Description != "已编辑的逆向专家描述" || updated.Skills[0] != "reverse-custom" || len(updated.RecommendedMCPServers) != 0 {
		t.Fatalf("unexpected updated semantic agent: %+v", updated)
	}

	enabledReq := httptest.NewRequest(http.MethodPost, "/api/admin/semantic-agents/reverse-windows/enabled", strings.NewReader(`{"enabled":false}`))
	enabledReq.Header.Set("Authorization", "Bearer "+adminToken)
	enabledReq = withRouteParam(enabledReq, "id", "reverse-windows")
	enabledRR := httptest.NewRecorder()
	api.SetSemanticAgentEnabled(enabledRR, enabledReq)
	if enabledRR.Code != http.StatusOK {
		t.Fatalf("unexpected enabled status: %d body=%s", enabledRR.Code, enabledRR.Body.String())
	}
	var disabled model.SemanticAgentProfile
	if err := json.Unmarshal(enabledRR.Body.Bytes(), &disabled); err != nil {
		t.Fatalf("decode disabled semantic agent: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("expected semantic agent disabled: %+v", disabled)
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/semantic-agents/reverse-windows/delete", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteReq = withRouteParam(deleteReq, "id", "reverse-windows")
	deleteRR := httptest.NewRecorder()
	api.DeleteSemanticAgent(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
	afterDeleteRR := httptest.NewRecorder()
	api.ListSemanticAgents(afterDeleteRR, listReq)
	if afterDeleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected list status after delete: %d body=%s", afterDeleteRR.Code, afterDeleteRR.Body.String())
	}
	var afterDelete struct {
		Items []model.SemanticAgentProfile `json:"items"`
	}
	if err := json.Unmarshal(afterDeleteRR.Body.Bytes(), &afterDelete); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if hasSemanticAgentID(afterDelete.Items, "reverse-windows") {
		t.Fatalf("deleted semantic agent was reseeded: %+v", afterDelete.Items)
	}
}

func TestSemanticAgentRegistrySkillAndMCPCatalogBindings(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	defaultToolsReq := httptest.NewRequest(http.MethodGet, "/api/admin/tool-catalog", nil)
	defaultToolsReq.Header.Set("Authorization", "Bearer "+adminToken)
	defaultToolsRR := httptest.NewRecorder()
	api.ListAdminToolCatalog(defaultToolsRR, defaultToolsReq)
	if defaultToolsRR.Code != http.StatusOK {
		t.Fatalf("unexpected default tool catalog status: %d body=%s", defaultToolsRR.Code, defaultToolsRR.Body.String())
	}

	skillReq := httptest.NewRequest(http.MethodPost, "/api/admin/skills/reverse-custom", strings.NewReader(`{
		"id":"reverse-custom",
		"name":"自定义逆向 Skill",
			"description":"用于测试绑定展开",
			"content":"# 自定义逆向 Skill\n\n只读分析流程。",
			"package_files":[
				{"path":"references/guide.md","content":"# Guide"},
				{"path":"../blocked.md","content":"blocked"},
				{"path":"scripts/check.sh","content":"#!/bin/sh\necho ok","executable":true}
			],
			"source":"测试",
		"enabled":true,
		"tags":["reverse","custom"],
		"sort_order":7
	}`))
	skillReq.Header.Set("Authorization", "Bearer "+adminToken)
	skillReq = withRouteParam(skillReq, "id", "reverse-custom")
	skillRR := httptest.NewRecorder()
	api.UpsertSkill(skillRR, skillReq)
	if skillRR.Code != http.StatusOK {
		t.Fatalf("unexpected skill status: %d body=%s", skillRR.Code, skillRR.Body.String())
	}

	mcpReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/reverse-frida", strings.NewReader(`{
		"id":"reverse-frida",
		"name":"frida-mcp-test",
		"title":"Frida MCP Test",
		"description":"用于测试语义 Agent MCP 展开",
		"type":"local",
		"source":"测试",
		"recommended":true,
		"enabled":true,
		"tags":["reverse","frida"],
		"sort_order":8,
		"config":{"name":"frida-mcp-test","type":"local","enabled":false,"command":["npx","-y","frida-mcp-test"]}
	}`))
	mcpReq.Header.Set("Authorization", "Bearer "+adminToken)
	mcpReq = withRouteParam(mcpReq, "id", "reverse-frida")
	mcpRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(mcpRR, mcpReq)
	if mcpRR.Code != http.StatusOK {
		t.Fatalf("unexpected mcp catalog status: %d body=%s", mcpRR.Code, mcpRR.Body.String())
	}
	var storedMCP model.MCPCatalogItem
	if err := json.Unmarshal(mcpRR.Body.Bytes(), &storedMCP); err != nil {
		t.Fatalf("decode mcp catalog response: %v", err)
	}
	if !storedMCP.LaunchReady {
		t.Fatalf("expected runnable mcp launch ready, got %+v", storedMCP)
	}

	blockedMCPReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/frida-analykit-blocked", strings.NewReader(`{
		"id":"frida-analykit-blocked",
		"name":"frida-analykit-blocked",
		"title":"Frida Analykit Blocked",
		"description":"包含示例占位路径的 MCP",
		"type":"local",
		"source":"测试",
		"source_url":"https://www.xyapi.top/codex/api/runtime/mcp/download/frida-analykit.tar.gz",
		"recommended":true,
		"enabled":true,
		"tags":["reverse","frida"],
		"install":{
			"package_manager":"uv",
			"source_url":"https://www.xyapi.top/codex/api/runtime/mcp/download/frida-analykit.tar.gz",
			"install_commands":["uvx --from https://www.xyapi.top/codex/api/runtime/mcp/download/frida-analykit.tar.gz frida-analykit-mcp --help"],
			"run_command_template":["frida-analykit-mcp","--config","${MCP_HOME}/mcp.toml"],
			"executable_names":["frida-analykit-mcp"],
			"install_notes":"需要先生成真实 mcp.toml"
		},
		"sort_order":8,
		"config":{"name":"frida-analykit-blocked","type":"local","enabled":false,"command":["frida-analykit-mcp","--config","/ABSOLUTE/PATH/frida-analykit/mcp.toml"]}
	}`))
	blockedMCPReq.Header.Set("Authorization", "Bearer "+adminToken)
	blockedMCPReq = withRouteParam(blockedMCPReq, "id", "frida-analykit-blocked")
	blockedMCPRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(blockedMCPRR, blockedMCPReq)
	if blockedMCPRR.Code != http.StatusOK {
		t.Fatalf("unexpected blocked mcp status: %d body=%s", blockedMCPRR.Code, blockedMCPRR.Body.String())
	}
	var blockedMCP model.MCPCatalogItem
	if err := json.Unmarshal(blockedMCPRR.Body.Bytes(), &blockedMCP); err != nil {
		t.Fatalf("decode blocked mcp response: %v", err)
	}
	if blockedMCP.LaunchReady || !strings.Contains(blockedMCP.LaunchBlockReason, "占位") {
		t.Fatalf("expected blocked launch readiness, got %+v", blockedMCP)
	}
	if blockedMCP.SourceURL == "" || len(blockedMCP.Install.InstallCommands) != 1 {
		t.Fatalf("expected install metadata preserved, got %+v", blockedMCP)
	}

	runtimeReq := httptest.NewRequest(http.MethodPost, "/api/admin/runtime-catalog/python", strings.NewReader(`{
		"id":"python",
		"name":"Python",
		"description":"用于测试语义 Agent 运行时展开",
		"category":"语言",
		"runtime_kind":"archive",
		"install_strategy":"archive",
		"default_version_constraint":">=3.12 <3.15",
		"enabled":true,
		"tags":["python","test"],
		"sort_order":9
	}`))
	runtimeReq.Header.Set("Authorization", "Bearer "+adminToken)
	runtimeReq = withRouteParam(runtimeReq, "id", "python")
	runtimeRR := httptest.NewRecorder()
	api.UpsertRuntimeCatalogItem(runtimeRR, runtimeReq)
	if runtimeRR.Code != http.StatusOK {
		t.Fatalf("unexpected runtime catalog status: %d body=%s", runtimeRR.Code, runtimeRR.Body.String())
	}

	toolReq := httptest.NewRequest(http.MethodPost, "/api/admin/tool-catalog/reverse-tool", strings.NewReader(`{
		"id":"reverse-tool",
		"name":"逆向测试命令",
		"description":"用于测试语义 Agent 工具权限清理",
		"category":"测试",
		"permission_key":"bash",
		"pattern":"reverse-test *",
		"default_action":"allow",
		"enabled":true,
		"tags":["reverse","shell"],
		"sort_order":10
	}`))
	toolReq.Header.Set("Authorization", "Bearer "+adminToken)
	toolReq = withRouteParam(toolReq, "id", "reverse-tool")
	toolRR := httptest.NewRecorder()
	api.UpsertToolCatalogItem(toolRR, toolReq)
	if toolRR.Code != http.StatusOK {
		t.Fatalf("unexpected tool catalog status: %d body=%s", toolRR.Code, toolRR.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPost, "/api/admin/semantic-agents/reverse-expert", strings.NewReader(`{
		"id":"reverse-expert",
		"name":"逆向专家",
		"description":"绑定测试",
		"icon":"reverse",
		"color":"primary",
		"opencode_name":"reverse-expert",
		"prompt":"绑定测试提示词",
		"skill_ids":["reverse-custom"],
		"mcp_ids":["reverse-frida","frida-analykit-blocked"],
		"runtime_requirements":[{"runtime_id":"python","version_constraint":">=3.12 <3.15","required":true,"purpose":"运行测试脚本"}],
		"tool_permissions":{"read":"allow","bash":{"reverse-test *":"allow"}},
		"enabled":true,
		"sort_order":20,
		"built_in":true
	}`))
	updateReq.Header.Set("Authorization", "Bearer "+adminToken)
	updateReq = withRouteParam(updateReq, "id", "reverse-expert")
	updateRR := httptest.NewRecorder()
	api.UpdateSemanticAgent(updateRR, updateReq)
	if updateRR.Code != http.StatusOK {
		t.Fatalf("unexpected semantic update status: %d body=%s", updateRR.Code, updateRR.Body.String())
	}
	var updated model.SemanticAgentProfile
	if err := json.Unmarshal(updateRR.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated semantic agent: %v", err)
	}
	if len(updated.SkillIDs) != 1 || updated.SkillIDs[0] != "reverse-custom" {
		t.Fatalf("expected skill binding, got %+v", updated)
	}
	if len(updated.SkillDefinitions) != 1 || updated.SkillDefinitions[0].Name != "reverse-custom" || strings.TrimSpace(updated.SkillDefinitions[0].Content) == "" {
		t.Fatalf("expected expanded skill definition, got %+v", updated.SkillDefinitions)
	}
	if len(updated.SkillDefinitions[0].PackageFiles) != 2 ||
		updated.SkillDefinitions[0].PackageFiles[0].Path != "references/guide.md" ||
		updated.SkillDefinitions[0].PackageFiles[1].Path != "scripts/check.sh" {
		t.Fatalf("expected cleaned package files, got %+v", updated.SkillDefinitions[0].PackageFiles)
	}
	if len(updated.MCPIDs) != 2 || updated.MCPIDs[0] != "reverse-frida" || updated.MCPIDs[1] != "frida-analykit-blocked" {
		t.Fatalf("expected mcp binding, got %+v", updated)
	}
	if len(updated.MCPDependencies) != 2 ||
		updated.MCPDependencies[0].ID != "reverse-frida" ||
		updated.MCPDependencies[1].ID != "frida-analykit-blocked" ||
		len(updated.MCPDependencies[1].Install.RunCommandTemplate) != 3 {
		t.Fatalf("expected expanded mcp dependency snapshots, got %+v", updated.MCPDependencies)
	}
	if len(updated.RecommendedMCPServers) != 2 || updated.RecommendedMCPServers[0] != "frida-mcp-test" || updated.RecommendedMCPServers[1] != "frida-analykit-blocked" {
		t.Fatalf("expected expanded mcp server name, got %+v", updated.RecommendedMCPServers)
	}
	if len(updated.RecommendedMCPConfigs) != 1 || updated.RecommendedMCPConfigs[0].Command[2] != "frida-mcp-test" {
		t.Fatalf("expected expanded mcp config, got %+v", updated.RecommendedMCPConfigs)
	}
	if len(updated.RuntimeRequirements) != 1 || updated.RuntimeRequirements[0].RuntimeID != "python" {
		t.Fatalf("expected runtime binding, got %+v", updated.RuntimeRequirements)
	}
	if !hasNestedToolPermission(updated.ToolPermissions, "bash", "reverse-test *") {
		t.Fatalf("expected tool permission binding, got %+v", updated.ToolPermissions)
	}

	deleteSkillReq := httptest.NewRequest(http.MethodPost, "/api/admin/skills/reverse-custom/delete", nil)
	deleteSkillReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteSkillReq = withRouteParam(deleteSkillReq, "id", "reverse-custom")
	deleteSkillRR := httptest.NewRecorder()
	api.DeleteSkill(deleteSkillRR, deleteSkillReq)
	if deleteSkillRR.Code != http.StatusOK {
		t.Fatalf("unexpected skill delete status: %d body=%s", deleteSkillRR.Code, deleteSkillRR.Body.String())
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/semantic-agents", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListSemanticAgents(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected semantic list after skill delete: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.SemanticAgentProfile `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode semantic list after skill delete: %v", err)
	}
	var reverseAfterDelete model.SemanticAgentProfile
	for _, item := range listPayload.Items {
		if item.ID == "reverse-expert" {
			reverseAfterDelete = item
			break
		}
	}
	if hasString(reverseAfterDelete.SkillIDs, "reverse-custom") ||
		hasString(reverseAfterDelete.Skills, "reverse-custom") ||
		hasSemanticSkillDefinition(reverseAfterDelete.SkillDefinitions, "reverse-custom") {
		t.Fatalf("deleted skill should not remain referenced by agent: %+v", reverseAfterDelete)
	}

	deleteMCPReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/reverse-frida/delete", nil)
	deleteMCPReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteMCPReq = withRouteParam(deleteMCPReq, "id", "reverse-frida")
	deleteMCPRR := httptest.NewRecorder()
	api.DeleteMCPCatalogItem(deleteMCPRR, deleteMCPReq)
	if deleteMCPRR.Code != http.StatusOK {
		t.Fatalf("unexpected mcp delete status: %d body=%s", deleteMCPRR.Code, deleteMCPRR.Body.String())
	}
	reverseAfterMCPDelete := loadSemanticAgentForTest(t, api, adminToken, "reverse-expert")
	if hasString(reverseAfterMCPDelete.MCPIDs, "reverse-frida") ||
		hasString(reverseAfterMCPDelete.RecommendedMCPServers, "reverse-frida") ||
		hasString(reverseAfterMCPDelete.RecommendedMCPServers, "frida-mcp-test") ||
		len(reverseAfterMCPDelete.RecommendedMCPConfigs) != 0 {
		t.Fatalf("deleted mcp should not remain referenced by agent: %+v", reverseAfterMCPDelete)
	}

	deleteRuntimeReq := httptest.NewRequest(http.MethodPost, "/api/admin/runtime-catalog/python/delete", nil)
	deleteRuntimeReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteRuntimeReq = withRouteParam(deleteRuntimeReq, "id", "python")
	deleteRuntimeRR := httptest.NewRecorder()
	api.DeleteRuntimeCatalogItem(deleteRuntimeRR, deleteRuntimeReq)
	if deleteRuntimeRR.Code != http.StatusOK {
		t.Fatalf("unexpected runtime delete status: %d body=%s", deleteRuntimeRR.Code, deleteRuntimeRR.Body.String())
	}
	reverseAfterRuntimeDelete := loadSemanticAgentForTest(t, api, adminToken, "reverse-expert")
	if hasRuntimeRequirement(reverseAfterRuntimeDelete.RuntimeRequirements, "python") {
		t.Fatalf("deleted runtime should not remain referenced by agent: %+v", reverseAfterRuntimeDelete)
	}

	deleteToolReq := httptest.NewRequest(http.MethodPost, "/api/admin/tool-catalog/reverse-tool/delete", nil)
	deleteToolReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteToolReq = withRouteParam(deleteToolReq, "id", "reverse-tool")
	deleteToolRR := httptest.NewRecorder()
	api.DeleteToolCatalogItem(deleteToolRR, deleteToolReq)
	if deleteToolRR.Code != http.StatusOK {
		t.Fatalf("unexpected tool delete status: %d body=%s", deleteToolRR.Code, deleteToolRR.Body.String())
	}
	reverseAfterToolDelete := loadSemanticAgentForTest(t, api, adminToken, "reverse-expert")
	if hasNestedToolPermission(reverseAfterToolDelete.ToolPermissions, "bash", "reverse-test *") {
		t.Fatalf("deleted tool permission should not remain referenced by agent: %+v", reverseAfterToolDelete.ToolPermissions)
	}
	if reverseAfterToolDelete.ToolPermissions["read"] != "allow" {
		t.Fatalf("unrelated tool permission should stay, got %+v", reverseAfterToolDelete.ToolPermissions)
	}
}

func TestAdminSkillDescriptionUsesFrontMatterFoldedBlock(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	content := "---\nname: Android-Pentesting-Skill\ndescription: >\n  Comprehensive Android APK security audit with static analysis,\n  dynamic instrumentation, and source-to-sink tracing.\n---\n\n# Body\n"
	req := httptest.NewRequest(http.MethodPost, "/api/admin/skills/android-pentest", strings.NewReader(fmt.Sprintf(`{
		"id":"android-pentest",
		"name":"Android Pentesting Skill",
		"description":">",
		"content":%q,
		"category":"移动安全",
		"source":"测试",
		"enabled":true
	}`, content)))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req = withRouteParam(req, "id", "android-pentest")
	rr := httptest.NewRecorder()
	api.UpsertSkill(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected skill upsert status: %d body=%s", rr.Code, rr.Body.String())
	}
	var item model.Skill
	if err := json.Unmarshal(rr.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	expected := "Comprehensive Android APK security audit with static analysis, dynamic instrumentation, and source-to-sink tracing."
	if item.Description != expected {
		t.Fatalf("expected folded frontmatter description, got %q", item.Description)
	}
	if item.Category != "移动安全" {
		t.Fatalf("expected skill category round-trip, got %+v", item)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/skills", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListSkills(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected skill list status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var payload struct {
		Items []model.Skill `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode skill list: %v", err)
	}
	for _, listed := range payload.Items {
		if listed.ID != "android-pentest" {
			continue
		}
		if listed.Description != expected {
			t.Fatalf("expected cleaned list description, got %+v", listed)
		}
		if listed.Category != "移动安全" {
			t.Fatalf("expected listed skill category, got %+v", listed)
		}
	}
}

func TestAdminMCPCatalogDoesNotExposeLegacyRecommendedIDs(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	createVerifyMCPCatalogItem(t, api, adminToken)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/mcp-catalog", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListAdminMCPCatalog(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected catalog status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.MCPCatalogItem `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(listPayload.Items) != len(defaultMCPCatalogItems()) {
		t.Fatalf("expected complete built-in MCP catalog, got %d items", len(listPayload.Items))
	}
	var item model.MCPCatalogItem
	for _, listed := range listPayload.Items {
		if isLegacyReverseMCPCatalogItem(listed) {
			t.Fatalf("legacy MCP item leaked into catalog: %+v", listed)
		}
		if listed.ID == "verify-mcp" {
			item = listed
		}
	}
	if item.ID != "verify-mcp" || strings.HasPrefix(item.ID, "recommended-") || item.Name != "verify" || !item.Recommended || !item.Enabled {
		t.Fatalf("unexpected catalog item: %+v", item)
	}
	if item.Category != "验证工具" {
		t.Fatalf("expected catalog category round-trip, got %+v", item)
	}
	if item.Config.Command[2] != "@ktbtw/verify-mcp" || item.Config.Environment["VERIFY_API_TOKEN"] != "vat_xxx" || item.Config.Headers["X-Test"] != "ok" || item.Config.Timeout != 45000 {
		t.Fatalf("expected real MCP config, got %+v", item.Config)
	}
}

func TestAdminDefaultCatalogDeletionDoesNotReseedMissingItems(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	if err := api.ensureDefaultSemanticAgentAssets(); err != nil {
		t.Fatalf("seed semantic agent assets: %v", err)
	}

	envListReq := httptest.NewRequest(http.MethodGet, "/api/admin/environment-presets", nil)
	envListReq.Header.Set("Authorization", "Bearer "+adminToken)
	envListRR := httptest.NewRecorder()
	api.ListAdminEnvironmentPresets(envListRR, envListReq)
	if envListRR.Code != http.StatusOK {
		t.Fatalf("unexpected env seed list status: %d body=%s", envListRR.Code, envListRR.Body.String())
	}
	envID := defaultEnvironmentPresets()[0].ID
	envDeleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/environment-presets/"+envID+"/delete", nil)
	envDeleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	envDeleteReq = withRouteParam(envDeleteReq, "id", envID)
	envDeleteRR := httptest.NewRecorder()
	api.DeleteEnvironmentPreset(envDeleteRR, envDeleteReq)
	if envDeleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected env delete status: %d body=%s", envDeleteRR.Code, envDeleteRR.Body.String())
	}
	envListAgainRR := httptest.NewRecorder()
	api.ListAdminEnvironmentPresets(envListAgainRR, envListReq)
	if envListAgainRR.Code != http.StatusOK {
		t.Fatalf("unexpected env list status after delete: %d body=%s", envListAgainRR.Code, envListAgainRR.Body.String())
	}
	var envPayload struct {
		Items []model.EnvironmentPreset `json:"items"`
	}
	if err := json.Unmarshal(envListAgainRR.Body.Bytes(), &envPayload); err != nil {
		t.Fatalf("decode env list after delete: %v", err)
	}
	if hasEnvironmentPresetID(envPayload.Items, envID) {
		t.Fatalf("deleted default environment preset was reseeded: %+v", envPayload.Items)
	}

	toolListReq := httptest.NewRequest(http.MethodGet, "/api/admin/tool-catalog", nil)
	toolListReq.Header.Set("Authorization", "Bearer "+adminToken)
	toolListRR := httptest.NewRecorder()
	api.ListAdminToolCatalog(toolListRR, toolListReq)
	if toolListRR.Code != http.StatusOK {
		t.Fatalf("unexpected tool seed list status: %d body=%s", toolListRR.Code, toolListRR.Body.String())
	}
	toolID := defaultToolCatalogItems()[0].ID
	toolDeleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/tool-catalog/"+toolID+"/delete", nil)
	toolDeleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	toolDeleteReq = withRouteParam(toolDeleteReq, "id", toolID)
	toolDeleteRR := httptest.NewRecorder()
	api.DeleteToolCatalogItem(toolDeleteRR, toolDeleteReq)
	if toolDeleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected tool delete status: %d body=%s", toolDeleteRR.Code, toolDeleteRR.Body.String())
	}
	toolListAgainRR := httptest.NewRecorder()
	api.ListAdminToolCatalog(toolListAgainRR, toolListReq)
	if toolListAgainRR.Code != http.StatusOK {
		t.Fatalf("unexpected tool list status after delete: %d body=%s", toolListAgainRR.Code, toolListAgainRR.Body.String())
	}
	var toolPayload struct {
		Items []model.ToolCatalogItem `json:"items"`
	}
	if err := json.Unmarshal(toolListAgainRR.Body.Bytes(), &toolPayload); err != nil {
		t.Fatalf("decode tool list after delete: %v", err)
	}
	if hasToolCatalogID(toolPayload.Items, toolID) {
		t.Fatalf("deleted default tool was reseeded: %+v", toolPayload.Items)
	}

	createVerifyMCPCatalogItem(t, api, adminToken)
	mcpID := "verify-mcp"
	mcpDeleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/"+mcpID+"/delete", nil)
	mcpDeleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	mcpDeleteReq = withRouteParam(mcpDeleteReq, "id", mcpID)
	mcpDeleteRR := httptest.NewRecorder()
	api.DeleteMCPCatalogItem(mcpDeleteRR, mcpDeleteReq)
	if mcpDeleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected mcp delete status: %d body=%s", mcpDeleteRR.Code, mcpDeleteRR.Body.String())
	}
	mcpListReq := httptest.NewRequest(http.MethodGet, "/api/admin/mcp-catalog", nil)
	mcpListReq.Header.Set("Authorization", "Bearer "+adminToken)
	mcpListAgainRR := httptest.NewRecorder()
	api.ListAdminMCPCatalog(mcpListAgainRR, mcpListReq)
	if mcpListAgainRR.Code != http.StatusOK {
		t.Fatalf("unexpected mcp list status after delete: %d body=%s", mcpListAgainRR.Code, mcpListAgainRR.Body.String())
	}
	var mcpPayload struct {
		Items []model.MCPCatalogItem `json:"items"`
	}
	if err := json.Unmarshal(mcpListAgainRR.Body.Bytes(), &mcpPayload); err != nil {
		t.Fatalf("decode mcp list after delete: %v", err)
	}
	if hasMCPCatalogID(mcpPayload.Items, mcpID) {
		t.Fatalf("deleted recommended mcp was reseeded: %+v", mcpPayload.Items)
	}
}

func TestDefaultSkillCatalogUpgradeRefreshesBuiltIns(t *testing.T) {
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), auth.NewManager(time.Hour), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	stale := model.Skill{
		ID:           "verify-framework",
		Name:         "verify-framework",
		Description:  "stale description",
		Content:      "stale content",
		PackageFiles: []model.SkillPackageFile{{Path: "references/old.md", Content: "old"}},
		Enabled:      false,
		BuiltIn:      true,
	}
	if _, err := mem.UpsertSkill(stale); err != nil {
		t.Fatalf("seed stale built-in skill: %v", err)
	}
	if err := mem.SetDeviceSetting(0, defaultSeedSettingsAgentID, "skills_v4", "true"); err != nil {
		t.Fatalf("mark previous skill seed initialized: %v", err)
	}

	if err := api.ensureDefaultSemanticAgentAssets(); err != nil {
		t.Fatalf("upgrade built-in skills: %v", err)
	}

	items, err := mem.ListSkills(true)
	if err != nil {
		t.Fatalf("list upgraded skills: %v", err)
	}
	var upgraded model.Skill
	for _, item := range items {
		if item.ID == stale.ID {
			upgraded = item
			break
		}
	}
	var expected model.Skill
	for _, item := range defaultSkills() {
		if item.ID == stale.ID {
			expected = item
			break
		}
	}
	if upgraded.Content != strings.TrimSpace(expected.Content) || len(upgraded.PackageFiles) != len(expected.PackageFiles) {
		t.Fatalf("built-in skill was not refreshed: got=%+v expected package files=%d", upgraded, len(expected.PackageFiles))
	}
	if upgraded.Enabled {
		t.Fatalf("built-in skill enabled state was not preserved: %+v", upgraded)
	}
	initialized, err := api.defaultSeedInitialized(defaultSeedSkillsKey)
	if err != nil {
		t.Fatalf("read upgraded seed marker: %v", err)
	}
	if !initialized {
		t.Fatal("upgraded skill seed marker was not persisted")
	}
}

func TestEnvironmentPresetsAdminCRUDAndUserList(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	userToken, err := authManager.Issue(model.Operator{ID: 2, Username: "tester"})
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/environment-presets/test-proxy", strings.NewReader(`{
		"id":"test-proxy",
		"name":"测试代理",
		"description":"用于 API 测试",
		"category":"代理",
		"source":"测试",
		"enabled":true,
		"tags":["proxy","test"],
		"variables":{"HTTP_PROXY":"http://127.0.0.1:7897"},
		"sort_order":5
	}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq = withRouteParam(createReq, "id", "test-proxy")
	createRR := httptest.NewRecorder()
	api.UpsertEnvironmentPreset(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created model.EnvironmentPreset
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created preset: %v", err)
	}
	if created.ID != "test-proxy" || created.Variables["HTTP_PROXY"] == "" {
		t.Fatalf("unexpected created preset: %+v", created)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/environment-presets", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListAdminEnvironmentPresets(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected admin list status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.EnvironmentPreset `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if !hasEnvironmentPresetID(listPayload.Items, "test-proxy") {
		t.Fatalf("expected custom preset, got %+v", listPayload.Items)
	}

	enabledReq := httptest.NewRequest(http.MethodPost, "/api/admin/environment-presets/test-proxy/enabled", strings.NewReader(`{"enabled":false}`))
	enabledReq.Header.Set("Authorization", "Bearer "+adminToken)
	enabledReq = withRouteParam(enabledReq, "id", "test-proxy")
	enabledRR := httptest.NewRecorder()
	api.SetEnvironmentPresetEnabled(enabledRR, enabledReq)
	if enabledRR.Code != http.StatusOK {
		t.Fatalf("unexpected enabled status: %d body=%s", enabledRR.Code, enabledRR.Body.String())
	}

	userListReq := httptest.NewRequest(http.MethodGet, "/api/environment-presets", nil)
	userListReq.Header.Set("Authorization", "Bearer "+userToken)
	userListRR := httptest.NewRecorder()
	api.ListEnvironmentPresets(userListRR, userListReq)
	if userListRR.Code != http.StatusOK {
		t.Fatalf("unexpected user list status: %d body=%s", userListRR.Code, userListRR.Body.String())
	}
	var userListPayload struct {
		Items []model.EnvironmentPreset `json:"items"`
	}
	if err := json.Unmarshal(userListRR.Body.Bytes(), &userListPayload); err != nil {
		t.Fatalf("decode user list: %v", err)
	}
	for _, item := range userListPayload.Items {
		if item.ID == "test-proxy" {
			t.Fatalf("disabled preset should not be visible to users: %+v", userListPayload.Items)
		}
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/environment-presets/test-proxy/delete", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteReq = withRouteParam(deleteReq, "id", "test-proxy")
	deleteRR := httptest.NewRecorder()
	api.DeleteEnvironmentPreset(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
}

func TestToolCatalogAdminCRUDAndUserList(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminToken, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	userToken, err := authManager.Issue(model.Operator{ID: 2, Username: "tester"})
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/tool-catalog/test-tool", strings.NewReader(`{
		"id":"test-tool",
		"name":"测试工具",
		"description":"用于权限列表测试",
		"category":"测试",
		"permission_key":"bash",
		"pattern":"echo *",
		"default_action":"allow",
		"enabled":true,
		"tags":["test","shell"],
		"sort_order":5
	}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq = withRouteParam(createReq, "id", "test-tool")
	createRR := httptest.NewRecorder()
	api.UpsertToolCatalogItem(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created model.ToolCatalogItem
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created tool: %v", err)
	}
	if created.PermissionKey != "bash" || created.Pattern != "echo *" || created.DefaultAction != "allow" {
		t.Fatalf("unexpected created tool: %+v", created)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/tool-catalog", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListAdminToolCatalog(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected admin list status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.ToolCatalogItem `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode admin tool list: %v", err)
	}
	if !hasToolCatalogID(listPayload.Items, "test-tool") {
		t.Fatalf("expected custom tool, got %+v", listPayload.Items)
	}

	userListReq := httptest.NewRequest(http.MethodGet, "/api/tool-catalog", nil)
	userListReq.Header.Set("Authorization", "Bearer "+userToken)
	userListRR := httptest.NewRecorder()
	api.ListToolCatalog(userListRR, userListReq)
	if userListRR.Code != http.StatusOK {
		t.Fatalf("unexpected user list status: %d body=%s", userListRR.Code, userListRR.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/tool-catalog/test-tool/delete", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteReq = withRouteParam(deleteReq, "id", "test-tool")
	deleteRR := httptest.NewRecorder()
	api.DeleteToolCatalogItem(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
}

func hasEnvironmentPresetID(items []model.EnvironmentPreset, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func hasToolCatalogID(items []model.ToolCatalogItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func hasMCPCatalogID(items []model.MCPCatalogItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func hasSemanticAgentID(items []model.SemanticAgentProfile, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func hasString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func hasSemanticSkillDefinition(items []model.SemanticAgentSkill, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasRuntimeRequirement(items []model.SemanticAgentRuntime, runtimeID string) bool {
	for _, item := range items {
		if item.RuntimeID == runtimeID {
			return true
		}
	}
	return false
}

func hasRuntimePreflightItem(items []model.RuntimeInstallJobItem, itemType string, itemID string) bool {
	for _, item := range items {
		if item.ItemType == itemType && item.ItemID == itemID {
			return true
		}
	}
	return false
}

func hasNestedToolPermission(permissions map[string]any, key string, pattern string) bool {
	value, ok := permissions[key]
	if !ok {
		return false
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, ok = nested[pattern]
	return ok
}

func loadSemanticAgentForTest(t *testing.T, api *API, adminToken string, id string) model.SemanticAgentProfile {
	t.Helper()
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/semantic-agents", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRR := httptest.NewRecorder()
	api.ListSemanticAgents(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("unexpected semantic list status: %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Items []model.SemanticAgentProfile `json:"items"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode semantic list: %v", err)
	}
	for _, item := range listPayload.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("semantic agent %s not found in %+v", id, listPayload.Items)
	return model.SemanticAgentProfile{}
}

func createVerifyMCPCatalogItem(t *testing.T, api *API, adminToken string) model.MCPCatalogItem {
	t.Helper()
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/mcp-catalog/verify-mcp", strings.NewReader(`{
	  "id":"verify-mcp",
	  "name":"verify",
	  "title":"Verify MCP",
	  "description":"真实推荐 MCP 配置",
	  "type":"local",
	  "source":"MCP 库",
	  "credential_url":"https://www.xyapi.top/verfiy",
	  "category":"验证工具",
	  "recommended":true,
	  "enabled":true,
	  "tags":["推荐"],
	  "sort_order":3,
	  "config":{
	    "name":"verify",
	    "type":"local",
	    "enabled":true,
	    "command":["npx","-y","@ktbtw/verify-mcp"],
	    "environment":{"VERIFY_API_TOKEN":"vat_xxx"},
	    "headers":{"X-Test":"ok"},
	    "timeout":45000
	  }
	}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq = withRouteParam(createReq, "id", "verify-mcp")
	createRR := httptest.NewRecorder()
	api.UpsertMCPCatalogItem(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("unexpected create mcp catalog status: %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created model.MCPCatalogItem
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created mcp catalog: %v", err)
	}
	return created
}

func withRouteParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func requireEventually(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ok() {
		t.Fatalf("condition was not met within %s", timeout)
	}
}
