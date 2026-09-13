package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func writeTunnelReleaseForTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	assets := make([]tunnelReleaseAsset, 0, len(tunnelReleaseFilenames))
	i := 0
	for platform, filename := range tunnelReleaseFilenames {
		sha := strings.Repeat(string(rune('a'+i)), 64)
		i++
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(platform), 0o644); err != nil {
			t.Fatal(err)
		}
		assets = append(assets, tunnelReleaseAsset{
			Platform: platform, Filename: filename,
			URL:    "https://relay.example/codex/api/launcher/download?artifact=" + filename,
			SHA256: sha,
		})
	}
	data, err := json.Marshal(tunnelReleaseMetadata{Version: "0.1.148", Downloads: assets})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "version.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func bootstrapRequest(app *App, token, platform string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/b/fixture/"+platform, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("bootstrapToken", token)
	routeContext.URLParams.Add("platform", platform)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
	recorder := httptest.NewRecorder()
	app.tunnelBootstrap(recorder, req)
	return recorder
}

func TestTunnelBootstrapHandlerServesRepeatedUnixReadsUntilClaim(t *testing.T) {
	manager := newTunnelManager()
	credentials, err := manager.createWithBootstrap(7, "machine-a", "launcher:machine-a", "三闻鱼")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.cancelSession(credentials.TunnelID, "test cleanup")
	app := &App{tunnels: manager, launcherVersionFile: writeTunnelReleaseForTest(t)}

	for i := 0; i < 2; i++ {
		response := bootstrapRequest(app, credentials.BootstrapToken, "unix")
		if response.Code != http.StatusOK {
			t.Fatalf("bootstrap read %d: code=%d body=%s", i, response.Code, response.Body.String())
		}
		for header, expected := range map[string]string{
			"Cache-Control": "no-store", "Pragma": "no-cache", "Referrer-Policy": "no-referrer",
			"X-Content-Type-Options": "nosniff", "Content-Type": "text/plain",
		} {
			if !strings.Contains(response.Header().Get(header), expected) {
				t.Fatalf("header %s=%q does not contain %q", header, response.Header().Get(header), expected)
			}
		}
		body := response.Body.String()
		for _, expected := range []string{
			"Darwin-arm64", "Linux-x86_64", "Tunnel client checksum mismatch",
			"StrictHostKeyChecking=no", "ssh -M -N -T", "launchctl submit", "systemd-run --user", "chat-codex-ssh-tun_", "TARGET='三闻鱼@localhost'", ".chat-codex/bin/0.1.148",
			"ssh-keygen -q -t ed25519", "/authorized-key", "IdentitiesOnly=yes", ".chat-codex/ssh/id_ed25519", `rm -f "$CONTROL" "$MASTER" "$MASTER_LOG"`, `rmdir "$SESSION_DIR"`,
		} {
			if !strings.Contains(body, expected) {
				t.Fatalf("Unix bootstrap missing %q: %s", expected, body)
			}
		}
		if strings.Contains(body, credentials.ClientToken) {
			t.Fatal("Unix bootstrap exposed the raw tunnel client token")
		}
	}

	if _, err := manager.claim(credentials.ClientToken, tunnelRoleClient); err != nil {
		t.Fatal(err)
	}
	response := bootstrapRequest(app, credentials.BootstrapToken, "unix")
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected consumed bootstrap 404, got %d body=%s", response.Code, response.Body.String())
	}
}

func TestTunnelBootstrapHandlerRendersWindowsAndHidesFailures(t *testing.T) {
	manager := newTunnelManager()
	credentials, err := manager.createWithBootstrap(7, "machine-a", "launcher:machine-a", "User Name")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.cancelSession(credentials.TunnelID, "test cleanup")
	app := &App{tunnels: manager, launcherVersionFile: writeTunnelReleaseForTest(t)}
	response := bootstrapRequest(app, credentials.BootstrapToken, "windows")
	if response.Code != http.StatusOK {
		t.Fatalf("Windows bootstrap: code=%d body=%s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"$ErrorActionPreference = 'Stop'", "'x64'", "'arm64'", "Get-FileHash",
		"UserKnownHostsFile NUL", "Start-Process -FilePath 'ssh.exe'", "$sshUser = 'User Name'", "User $sshUserConfig", "__SESSION_ID__", "__ALIAS__", "ChatCodex/bin/0.1.148",
		"ssh-keygen.exe", "Invoke-RestMethod -Method Post", "IdentityFile $keyConfig", "IdentitiesOnly yes", "/authorized-key",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("Windows bootstrap missing %q: %s", expected, response.Body.String())
		}
	}

	missing := &App{tunnels: manager, launcherVersionFile: filepath.Join(t.TempDir(), "missing.json")}
	response = bootstrapRequest(missing, credentials.BootstrapToken, "windows")
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), credentials.BootstrapToken) {
		t.Fatalf("unexpected unavailable response: code=%d body=%s", response.Code, response.Body.String())
	}
	response = bootstrapRequest(app, credentials.BootstrapToken, "other")
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected unsupported platform 404, got %d", response.Code)
	}
}

func TestTunnelBootstrapUnixScriptDownloadsVerifiesAndRuns(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is unavailable")
	}
	helper := []byte("#!/bin/sh\nexit 0\n")
	helperHash := fmt.Sprintf("%x", sha256.Sum256(helper))
	enrolled := make(chan string, 1)
	downloads := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/authorized-key") {
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			enrolled <- r.Form.Get("public_key")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"installed":true}`))
			return
		}
		_, _ = w.Write(helper)
	}))
	defer downloads.Close()

	dir := t.TempDir()
	assets := make([]tunnelReleaseAsset, 0, len(tunnelReleaseFilenames))
	for platform, filename := range tunnelReleaseFilenames {
		if err := os.WriteFile(filepath.Join(dir, filename), helper, 0o755); err != nil {
			t.Fatal(err)
		}
		assets = append(assets, tunnelReleaseAsset{
			Platform: platform, Filename: filename,
			URL: downloads.URL + "/api/launcher/download?artifact=" + filename, SHA256: helperHash,
		})
	}
	metadata, err := json.Marshal(tunnelReleaseMetadata{Version: "0.1.148-test", Downloads: assets})
	if err != nil {
		t.Fatal(err)
	}
	versionFile := filepath.Join(dir, "version.json")
	if err := os.WriteFile(versionFile, metadata, 0o644); err != nil {
		t.Fatal(err)
	}

	manager := newTunnelManager()
	credentials, err := manager.createWithBootstrap(7, "machine-a", "launcher:machine-a", "三闻鱼")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.cancelSession(credentials.TunnelID, "test cleanup")
	response := bootstrapRequest(&App{tunnels: manager, launcherVersionFile: versionFile}, credentials.BootstrapToken, "unix")
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap response: code=%d body=%s", response.Code, response.Body.String())
	}

	home := t.TempDir()
	binDir := t.TempDir()
	sshLog := filepath.Join(t.TempDir(), "ssh.log")
	launchctlLog := filepath.Join(t.TempDir(), "launchctl.log")
	fakeSSH := `#!/bin/sh
printf '%s\n' "$*" >> "$SSH_LOG"
case "$*" in
	  *"-M -N -T"*) : > "$SSH_READY"; exit 0 ;;
	  *"-O check"*) test -f "$SSH_READY"; exit $? ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(fakeSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeLaunchctl := `#!/bin/sh
printf '%s\n' "$*" >> "$LAUNCHCTL_LOG"
case "${1:-}" in
  submit)
    shift
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "--" ]; then shift; break; fi
      shift
    done
    "$@"
    ;;
  remove) exit 0 ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "launchctl"), []byte(fakeLaunchctl), 0o755); err != nil {
		t.Fatal(err)
	}
	process := exec.Command("bash")
	process.Stdin = strings.NewReader(response.Body.String())
	process.Env = append(os.Environ(), "HOME="+home, "PATH="+binDir+":"+os.Getenv("PATH"), "SSH_LOG="+sshLog, "SSH_READY="+filepath.Join(home, "ssh-ready"), "LAUNCHCTL_LOG="+launchctlLog)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("execute Unix bootstrap: %v\n%s", err, output)
	}
	installed := filepath.Join(home, ".chat-codex", "bin", "0.1.148-test", "chat-codex-tunnel")
	if data, err := os.ReadFile(installed); err != nil || string(data) != string(helper) {
		t.Fatalf("unexpected installed helper: err=%v data=%q", err, data)
	}
	keyPath := filepath.Join(home, ".chat-codex", "ssh", "id_ed25519")
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("managed SSH key missing or has wrong permissions: info=%v err=%v", info, err)
	}
	select {
	case publicKey := <-enrolled:
		if !strings.HasPrefix(publicKey, "ssh-ed25519 ") || !strings.Contains(publicKey, "chat-codex-agent") {
			t.Fatalf("unexpected enrolled public key %q", publicKey)
		}
	default:
		t.Fatal("bootstrap did not enroll its managed SSH public key")
	}
	wrappers, err := filepath.Glob(filepath.Join(home, ".chat-codex", "bin", "chat-codex-ssh-tun_*"))
	if err != nil || len(wrappers) != 1 {
		t.Fatalf("expected one Agent SSH wrapper, got %v err=%v", wrappers, err)
	}
	wrapperData, err := os.ReadFile(wrappers[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"case \"${1:-}\"", "exec", "status", "close", "三闻鱼@localhost", "IdentitiesOnly=yes", ".chat-codex/ssh/id_ed25519"} {
		if !strings.Contains(string(wrapperData), expected) {
			t.Fatalf("Agent wrapper missing %q: %s", expected, wrapperData)
		}
	}
	sessionDir := filepath.Join(home, ".chat-codex", "ssh-sessions", strings.TrimPrefix(filepath.Base(wrappers[0]), "chat-codex-ssh-"))
	masterData, err := os.ReadFile(filepath.Join(sessionDir, "master.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ssh -M -N -T", "ControlPersist=no", "ServerAliveInterval=15", "--ticket ", "IdentitiesOnly=yes"} {
		if !strings.Contains(string(masterData), expected) {
			t.Fatalf("SSH master script missing %q: %s", expected, masterData)
		}
	}
	sshArgs, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--ticket ", "三闻鱼@localhost", "UserKnownHostsFile=/dev/null", "LogLevel=ERROR", "-M -N -T", "ssh-sessions/tun_", "IdentitiesOnly=yes", ".chat-codex/ssh/id_ed25519"} {
		if !strings.Contains(string(sshArgs), expected) {
			t.Fatalf("SSH invocation missing %q: %s", expected, sshArgs)
		}
	}
	if strings.Contains(string(sshArgs), "-tt") {
		t.Fatalf("Agent bootstrap unexpectedly requested an interactive terminal: %s", sshArgs)
	}
	launchctlArgs, err := os.ReadFile(launchctlLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"submit -l com.chatcodex.ssh.tun_", "master.sh"} {
		if !strings.Contains(string(launchctlArgs), expected) {
			t.Fatalf("launchctl invocation missing %q: %s", expected, launchctlArgs)
		}
	}
	status := exec.Command(wrappers[0], "status")
	status.Env = process.Env
	if output, err := status.CombinedOutput(); err != nil {
		t.Fatalf("Agent wrapper status: %v\n%s", err, output)
	}
	closeSession := exec.Command(wrappers[0], "close")
	closeSession.Env = process.Env
	if output, err := closeSession.CombinedOutput(); err != nil {
		t.Fatalf("Agent wrapper close: %v\n%s", err, output)
	}
	if _, err := os.Stat(wrappers[0]); !os.IsNotExist(err) {
		t.Fatalf("expected wrapper cleanup, stat err=%v", err)
	}
	launchctlArgs, err = os.ReadFile(launchctlLog)
	if err != nil || !strings.Contains(string(launchctlArgs), "remove com.chatcodex.ssh.tun_") {
		t.Fatalf("expected launchctl job cleanup, err=%v log=%s", err, launchctlArgs)
	}
	sessions, err := filepath.Glob(filepath.Join(home, ".chat-codex", "ssh-sessions", "tun_*"))
	if err != nil || len(sessions) != 0 {
		t.Fatalf("expected session directory cleanup, got %v err=%v", sessions, err)
	}

	failureHome := t.TempDir()
	failureBin := t.TempDir()
	for name, content := range map[string]string{
		"ssh":       "#!/bin/sh\nexit 1\n",
		"sleep":     "#!/bin/sh\nexit 0\n",
		"launchctl": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(failureBin, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	failure := exec.Command("bash")
	failure.Stdin = strings.NewReader(response.Body.String())
	failure.Env = append(os.Environ(), "HOME="+failureHome, "PATH="+failureBin+":"+os.Getenv("PATH"))
	if output, err := failure.CombinedOutput(); err == nil || !strings.Contains(string(output), "SSH master session did not become ready") {
		t.Fatalf("expected failed master startup, err=%v output=%s", err, output)
	}
	failureSessions, err := filepath.Glob(filepath.Join(failureHome, ".chat-codex", "ssh-sessions", "tun_*"))
	if err != nil || len(failureSessions) != 0 {
		t.Fatalf("failed master startup left session files: %v err=%v", failureSessions, err)
	}
}

func TestTunnelBootstrapValidatesUsernameAndReleaseAssets(t *testing.T) {
	for _, value := range []string{"bad\nname", "bad\x00name", "-option", strings.Repeat("a", 129)} {
		if _, err := validateTunnelSSHUsername(value); err == nil {
			t.Fatalf("expected username rejection for %q", value)
		}
	}
	if got, err := validateTunnelSSHUsername("  三闻鱼  "); err != nil || got != "三闻鱼" {
		t.Fatalf("unexpected valid username result: got=%q err=%v", got, err)
	}
	versionFile := writeTunnelReleaseForTest(t)
	release, err := loadTunnelBootstrapRelease(versionFile, "unix")
	if err != nil || release.ServerURL != "https://relay.example/codex" {
		t.Fatalf("unexpected release: %+v err=%v", release, err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(versionFile), tunnelReleaseFilenames["tunnel-linux-x64"])); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTunnelBootstrapRelease(versionFile, "unix"); err == nil {
		t.Fatal("expected missing release artifact rejection")
	}
}
