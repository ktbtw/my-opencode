package toolchain

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureUsesManagedNodeAndInjectsMirrorEnv(t *testing.T) {
	runtimeDir := t.TempDir()
	nodeDir := filepath.Join(runtimeDir, "runtimes", "node", defaultNodeVersion)
	writeExecutable(t, nodeDir, "node", "v22.16.0")
	writeExecutable(t, nodeDir, "npm", "10.9.2")
	writeExecutable(t, nodeDir, "npx", "10.9.2")
	systemNodeDir := t.TempDir()
	writeExecutable(t, systemNodeDir, "node", "v20.19.0")
	writeExecutable(t, systemNodeDir, "npm", "10.8.0")
	writeExecutable(t, systemNodeDir, "npx", "10.8.0")
	uvDir := filepath.Join(runtimeDir, "runtimes", "uv", defaultUVVersion)
	writeExecutable(t, uvDir, "uv", "uv 0.7.6")
	javaHome := filepath.Join(runtimeDir, "runtimes", "java", defaultJavaVersion)
	writeExecutable(t, filepath.Join(javaHome, "bin"), "java", "openjdk 21")
	writeExecutable(t, filepath.Join(javaHome, "bin"), "javac", "javac 21")

	t.Setenv("LAUNCHER_TOOLCHAIN_AUTO_INSTALL", "0")
	t.Setenv("LAUNCHER_TOOLCHAIN_INSTALL_PYTHON", "0")
	t.Setenv("LAUNCHER_SYSTEM_NODE_DIRS", systemNodeDir)

	patch, diagnostics, err := Ensure(Options{RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if diagnostics.Node.Path == "" || !diagnostics.Node.Managed {
		t.Fatalf("expected managed node, got %+v", diagnostics.Node)
	}
	if diagnostics.NPX.Path == "" || !diagnostics.NPX.Managed {
		t.Fatalf("expected managed npx, got %+v", diagnostics.NPX)
	}
	if diagnostics.Java.Path == "" || !diagnostics.Java.Managed {
		t.Fatalf("expected managed java, got %+v", diagnostics.Java)
	}
	if got := patch.Env["JAVA_HOME"]; got != javaHome {
		t.Fatalf("expected JAVA_HOME %q, got %q", javaHome, got)
	}
	if got := patch.Env["NPM_CONFIG_REGISTRY"]; got != defaultNPMRegistry {
		t.Fatalf("expected npm registry %q, got %q", defaultNPMRegistry, got)
	}
	if got := patch.Env["PIP_INDEX_URL"]; got != defaultPythonIndex {
		t.Fatalf("expected python index %q, got %q", defaultPythonIndex, got)
	}
	if got := patch.Env["NPM_CONFIG_CACHE"]; got != filepath.Join(runtimeDir, "caches", "npm") {
		t.Fatalf("expected npm cache in runtime dir, got %q", got)
	}
	if got := patch.Env["UV_CACHE_DIR"]; got != filepath.Join(runtimeDir, "caches", "uv") {
		t.Fatalf("expected uv cache in runtime dir, got %q", got)
	}
	pathValue := patch.Env[pathKey()]
	if !strings.Contains(pathValue, nodeDir) {
		t.Fatalf("expected PATH to contain node dir %q, got %q", nodeDir, pathValue)
	}
	if !strings.Contains(pathValue, filepath.Join(javaHome, "bin")) {
		t.Fatalf("expected PATH to contain java bin %q, got %q", filepath.Join(javaHome, "bin"), pathValue)
	}
}

func TestNodePackageManagerVersionReadsManifestWithoutExecutingCommand(t *testing.T) {
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "node_modules", "npm")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("create npm manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "package.json"), []byte(`{"version":"10.9.2"}`), 0o644); err != nil {
		t.Fatalf("write npm package.json: %v", err)
	}
	for _, name := range []string{"npm.cmd", "npx.cmd"} {
		if got := nodePackageManagerVersion(filepath.Join(dir, name)); got != "10.9.2" {
			t.Fatalf("expected %s version 10.9.2, got %q", name, got)
		}
	}
}

func TestEnsureReturnsEnvPatchEvenWhenToolchainMissing(t *testing.T) {
	t.Setenv("LAUNCHER_TOOLCHAIN_AUTO_INSTALL", "0")
	t.Setenv("LAUNCHER_TOOLCHAIN_INSTALL_PYTHON", "0")
	t.Setenv(pathKey(), t.TempDir())
	t.Setenv("LAUNCHER_SYSTEM_NODE_DIRS", t.TempDir())

	patch, diagnostics, err := Ensure(Options{RuntimeDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected missing toolchain error")
	}
	if diagnostics.Node.Error == "" {
		t.Fatalf("expected node diagnostic error, got %+v", diagnostics.Node)
	}
	if got := patch.Env["NPM_CONFIG_REGISTRY"]; got != defaultNPMRegistry {
		t.Fatalf("expected env patch to include registry despite error, got %q", got)
	}
}

func TestEnsureFallsBackToConfiguredSystemNodeWhenManagedInstallFails(t *testing.T) {
	runtimeDir := t.TempDir()
	systemNodeDir := t.TempDir()
	writeExecutable(t, systemNodeDir, "node", "v20.19.6")
	writeExecutable(t, systemNodeDir, "npm", "10.8.2")
	writeExecutable(t, systemNodeDir, "npx", "10.8.2")
	uvDir := filepath.Join(runtimeDir, "runtimes", "uv", defaultUVVersion)
	writeExecutable(t, uvDir, "uv", "uv 0.7.6")

	t.Setenv(pathKey(), t.TempDir())
	t.Setenv("LAUNCHER_SYSTEM_NODE_DIRS", systemNodeDir)
	t.Setenv("LAUNCHER_TOOLCHAIN_INSTALL_PYTHON", "0")

	downloader := &alwaysFailDownloader{err: errors.New("下载失败: 403 Forbidden")}
	patch, diagnostics, err := Ensure(Options{RuntimeDir: runtimeDir, Downloader: downloader})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if downloader.count == 0 {
		t.Fatal("expected managed node download to be attempted before system fallback")
	}
	if diagnostics.Node.Path == "" || diagnostics.Node.Managed {
		t.Fatalf("expected configured system node fallback, got %+v", diagnostics.Node)
	}
	if !strings.HasPrefix(diagnostics.Node.Path, systemNodeDir) {
		t.Fatalf("expected node path under %q, got %q", systemNodeDir, diagnostics.Node.Path)
	}
	if got := patch.Env[pathKey()]; !strings.Contains(got, systemNodeDir) {
		t.Fatalf("expected PATH to contain system node dir %q, got %q", systemNodeDir, got)
	}
}

func TestJoinPathWithCurrentPrependsManagedDirs(t *testing.T) {
	currentKey := pathKey()
	t.Setenv(currentKey, filepath.Join("existing", "bin"))
	first := filepath.Join("managed", "node")
	second := filepath.Join("managed", "uv")

	got := joinPathWithCurrent([]string{first, second})
	parts := strings.Split(got, string(os.PathListSeparator))
	if len(parts) < 3 {
		t.Fatalf("expected at least 3 path parts, got %q", got)
	}
	if parts[0] != first || parts[1] != second {
		t.Fatalf("expected managed dirs first, got %q", got)
	}
}

func TestUVDownloadURLsUseHostedSourceByDefault(t *testing.T) {
	t.Setenv("LAUNCHER_PUBLIC_BASE", "https://download.example/codex")
	asset := archiveAsset{Filename: "uv-x86_64-pc-windows-msvc.zip"}
	urls := uvDownloadURLs(asset)
	if len(urls) != 1 {
		t.Fatalf("expected hosted uv source only, got %#v", urls)
	}
	if !strings.Contains(urls[0], "/api/runtime/uv/download?artifact=uv-x86_64-pc-windows-msvc.zip") {
		t.Fatalf("expected hosted uv source, got %#v", urls)
	}
	joined := strings.Join(urls, "\n")
	if strings.Contains(joined, "github.com") || strings.Contains(joined, "gh-proxy.com") {
		t.Fatalf("expected no GitHub uv source, got %#v", urls)
	}
}

func TestNodeDownloadURLsUseConfiguredMirrorBeforeHostedFallback(t *testing.T) {
	t.Setenv("LAUNCHER_PUBLIC_BASE", "https://download.example/codex")
	asset := archiveAsset{Filename: "node-v22.16.0-darwin-arm64.tar.gz"}
	urls := nodeDownloadURLs("v22.16.0", asset)
	if len(urls) < 2 {
		t.Fatalf("expected multiple node download urls, got %#v", urls)
	}
	if !strings.Contains(urls[0], defaultNodeMirror+"/v22.16.0/node-v22.16.0-darwin-arm64.tar.gz") {
		t.Fatalf("expected default node mirror first, got %#v", urls)
	}
	if !strings.Contains(urls[1], "/api/runtime/node/download?artifact=node-v22.16.0-darwin-arm64.tar.gz") {
		t.Fatalf("expected hosted node fallback, got %#v", urls)
	}
}

func TestUntarGzPreservesNodeNpmSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Node runtime uses zip artifacts")
	}
	archivePath := filepath.Join(t.TempDir(), "node-test.tar.gz")
	if err := writeFakeNodeTarGz(t, archivePath); err != nil {
		t.Fatalf("write fake node archive: %v", err)
	}
	extractDir := t.TempDir()
	if err := untarGz(archivePath, extractDir); err != nil {
		t.Fatalf("untarGz: %v", err)
	}
	binDir := filepath.Join(extractDir, "node-vtest", "bin")
	for _, name := range []string{"npm", "npx"} {
		path := filepath.Join(binDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("expected %s symlink: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected %s to be a symlink, got %s", name, info.Mode())
		}
	}
	if !isNodeDir(binDir) {
		t.Fatalf("expected extracted node bin dir to pass node/npm/npx detection")
	}
}

func TestInstallNodeReplacesBrokenManagedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Node runtime uses zip artifacts")
	}
	t.Setenv("LAUNCHER_NODE_VERSION", defaultNodeVersion)
	runtimeDir := t.TempDir()
	brokenDir := filepath.Join(runtimeDir, "runtimes", "node", defaultNodeVersion)
	writeExecutable(t, filepath.Join(brokenDir, "bin"), "node", "v22.16.0")
	if nodeBinDir(brokenDir) != "" {
		t.Fatal("broken node dir unexpectedly passed validation")
	}
	asset, err := nodeAsset(defaultNodeVersion)
	if err != nil {
		t.Fatalf("nodeAsset: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), asset.Filename)
	if err := writeFakeNodeTarGzWithRoot(t, archivePath, asset.Root); err != nil {
		t.Fatalf("write fake node archive: %v", err)
	}
	downloader := &copyArchiveDownloader{source: archivePath}
	binDir, err := installNode(Options{RuntimeDir: runtimeDir, Downloader: downloader})
	if err != nil {
		t.Fatalf("installNode: %v", err)
	}
	if downloader.count == 0 {
		t.Fatal("expected installNode to redownload broken managed directory")
	}
	if binDir != filepath.Join(brokenDir, "bin") {
		t.Fatalf("expected repaired bin dir %q, got %q", filepath.Join(brokenDir, "bin"), binDir)
	}
	for _, name := range []string{"npm", "npx"} {
		path := filepath.Join(binDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("expected repaired %s symlink: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected repaired %s to be a symlink, got %s", name, info.Mode())
		}
	}
}

func TestJavaDownloadURLsUseHostedSourceByDefault(t *testing.T) {
	t.Setenv("LAUNCHER_PUBLIC_BASE", "https://download.example/codex")
	asset := archiveAsset{Filename: "jdk-21-darwin-aarch64.tar.gz"}
	urls := javaDownloadURLs("21", asset)
	if len(urls) != 1 {
		t.Fatalf("expected single java download url, got %#v", urls)
	}
	if !strings.Contains(urls[0], "/api/runtime/java/download?artifact=jdk-21-darwin-aarch64.tar.gz") {
		t.Fatalf("expected hosted java source, got %#v", urls)
	}
	if strings.Contains(strings.Join(urls, "\n"), "api.adoptium.net") || strings.Contains(strings.Join(urls, "\n"), "github.com") {
		t.Fatalf("expected no official Java/GitHub fallback, got %#v", urls)
	}
}

func TestJavaDownloadURLsUseSingleConfiguredMirror(t *testing.T) {
	t.Setenv("LAUNCHER_JAVA_MIRROR", "https://mirror.example/java")
	t.Setenv("LAUNCHER_JAVA_DOWNLOAD_URLS", "https://download-a.example/java.zip,https://download-b.example/java.zip")
	t.Setenv("LAUNCHER_JAVA_MIRRORS", "https://mirror-a.example/java,https://mirror-b.example/java")
	asset := archiveAsset{Filename: "jdk-21-darwin-aarch64.tar.gz"}
	urls := javaDownloadURLs("21", asset)
	want := []string{"https://mirror.example/java/jdk-21-darwin-aarch64.tar.gz"}
	if strings.Join(urls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected single configured java mirror %#v, got %#v", want, urls)
	}
}

func TestUVMirrorURLSupportsQueryPrefixAndTemplate(t *testing.T) {
	file := "uv-test.zip"
	cases := map[string]string{
		"https://example.com/download?artifact=":        "https://example.com/download?artifact=uv-test.zip",
		"https://example.com/download/{artifact}":       "https://example.com/download/uv-test.zip",
		"https://example.com/github-release/astral/uv":  "https://example.com/github-release/astral/uv/uv-test.zip",
		"https://example.com/github-release/astral/uv/": "https://example.com/github-release/astral/uv/uv-test.zip",
	}
	for mirror, want := range cases {
		if got := uvMirrorURL(mirror, file); got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	}
}

func TestUVDownloadURLsUseSingleConfiguredMirror(t *testing.T) {
	t.Setenv("LAUNCHER_UV_MIRRORS", "https://mirror-a.example/uv, https://mirror-b.example/uv")
	t.Setenv("LAUNCHER_UV_MIRROR", "https://mirror.example/uv")
	t.Setenv("LAUNCHER_PUBLIC_BASE", "https://download.example/codex")
	asset := archiveAsset{Filename: "uv-test.zip"}
	urls := uvDownloadURLs(asset)
	want := []string{
		"https://mirror.example/uv/uv-test.zip",
		"https://download.example/codex/api/runtime/uv/download?artifact=uv-test.zip",
	}
	if strings.Join(urls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected single configured mirror urls %#v, got %#v", want, urls)
	}
}

func TestUVDownloadURLsFullURLOverrideWins(t *testing.T) {
	t.Setenv("LAUNCHER_UV_DOWNLOAD_URL", "https://download.example/uv.zip")
	t.Setenv("LAUNCHER_UV_DOWNLOAD_URLS", "https://download-a.example/uv.zip,https://download-b.example/uv.zip")
	t.Setenv("LAUNCHER_UV_MIRRORS", "https://mirror.example/uv")
	urls := uvDownloadURLs(archiveAsset{Filename: "uv-test.zip"})
	if len(urls) != 1 || urls[0] != "https://download.example/uv.zip" {
		t.Fatalf("expected single override url, got %#v", urls)
	}
}

func TestDownloadFirstFallsBackToNextURL(t *testing.T) {
	downloader := &recordingDownloader{
		failures: map[string]error{
			"https://mirror-a.example/uv.zip": errors.New("下载失败: 403 Forbidden"),
		},
	}
	var progress []string
	err := downloadFirst(
		Options{
			Downloader: downloader,
			Progress: func(message string) {
				progress = append(progress, message)
			},
		},
		[]string{"https://mirror-a.example/uv.zip", "https://mirror-b.example/uv.zip"},
		filepath.Join(t.TempDir(), "uv.zip"),
		"托管 uv",
	)
	if err != nil {
		t.Fatalf("downloadFirst: %v", err)
	}
	got := strings.Join(downloader.urls, "\n")
	want := "https://mirror-a.example/uv.zip\nhttps://mirror-b.example/uv.zip"
	if got != want {
		t.Fatalf("expected fallback attempts %q, got %q", want, got)
	}
	if !strings.Contains(strings.Join(progress, "\n"), "尝试备用源") {
		t.Fatalf("expected progress to mention fallback, got %#v", progress)
	}
}

func TestHTTPDownloaderUsesLauncherUserAgent(t *testing.T) {
	var observedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUA = r.UserAgent()
		if observedUA != downloadUserAgent {
			http.Error(w, "blocked user agent", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("archive"))
	}))
	t.Cleanup(server.Close)

	target := filepath.Join(t.TempDir(), "archive.zip")
	if err := (httpDownloader{}).Download(server.URL, target); err != nil {
		t.Fatalf("download: %v", err)
	}
	if observedUA != downloadUserAgent {
		t.Fatalf("expected user agent %q, got %q", downloadUserAgent, observedUA)
	}
}

func writeExecutable(t *testing.T, dir string, name string, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, executableName(name))
	content := "#!/bin/sh\nprintf '%s\\n' '" + version + "'\n"
	if runtime.GOOS == "windows" {
		content = "@echo off\r\necho " + version + "\r\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func writeFakeNodeTarGz(t *testing.T, path string) error {
	return writeFakeNodeTarGzWithRoot(t, path, "node-vtest")
}

func writeFakeNodeTarGzWithRoot(t *testing.T, path string, root string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()
	entries := []struct {
		name    string
		content string
	}{
		{root + "/bin/node", "#!/bin/sh\necho v22.16.0\n"},
		{root + "/lib/node_modules/npm/bin/npm-cli.js", "#!/bin/sh\necho 10.9.2\n"},
		{root + "/lib/node_modules/npm/bin/npx-cli.js", "#!/bin/sh\necho 10.9.2\n"},
	}
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name,
			Mode: 0o755,
			Size: int64(len(entry.content)),
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if _, err := writer.Write([]byte(entry.content)); err != nil {
			return err
		}
	}
	links := []struct {
		name string
		link string
	}{
		{root + "/bin/npm", "../lib/node_modules/npm/bin/npm-cli.js"},
		{root + "/bin/npx", "../lib/node_modules/npm/bin/npx-cli.js"},
	}
	for _, link := range links {
		if err := writer.WriteHeader(&tar.Header{
			Name:     link.name,
			Mode:     0o755,
			Typeflag: tar.TypeSymlink,
			Linkname: link.link,
		}); err != nil {
			return err
		}
	}
	return nil
}

type recordingDownloader struct {
	urls     []string
	failures map[string]error
}

func (d *recordingDownloader) Download(url string, target string) error {
	d.urls = append(d.urls, url)
	if err := d.failures[url]; err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte("ok"), 0o644)
}

type alwaysFailDownloader struct {
	count int
	err   error
}

func (d *alwaysFailDownloader) Download(url string, target string) error {
	d.count++
	if d.err != nil {
		return d.err
	}
	return errors.New("download failed")
}

type copyArchiveDownloader struct {
	source string
	count  int
}

func (d *copyArchiveDownloader) Download(url string, target string) error {
	d.count++
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(d.source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".cmd"
	}
	return name
}
