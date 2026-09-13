package bootstrap

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"launcher/internal/config"
	"launcher/internal/release"
)

func TestEnsureOperatorKeyPromptsAndStoresValue(t *testing.T) {
	runtimeDir := t.TempDir()
	cfg := &config.Config{
		RuntimeDir: runtimeDir,
		Relay: config.RelayConfig{
			URL: "wss://www.xyapi.top/codex/ws/device",
		},
	}
	input := strings.NewReader("opk_demo1234567890\n")
	var output strings.Builder
	key, err := ensureOperatorKey(cfg, Console{
		In:          input,
		Out:         &output,
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("ensureOperatorKey: %v", err)
	}
	if key != "opk_demo1234567890" {
		t.Fatalf("unexpected key %s", key)
	}
	stored, err := readStoredOperatorKey(runtimeDir)
	if err != nil {
		t.Fatalf("read stored operator key: %v", err)
	}
	if stored != key {
		t.Fatalf("expected stored key %s, got %s", key, stored)
	}
	if !strings.Contains(output.String(), "操作指引页") {
		t.Fatalf("expected output to contain guide hint, got %s", output.String())
	}
}

func TestEnsureOperatorKeyFailsWithoutInputWhenNonInteractive(t *testing.T) {
	cfg := &config.Config{
		RuntimeDir: t.TempDir(),
		Relay: config.RelayConfig{
			URL: "wss://www.xyapi.top/codex/ws/device",
		},
	}
	var output strings.Builder
	_, err := ensureOperatorKey(cfg, Console{
		Out:         &output,
		Interactive: false,
	})
	if err == nil {
		t.Fatal("expected ensureOperatorKey to fail without interactive input")
	}
	if !strings.Contains(err.Error(), "缺少 operator key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeEnvPreservesExplicitValuesAndPrependsPath(t *testing.T) {
	target := map[string]string{
		"NPM_CONFIG_REGISTRY": "https://custom.registry",
		"PATH":                "/custom/bin",
	}
	patch := map[string]string{
		"NPM_CONFIG_REGISTRY": "https://registry.npmmirror.com",
		"PATH":                "/managed/bin" + string(os.PathListSeparator) + "/custom/bin",
		"PIP_INDEX_URL":       "https://pypi.tuna.tsinghua.edu.cn/simple",
	}

	mergeEnv(target, patch)
	if got := target["NPM_CONFIG_REGISTRY"]; got != "https://custom.registry" {
		t.Fatalf("expected explicit registry to be preserved, got %q", got)
	}
	if got := target["PIP_INDEX_URL"]; got != "https://pypi.tuna.tsinghua.edu.cn/simple" {
		t.Fatalf("expected pip index to be added, got %q", got)
	}
	parts := strings.Split(target["PATH"], string(os.PathListSeparator))
	if len(parts) < 2 || parts[0] != "/managed/bin" || parts[1] != "/custom/bin" {
		t.Fatalf("expected managed path before custom path, got %q", target["PATH"])
	}
}

func TestEnsureCLIAutoDownloadsLatestWhenMissing(t *testing.T) {
	runtimeDir := t.TempDir()
	platform := downloadPlatformForTest()
	archiveName := archiveNameForTest()
	archivePath := filepath.Join(t.TempDir(), archiveName)
	sha := writeArchiveForTest(t, archivePath)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/cli/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "9.9.9-test",
			"channel": "latest",
			"downloads": []map[string]any{
				{
					"platform": platform,
					"filename": archiveName,
					"url":      serverURL(r) + "/artifacts/" + archiveName,
					"sha256":   sha,
				},
			},
		})
	})
	mux.HandleFunc("/artifacts/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("LAUNCHER_UPDATE_BASE_URL", server.URL)
	cfg := &config.Config{
		RuntimeDir: runtimeDir,
		Agent: config.AgentConfig{
			BinaryName: "opencode-missing-for-test",
		},
	}
	var output strings.Builder
	if err := ensureCLI(cfg, &output); err != nil {
		t.Fatalf("ensureCLI: %v", err)
	}
	version, executable, err := release.CurrentExecutable(runtimeDir)
	if err != nil {
		t.Fatalf("current executable: %v", err)
	}
	if version != "9.9.9-test" {
		t.Fatalf("expected switched version 9.9.9-test, got %s", version)
	}
	if _, err := os.Stat(executable); err != nil {
		t.Fatalf("expected executable at %s: %v", executable, err)
	}
	if !strings.Contains(output.String(), "自动下载") {
		t.Fatalf("expected output to mention auto download, got %s", output.String())
	}
}

func serverURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func downloadPlatformForTest() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "darwin-arm64"
		}
		return "darwin-x64"
	case "linux":
		if runtime.GOARCH == "arm64" {
			return "linux-arm64"
		}
		return "linux-x64"
	case "windows":
		if runtime.GOARCH == "arm64" {
			return "windows-arm64"
		}
		return "windows-x64"
	default:
		return runtime.GOOS + "-" + runtime.GOARCH
	}
}

func archiveNameForTest() string {
	switch runtime.GOOS {
	case "windows", "darwin":
		return "opencode-test.zip"
	default:
		return "opencode-test.tar.gz"
	}
}

func writeArchiveForTest(t *testing.T, path string) string {
	t.Helper()
	switch filepath.Ext(path) {
	case ".zip":
		return writeZipArchiveForTest(t, path)
	default:
		return writeTarGzArchiveForTest(t, path)
	}
}

func writeZipArchiveForTest(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	entry, err := zipWriter.Create(executableNameForTest())
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("test-binary")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return sha256ForTest(t, path)
}

func writeTarGzArchiveForTest(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	gzWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzWriter)
	content := []byte("test-binary")
	header := &tar.Header{
		Name: executableNameForTest(),
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatalf("write tar content: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close tar.gz file: %v", err)
	}
	return sha256ForTest(t, path)
}

func executableNameForTest() string {
	if runtime.GOOS == "windows" {
		return "opencode.exe"
	}
	return "opencode"
}

func sha256ForTest(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file for sha256: %v", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatalf("hash file: %v", err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
