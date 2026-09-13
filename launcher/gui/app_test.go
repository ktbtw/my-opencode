package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"launcher/internal/binarymeta"
	"launcher/internal/config"
	"launcher/internal/model"
)

func TestSavedLoginLifecycle(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("LAUNCHER_RUNTIME_DIR", runtimeDir)

	app := NewGUIApp()
	empty, err := app.LoadSavedLogin()
	if err != nil {
		t.Fatalf("load empty saved login: %v", err)
	}
	if empty.HasSaved {
		t.Fatalf("expected no saved login, got %+v", empty)
	}

	if err := app.SaveSavedLogin(SavedLogin{
		ServerURL: "https://www.xyapi.top/codex",
		Username:  "admin",
		Password:  "secret",
		AutoLogin: true,
	}); err != nil {
		t.Fatalf("save login: %v", err)
	}

	path := filepath.Join(runtimeDir, "gui", "login.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved login: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("expected saved login file mode 0600, got %o", info.Mode().Perm())
	}

	saved, err := app.LoadSavedLogin()
	if err != nil {
		t.Fatalf("load saved login: %v", err)
	}
	if !saved.HasSaved || !saved.AutoLogin || saved.Username != "admin" || saved.Password != "secret" {
		t.Fatalf("unexpected saved login: %+v", saved)
	}

	if err := app.ClearSavedLogin(); err != nil {
		t.Fatalf("clear saved login: %v", err)
	}
	cleared, err := app.LoadSavedLogin()
	if err != nil {
		t.Fatalf("load cleared saved login: %v", err)
	}
	if cleared.HasSaved {
		t.Fatalf("expected saved login to be cleared, got %+v", cleared)
	}
}

func TestGUISessionLifecycleUsesRestrictedPermissions(t *testing.T) {
	runtimeDir := t.TempDir()
	session := guiSession{ServerURL: "https://relay.example/codex/", AccessToken: "secret-token"}
	if err := storeGUISession(runtimeDir, session); err != nil {
		t.Fatalf("store GUI session: %v", err)
	}
	path := guiSessionPath(runtimeDir)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat GUI session: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("expected GUI session mode 0600, got %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat GUI session directory: %v", err)
	}
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected GUI session directory mode 0700, got %o", dirInfo.Mode().Perm())
	}
	loaded, err := loadGUISession(runtimeDir)
	if err != nil {
		t.Fatalf("load GUI session: %v", err)
	}
	if loaded.ServerURL != "https://relay.example/codex" || loaded.AccessToken != "secret-token" {
		t.Fatalf("unexpected GUI session: %+v", loaded)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["access_token"] != "secret-token" {
		t.Fatalf("session token was not persisted")
	}
}

func TestCreateSSHTunnelUsesShortServerBootstrapCommands(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("LAUNCHER_RUNTIME_DIR", runtimeDir)
	const accessToken = "long-lived-access-token"
	const bootstrapToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	versionRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/launcher/version" {
			versionRequested = true
			http.Error(w, "unexpected legacy fallback", http.StatusInternalServerError)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			t.Fatalf("unexpected authorization header")
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(fmt.Sprint(input["username"])) == "" {
			t.Fatalf("missing SSH username: %+v", input)
		}
		if input["bootstrap_version"] != float64(1) {
			t.Fatalf("missing bootstrap protocol version: %+v", input)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tunnel_id": "tunnel-short", "target": "127.0.0.1:22",
			"bootstrap_unix_path":    "/b/" + bootstrapToken + "/unix",
			"bootstrap_windows_path": "/b/" + bootstrapToken + "/windows",
			"expires_at":             time.Now().Add(time.Minute).UTC(),
		})
	}))
	defer server.Close()
	if err := storeGUISession(runtimeDir, guiSession{ServerURL: server.URL + "/codex", AccessToken: accessToken}); err != nil {
		t.Fatal(err)
	}
	result, err := NewGUIApp().CreateSSHTunnel()
	if err != nil {
		t.Fatal(err)
	}
	if versionRequested {
		t.Fatal("short bootstrap path unexpectedly requested launcher release metadata")
	}
	unixURL := server.URL + "/codex/b/" + bootstrapToken + "/unix"
	windowsURL := server.URL + "/codex/b/" + bootstrapToken + "/windows"
	if !strings.Contains(result.UnixCommand, "bash -o pipefail") || !strings.Contains(result.UnixCommand, unixURL) ||
		!strings.Contains(result.WindowsCommand, "$ErrorActionPreference='Stop'") || !strings.Contains(result.WindowsCommand, windowsURL) {
		t.Fatalf("unexpected short commands: %+v", result)
	}
}

func TestCreateSSHTunnelRequiresBothBootstrapPaths(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("LAUNCHER_RUNTIME_DIR", runtimeDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tunnel_id": "tunnel-missing-path", "bootstrap_unix_path": "/b/token/unix",
		})
	}))
	defer server.Close()
	if err := storeGUISession(runtimeDir, guiSession{ServerURL: server.URL, AccessToken: "token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGUIApp().CreateSSHTunnel(); err == nil || !strings.Contains(err.Error(), "临时 SSH 脚本地址") {
		t.Fatalf("expected missing bootstrap path error, got %v", err)
	}
}

func TestTunnelBootstrapURLRejectsMalformedServerPaths(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	valid, err := tunnelBootstrapURL("https://relay.example/codex/", "/b/"+token+"/unix", "unix")
	if err != nil || valid != "https://relay.example/codex/b/"+token+"/unix" {
		t.Fatalf("unexpected bootstrap URL: %q err=%v", valid, err)
	}
	for _, path := range []string{"/b/short/unix", "/b/" + token + "/windows", "https://evil.example/b/" + token + "/unix"} {
		if _, err := tunnelBootstrapURL("https://relay.example/codex", path, "unix"); err == nil {
			t.Fatalf("expected malformed bootstrap path rejection: %s", path)
		}
	}
}

func TestTerminalCompatAutostartOptionsOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("terminal compatibility mode is macOS only")
	}
	runtimeDir := t.TempDir()
	cfg, err := NewGUIApp().currentConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.RuntimeDir = runtimeDir
	cfg.ListenAddr = "127.0.0.1:4071"
	executable := filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatalf("mkdir executable dir: %v", err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	command, args, err := terminalCompatAutostartOptions(cfg, executable)
	if err != nil {
		t.Fatalf("terminal compat options: %v", err)
	}
	if command != "/usr/bin/open" {
		t.Fatalf("expected open command, got %s", command)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "Terminal") || !strings.Contains(joined, "start-terminal-compat.command") {
		t.Fatalf("expected terminal compat args, got %v", args)
	}
	script := filepath.Join(runtimeDir, "gui", "start-terminal-compat.command")
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, executable) || !strings.Contains(content, "nohup") {
		t.Fatalf("script missing launcher command: %s", content)
	}
	if !strings.Contains(content, "close-terminal-compat.applescript") || !strings.Contains(content, "&!") {
		t.Fatalf("script missing detached terminal close hook: %s", content)
	}
	closeScript := filepath.Join(runtimeDir, "gui", "close-terminal-compat.applescript")
	closeData, err := os.ReadFile(closeScript)
	if err != nil {
		t.Fatalf("read close script: %v", err)
	}
	if !strings.Contains(string(closeData), "close terminalWindow saving no") {
		t.Fatalf("close script missing window close command: %s", string(closeData))
	}
}

func TestBackgroundExecutableCopiesCurrentBinaryToRuntimeBin(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("LAUNCHER_RUNTIME_DIR", runtimeDir)
	t.Setenv("LAUNCHER_SKIP_MAC_APP_INSTALL", "1")

	cfg, err := NewGUIApp().currentConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	path, err := NewGUIApp().backgroundExecutable(cfg)
	if err != nil {
		t.Fatalf("background executable: %v", err)
	}
	if filepath.Dir(path) != filepath.Join(runtimeDir, "bin") {
		t.Fatalf("expected runtime bin executable, got %s", path)
	}
	if filepath.Base(path) != binarymeta.GUILauncherBinaryName() {
		t.Fatalf("expected GUI launcher executable, got %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat background executable: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected copied executable to be executable, got mode %o", info.Mode().Perm())
	}
}

func TestAutostartBackgroundExecutableKeepsExistingInstall(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("LAUNCHER_RUNTIME_DIR", runtimeDir)
	t.Setenv("LAUNCHER_SKIP_MAC_APP_INSTALL", "1")

	app := NewGUIApp()
	cfg, err := app.currentConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	target := filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	oldContents := []byte("existing launcher must not be overwritten by autostart migration")
	if err := os.WriteFile(target, oldContents, 0o755); err != nil {
		t.Fatalf("write existing target: %v", err)
	}

	path, err := app.autostartBackgroundExecutable(cfg)
	if err != nil {
		t.Fatalf("resolve autostart executable: %v", err)
	}
	if path != target {
		t.Fatalf("expected autostart target %s, got %s", target, path)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read existing target: %v", err)
	}
	if string(contents) != string(oldContents) {
		t.Fatalf("autostart migration overwrote the existing launcher")
	}
}

func TestInstalledGUIExecutableUsesStableRuntimePath(t *testing.T) {
	runtimeDir := t.TempDir()
	got := installedGUIExecutable(config.Config{RuntimeDir: runtimeDir})
	want := filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if got != want {
		t.Fatalf("expected stable GUI path %s, got %s", want, got)
	}
}

func TestStableGUIPathUsesRuntimeBin(t *testing.T) {
	runtimeDir := filepath.Join("C:", "Users", "demo", "AppData", "Local", "my-opencode-launcher")
	want := filepath.Join(runtimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if got := stableGUIPath(runtimeDir); got != want {
		t.Fatalf("expected stable GUI path %s, got %s", want, got)
	}
}

func TestMigrateLegacyRuntimeScriptUsesStableBackgroundLauncher(t *testing.T) {
	runtimeDir := t.TempDir()
	path := filepath.Join(runtimeDir, "run-launcher.sh")
	content := "#!/bin/zsh\nexport KEEP_ME=1\nexec \"$HOME/.my-opencode-launcher/bin/launcher\"\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{RuntimeDir: runtimeDir}
	if err := migrateLegacyRuntimeScript(cfg); err != nil {
		t.Fatalf("migrate legacy runtime script: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "export KEEP_ME=1") || !strings.Contains(text, installedGUIExecutable(cfg)+"\" --background") {
		t.Fatalf("unexpected migrated script: %s", text)
	}
	if strings.Contains(text, "/bin/launcher\"") {
		t.Fatalf("legacy launcher entrypoint remained: %s", text)
	}
}

func TestCopyExecutableIfChangedPreservesUnchangedTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	contents := []byte("same launcher bytes")
	if err := os.WriteFile(source, contents, 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(target, contents, 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	before := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(target, before, before); err != nil {
		t.Fatalf("set target timestamp: %v", err)
	}

	if err := copyExecutableIfChanged(source, target); err != nil {
		t.Fatalf("copy unchanged executable: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if !info.ModTime().Equal(before) {
		t.Fatalf("unchanged target was rewritten: before=%s after=%s", before, info.ModTime())
	}
}

func TestBootstrapResultCarriesStructuredProgress(t *testing.T) {
	app := NewGUIApp()
	app.setBootstrapProgress("waiting_control", "正在等待本地控制口连接", 70, "")

	result := app.bootstrapResult(false, "launcher 尚未启动", model.DeviceView{})
	if result.Bootstrap.Phase != "waiting_control" {
		t.Fatalf("expected bootstrap phase waiting_control, got %+v", result.Bootstrap)
	}
	if result.Bootstrap.Percent != 70 {
		t.Fatalf("expected bootstrap percent 70, got %+v", result.Bootstrap)
	}
	if result.Bootstrap.StartedAt == "" || result.Bootstrap.UpdatedAt == "" {
		t.Fatalf("expected bootstrap timestamps, got %+v", result.Bootstrap)
	}
}

func TestSyncBootstrapFromDeviceUpgradeMapsDownloadStage(t *testing.T) {
	app := NewGUIApp()
	app.setBootstrapProgress("checking_update", "正在确认后台 launcher 版本", 30, "")
	app.syncBootstrapFromDeviceUpgrade(model.DeviceView{
		UpgradeStage:    "downloading",
		UpgradeMessage:  "正在下载 launcher 更新包",
		UpgradeProgress: 50,
	})

	progress := app.snapshotBootstrapProgress()
	if progress.Phase != "self_update_downloading" {
		t.Fatalf("expected self_update_downloading, got %+v", progress)
	}
	if progress.Message != "正在下载 launcher 更新包" {
		t.Fatalf("expected upgrade message, got %+v", progress)
	}
	if progress.Percent <= 30 {
		t.Fatalf("expected mapped percent to advance, got %+v", progress)
	}
}
