package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

const defaultTunnelLauncherVersionFile = "/opt/chat-codex/apk/launcher/version.json"

type tunnelReleaseAsset struct {
	Platform string `json:"platform"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

type tunnelReleaseMetadata struct {
	Version   string               `json:"version"`
	Downloads []tunnelReleaseAsset `json:"downloads"`
}

type tunnelBootstrapRelease struct {
	Version   string
	ServerURL string
	Assets    map[string]tunnelReleaseAsset
}

var tunnelReleaseFilenames = map[string]string{
	"tunnel-darwin-arm64":  "chat-codex-tunnel-darwin-arm64",
	"tunnel-darwin-x64":    "chat-codex-tunnel-darwin-x64",
	"tunnel-linux-x64":     "chat-codex-tunnel-linux-x64",
	"tunnel-linux-arm64":   "chat-codex-tunnel-linux-arm64",
	"tunnel-windows-x64":   "chat-codex-tunnel-windows-x64.exe",
	"tunnel-windows-arm64": "chat-codex-tunnel-windows-arm64.exe",
}

func tunnelLauncherVersionFile() string {
	if value := strings.TrimSpace(os.Getenv("CHAT_CODEX_LAUNCHER_VERSION_FILE")); value != "" {
		return value
	}
	return defaultTunnelLauncherVersionFile
}

func validateTunnelSSHUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "-") {
		return "", errors.New("SSH 用户名格式错误")
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
		return "", errors.New("SSH 用户名格式错误")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", errors.New("SSH 用户名包含控制字符")
		}
	}
	return value, nil
}

func loadTunnelBootstrapRelease(versionFile, platform string) (tunnelBootstrapRelease, error) {
	required := []string{
		"tunnel-darwin-arm64", "tunnel-darwin-x64", "tunnel-linux-x64", "tunnel-linux-arm64",
	}
	if platform == "windows" {
		required = []string{"tunnel-windows-x64", "tunnel-windows-arm64"}
	} else if platform != "unix" {
		return tunnelBootstrapRelease{}, errors.New("unsupported bootstrap platform")
	}

	data, err := os.ReadFile(versionFile)
	if err != nil {
		return tunnelBootstrapRelease{}, fmt.Errorf("read launcher release: %w", err)
	}
	if len(data) > 2<<20 {
		return tunnelBootstrapRelease{}, errors.New("launcher release metadata is too large")
	}
	var metadata tunnelReleaseMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return tunnelBootstrapRelease{}, fmt.Errorf("decode launcher release: %w", err)
	}
	metadata.Version = strings.TrimSpace(metadata.Version)
	if !validTunnelReleaseVersion(metadata.Version) {
		return tunnelBootstrapRelease{}, errors.New("invalid launcher release version")
	}

	byPlatform := make(map[string]tunnelReleaseAsset, len(metadata.Downloads))
	for _, asset := range metadata.Downloads {
		byPlatform[strings.TrimSpace(asset.Platform)] = asset
	}
	release := tunnelBootstrapRelease{Version: metadata.Version, Assets: make(map[string]tunnelReleaseAsset, len(required))}
	for _, key := range required {
		asset, ok := byPlatform[key]
		expectedFilename := tunnelReleaseFilenames[key]
		asset.Filename = strings.TrimSpace(asset.Filename)
		asset.URL = strings.TrimSpace(asset.URL)
		asset.SHA256 = strings.ToLower(strings.TrimSpace(asset.SHA256))
		if !ok || asset.Filename != expectedFilename || !validTunnelSHA256(asset.SHA256) {
			return tunnelBootstrapRelease{}, fmt.Errorf("invalid launcher tunnel asset: %s", key)
		}
		parsedURL, err := url.Parse(asset.URL)
		if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") || parsedURL.Host == "" || parsedURL.Fragment != "" || parsedURL.Query().Get("artifact") != expectedFilename {
			return tunnelBootstrapRelease{}, fmt.Errorf("invalid launcher tunnel URL: %s", key)
		}
		info, err := os.Stat(filepath.Join(filepath.Dir(versionFile), expectedFilename))
		if err != nil || !info.Mode().IsRegular() {
			return tunnelBootstrapRelease{}, fmt.Errorf("launcher tunnel artifact unavailable: %s", key)
		}
		serverURL, err := tunnelServerURLFromAsset(parsedURL)
		if err != nil {
			return tunnelBootstrapRelease{}, err
		}
		if release.ServerURL == "" {
			release.ServerURL = serverURL
		} else if release.ServerURL != serverURL {
			return tunnelBootstrapRelease{}, errors.New("launcher tunnel assets use different server URLs")
		}
		release.Assets[key] = asset
	}
	return release, nil
}

func validTunnelReleaseVersion(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func validTunnelSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func tunnelServerURLFromAsset(assetURL *url.URL) (string, error) {
	const marker = "/api/launcher/download"
	index := strings.Index(assetURL.Path, marker)
	if index < 0 {
		return "", errors.New("launcher tunnel URL has no launcher download path")
	}
	base := *assetURL
	base.Path = strings.TrimRight(base.Path[:index], "/")
	base.RawPath = ""
	base.RawQuery = ""
	base.ForceQuery = false
	base.Fragment = ""
	return strings.TrimRight(base.String(), "/"), nil
}

func (a *App) tunnelBootstrap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	platform := strings.TrimSpace(chi.URLParam(r, "platform"))
	if platform != "unix" && platform != "windows" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rawToken := strings.TrimSpace(chi.URLParam(r, "bootstrapToken"))
	if len(rawToken) != 64 || !validTunnelSHA256(rawToken) || a.tunnels == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	snapshot, err := a.tunnels.bootstrap(rawToken)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	versionFile := a.launcherVersionFile
	if strings.TrimSpace(versionFile) == "" {
		versionFile = tunnelLauncherVersionFile()
	}
	release, err := loadTunnelBootstrapRelease(versionFile, platform)
	if err != nil {
		log.Printf("[tunnel.bootstrap] render unavailable tunnel_id=%s platform=%s error=%v", snapshot.TunnelID, platform, err)
		http.Error(w, "bootstrap temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	ticket, err := encodeTunnelBootstrapTicket(release.ServerURL, snapshot.ClientToken)
	if err != nil {
		log.Printf("[tunnel.bootstrap] ticket encode failed tunnel_id=%s platform=%s error=%v", snapshot.TunnelID, platform, err)
		http.Error(w, "bootstrap temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	var script string
	enrollmentURL := release.ServerURL + "/b/" + rawToken + "/authorized-key"
	if platform == "unix" {
		script = formatUnixTunnelBootstrap(release, enrollmentURL, ticket, snapshot.SSHUsername, snapshot.TunnelID)
	} else {
		script = formatWindowsTunnelBootstrap(release, enrollmentURL, ticket, snapshot.SSHUsername, snapshot.TunnelID)
	}
	if !a.tunnels.bootstrapActive(rawToken, snapshot.TunnelID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

func encodeTunnelBootstrapTicket(serverURL, clientToken string) (string, error) {
	data, err := json.Marshal(map[string]string{
		"server": serverURL, "client_path": tunnelClientPath, "token": clientToken,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func formatUnixTunnelBootstrap(release tunnelBootstrapRelease, enrollmentURL, ticket, username, tunnelID string) string {
	darwinARM := release.Assets["tunnel-darwin-arm64"]
	darwinX64 := release.Assets["tunnel-darwin-x64"]
	linuxX64 := release.Assets["tunnel-linux-x64"]
	linuxARM := release.Assets["tunnel-linux-arm64"]
	return fmt.Sprintf(`set -eu
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64|Darwin-aarch64) PLATFORM=darwin; U=%s; H=%s ;;
  Darwin-x86_64|Darwin-amd64) PLATFORM=darwin; U=%s; H=%s ;;
  Linux-x86_64|Linux-amd64) PLATFORM=linux; U=%s; H=%s ;;
  Linux-aarch64|Linux-arm64) PLATFORM=linux; U=%s; H=%s ;;
  *) echo "Unsupported platform: $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac
D="$HOME/.chat-codex/bin/%s"; B="$D/chat-codex-tunnel"; mkdir -p "$D"
hash_file() { if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'; else sha256sum "$1" | awk '{print $1}'; fi; }
if [ ! -f "$B" ] || [ "$(hash_file "$B")" != "$H" ]; then
  T="$B.tmp.$$"; trap 'rm -f "$T"' EXIT
  if command -v curl >/dev/null 2>&1; then curl -fL --retry 2 "$U" -o "$T"; else wget -O "$T" "$U"; fi
  [ "$(hash_file "$T")" = "$H" ] || { echo "Tunnel client checksum mismatch" >&2; exit 1; }
  chmod 755 "$T"; mv -f "$T" "$B"; trap - EXIT
fi

KEY_DIR="$HOME/.chat-codex/ssh"
KEY="$KEY_DIR/id_ed25519"
ENROLL=%s
mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR"
if [ ! -s "$KEY" ] || [ ! -s "$KEY.pub" ]; then
  rm -f "$KEY" "$KEY.pub"
  command -v ssh-keygen >/dev/null 2>&1 || { echo "ssh-keygen is required" >&2; exit 1; }
  ssh-keygen -q -t ed25519 -N '' -C chat-codex-agent -f "$KEY"
fi
chmod 600 "$KEY"
chmod 644 "$KEY.pub"
PUB="$(cat "$KEY.pub")"
curl -fsS --retry 2 -X POST --data-urlencode "public_key=$PUB" "$ENROLL" >/dev/null

# The bootstrap URL is single-use. Establish one long-lived SSH master and
# leave a local wrapper for the Agent to reuse for subsequent commands.
SESSION_DIR="$HOME/.chat-codex/ssh-sessions/%s"
CONTROL="$SESSION_DIR/control"
TARGET=%s
WRAPPER="$HOME/.chat-codex/bin/chat-codex-ssh-%s"
MASTER="$SESSION_DIR/master.sh"
MASTER_LOG="$SESSION_DIR/master.log"
LABEL="com.chatcodex.ssh.%s"
UNIT="chat-codex-ssh-%s"
mkdir -p "$SESSION_DIR" "$(dirname "$WRAPPER")"
chmod 700 "$SESSION_DIR" "$(dirname "$WRAPPER")" 2>/dev/null || true

cat > "$MASTER" <<'CHAT_CODEX_SSH_MASTER'
#!/bin/sh
set -eu
B="$HOME/.chat-codex/bin/%s/chat-codex-tunnel"
KEY="$HOME/.chat-codex/ssh/id_ed25519"
CONTROL="$HOME/.chat-codex/ssh-sessions/%s/control"
LOG="$HOME/.chat-codex/ssh-sessions/%s/master.log"
TARGET=%s
exec ssh -M -N -T \
  -i "$KEY" \
  -o IdentitiesOnly=yes \
  -o ControlPath="$CONTROL" \
  -o ControlPersist=no \
  -o ServerAliveInterval=15 \
  -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  -o GlobalKnownHostsFile=/dev/null \
  -o LogLevel=ERROR \
  -o "ProxyCommand=\"$B\" connect --ticket %s" "$TARGET" >>"$LOG" 2>&1
CHAT_CODEX_SSH_MASTER
chmod 700 "$MASTER"

if ! ssh -i "$KEY" -o IdentitiesOnly=yes -S "$CONTROL" -O check "$TARGET" >/dev/null 2>&1; then
  rm -f "$CONTROL"
  if [ "$PLATFORM" = darwin ]; then
    launchctl remove "$LABEL" >/dev/null 2>&1 || true
    launchctl submit -l "$LABEL" -- "$MASTER"
  elif command -v systemd-run >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    systemctl --user stop "$UNIT.service" >/dev/null 2>&1 || true
    systemd-run --user --quiet --collect --unit="$UNIT" "$MASTER"
  elif command -v setsid >/dev/null 2>&1; then
    nohup setsid "$MASTER" </dev/null >/dev/null 2>&1 &
  else
    nohup "$MASTER" </dev/null >/dev/null 2>&1 &
  fi
fi

ready=0
attempt=0
while [ "$attempt" -lt 40 ]; do
  if ssh -i "$KEY" -o IdentitiesOnly=yes -S "$CONTROL" -O check "$TARGET" >/dev/null 2>&1; then ready=1; break; fi
  attempt=$((attempt + 1))
  sleep 0.25
done
if [ "$ready" != 1 ]; then
  case "$PLATFORM" in
    darwin) launchctl remove "$LABEL" >/dev/null 2>&1 || true ;;
    linux) systemctl --user stop "$UNIT.service" >/dev/null 2>&1 || true ;;
  esac
  rm -f "$CONTROL" "$MASTER" "$MASTER_LOG"
  rmdir "$SESSION_DIR" 2>/dev/null || true
  echo "SSH master session did not become ready" >&2
  exit 1
fi

tmp="$WRAPPER.tmp.$$"
trap 'rm -f "$tmp"' EXIT
cat > "$tmp" <<'CHAT_CODEX_SSH_WRAPPER'
#!/bin/sh
set -eu
CONTROL="$HOME/.chat-codex/ssh-sessions/%s/control"
KEY="$HOME/.chat-codex/ssh/id_ed25519"
TARGET=%s
MASTER="$HOME/.chat-codex/ssh-sessions/%s/master.sh"
MASTER_LOG="$HOME/.chat-codex/ssh-sessions/%s/master.log"
LABEL="com.chatcodex.ssh.%s"
UNIT="chat-codex-ssh-%s"
cleanup() {
  case "$(uname -s)" in
    Darwin) launchctl remove "$LABEL" >/dev/null 2>&1 || true ;;
    Linux) systemctl --user stop "$UNIT.service" >/dev/null 2>&1 || true ;;
  esac
  rm -f "$CONTROL" "$MASTER" "$MASTER_LOG" "$0"
  rmdir "$(dirname "$CONTROL")" 2>/dev/null || true
}
case "${1:-}" in
  exec)
    shift
    [ "$#" -gt 0 ] || { echo "usage: $0 exec COMMAND [ARGS...]" >&2; exit 2; }
    if ! ssh -i "$KEY" -o IdentitiesOnly=yes -S "$CONTROL" -O check "$TARGET" >/dev/null 2>&1; then cleanup; echo "SSH session is not running" >&2; exit 1; fi
    exec ssh -T -i "$KEY" -o IdentitiesOnly=yes -o ControlMaster=no -o ControlPath="$CONTROL" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o GlobalKnownHostsFile=/dev/null "$TARGET" "$@"
    ;;
  status)
    if ssh -i "$KEY" -o IdentitiesOnly=yes -S "$CONTROL" -O check "$TARGET"; then exit 0; fi
    cleanup
    exit 1
    ;;
  close)
    ssh -i "$KEY" -o IdentitiesOnly=yes -S "$CONTROL" -O exit "$TARGET" >/dev/null 2>&1 || true
    cleanup
    echo "SSH session closed"
    ;;
  *)
    echo "usage: $0 {exec|status|close}" >&2
    exit 2
    ;;
esac
CHAT_CODEX_SSH_WRAPPER
chmod 700 "$tmp"
mv -f "$tmp" "$WRAPPER"
trap - EXIT
printf 'CHAT_CODEX_SSH_SESSION=%%s\nCHAT_CODEX_SSH_COMMAND=%%s exec COMMAND\nCHAT_CODEX_SSH_STATUS=%%s status\nCHAT_CODEX_SSH_CLOSE=%%s close\n' "$SESSION_DIR" "$WRAPPER" "$WRAPPER" "$WRAPPER"
`, shellQuote(darwinARM.URL), shellQuote(darwinARM.SHA256),
		shellQuote(darwinX64.URL), shellQuote(darwinX64.SHA256),
		shellQuote(linuxX64.URL), shellQuote(linuxX64.SHA256),
		shellQuote(linuxARM.URL), shellQuote(linuxARM.SHA256),
		release.Version, shellQuote(enrollmentURL), tunnelID, shellQuote(username+"@localhost"), tunnelID,
		tunnelID, tunnelID, release.Version, tunnelID, tunnelID, shellQuote(username+"@localhost"), ticket,
		tunnelID, shellQuote(username+"@localhost"), tunnelID, tunnelID, tunnelID, tunnelID)
}

func formatWindowsTunnelBootstrap(release tunnelBootstrapRelease, enrollmentURL, ticket, username, tunnelID string) string {
	windowsX64 := release.Assets["tunnel-windows-x64"]
	windowsARM := release.Assets["tunnel-windows-arm64"]
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$arch = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($arch) {
  'x64' { $url = %s; $expected = %s }
  'arm64' { $url = %s; $expected = %s }
  default { throw "Unsupported Windows architecture: $arch" }
}
$dir = Join-Path $env:LOCALAPPDATA %s
$bin = Join-Path $dir 'chat-codex-tunnel.exe'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$valid = (Test-Path -LiteralPath $bin) -and ((Get-FileHash -Algorithm SHA256 -LiteralPath $bin).Hash.ToLowerInvariant() -eq $expected)
if (-not $valid) {
  $tmp = "$bin.download.$PID"
  try {
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $tmp).Hash.ToLowerInvariant() -ne $expected) { throw 'Tunnel client checksum mismatch' }
    Move-Item -Force -LiteralPath $tmp -Destination $bin
  } finally { Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $tmp }
}

$keyDir = Join-Path $env:USERPROFILE '.chat-codex\ssh'
$key = Join-Path $keyDir 'id_ed25519'
New-Item -ItemType Directory -Force -Path $keyDir | Out-Null
if (-not (Test-Path -LiteralPath $key) -or -not (Test-Path -LiteralPath ($key + '.pub'))) {
  Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $key, ($key + '.pub')
  & ssh-keygen.exe -q -t ed25519 -N '""' -C chat-codex-agent -f $key
  if ($LASTEXITCODE -ne 0) { throw 'ssh-keygen failed' }
}
$publicKey = (Get-Content -Raw -LiteralPath ($key + '.pub')).Trim()
Invoke-RestMethod -Method Post -Uri %s -ContentType 'application/json' -Body (@{ public_key = $publicKey } | ConvertTo-Json -Compress) | Out-Null

$sessionRoot = Join-Path $env:USERPROFILE '.chat-codex\ssh-sessions'
$sessionDir = Join-Path $sessionRoot %s
$control = Join-Path $sessionDir 'control'
$alias = %s
$wrapper = Join-Path (Join-Path $env:USERPROFILE '.chat-codex\bin') ('chat-codex-ssh-' + %s + '.ps1')
$config = Join-Path $sessionDir 'ssh_config'
New-Item -ItemType Directory -Force -Path $sessionDir, (Split-Path -Parent $wrapper) | Out-Null

function Test-Master {
	  & ssh.exe -i $key -o IdentitiesOnly=yes -S $control -O check $alias *> $null
  return ($LASTEXITCODE -eq 0)
}

if (-not (Test-Master)) {
  Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $control
	  $sshUser = %s
	  $sshUserConfig = '"' + $sshUser.Replace('\', '\\').Replace('"', '\"') + '"'
	  $keyConfig = '"' + $key.Replace('\', '\\').Replace('"', '\"') + '"'
  $configText = @"
Host $alias
  HostName localhost
	  User $sshUserConfig
	  IdentityFile $keyConfig
	  IdentitiesOnly yes
  ControlMaster yes
  ControlPath $control
  ControlPersist no
  ServerAliveInterval 15
  ServerAliveCountMax 3
  StrictHostKeyChecking no
  UserKnownHostsFile NUL
  GlobalKnownHostsFile NUL
  LogLevel ERROR
  ProxyCommand "$bin" connect --ticket %s
"@
  [IO.File]::WriteAllText($config, $configText, (New-Object Text.UTF8Encoding($false)))
  try {
    $argumentLine = '-F "' + $config.Replace('"', '\"') + '" -M -N -T ' + $alias
    $process = Start-Process -FilePath 'ssh.exe' -ArgumentList $argumentLine -PassThru -WindowStyle Hidden
    $ready = $false
    1..40 | ForEach-Object {
      if (-not $ready) {
        Start-Sleep -Milliseconds 250
        $ready = Test-Master
      }
    }
  } finally {
    Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $config
  }
  if (-not $ready) { throw 'SSH master session did not become ready' }
}

$wrapperText = @'
param(
  [Parameter(Position=0)][string]$Action = 'status',
  [Parameter(ValueFromRemainingArguments=$true)][string[]]$Command
)
$sessionDir = Join-Path (Join-Path $env:USERPROFILE '.chat-codex\ssh-sessions') '__SESSION_ID__'
$control = Join-Path $sessionDir 'control'
$key = Join-Path (Join-Path $env:USERPROFILE '.chat-codex\ssh') 'id_ed25519'
$alias = '__ALIAS__'
function Clear-Session {
  Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $control, $PSCommandPath
  Remove-Item -Force -Recurse -ErrorAction SilentlyContinue -LiteralPath $sessionDir
}
switch ($Action) {
  'exec' {
    if (-not $Command -or $Command.Count -eq 0) { throw 'usage: wrapper.ps1 exec COMMAND [ARGS...]' }
	    & ssh.exe -i $key -o IdentitiesOnly=yes -S $control -O check $alias *> $null
    if ($LASTEXITCODE -ne 0) { Clear-Session; throw 'SSH session is not running' }
	    & ssh.exe -i $key -o IdentitiesOnly=yes -S $control -o ControlMaster=no -T $alias @Command
    exit $LASTEXITCODE
  }
  'status' {
	    & ssh.exe -i $key -o IdentitiesOnly=yes -S $control -O check $alias
    $statusCode = $LASTEXITCODE
    if ($statusCode -ne 0) { Clear-Session }
    exit $statusCode
  }
  'close' {
	    & ssh.exe -i $key -o IdentitiesOnly=yes -S $control -O exit $alias *> $null
    Clear-Session
    Write-Output 'SSH session closed'
    exit 0
  }
  default { throw 'usage: wrapper.ps1 {exec|status|close}' }
}
'@
$wrapperText = $wrapperText.Replace('__SESSION_ID__', %s).Replace('__ALIAS__', %s)
[IO.File]::WriteAllText($wrapper, $wrapperText, (New-Object Text.UTF8Encoding($false)))
Write-Output ('CHAT_CODEX_SSH_SESSION=' + %s)
Write-Output ('CHAT_CODEX_SSH_COMMAND=powershell -NoProfile -ExecutionPolicy Bypass -File "' + $wrapper + '" exec "COMMAND"')
Write-Output ('CHAT_CODEX_SSH_STATUS=powershell -NoProfile -ExecutionPolicy Bypass -File "' + $wrapper + '" status')
Write-Output ('CHAT_CODEX_SSH_CLOSE=powershell -NoProfile -ExecutionPolicy Bypass -File "' + $wrapper + '" close')
	`, powerShellQuote(windowsX64.URL), powerShellQuote(windowsX64.SHA256),
		powerShellQuote(windowsARM.URL), powerShellQuote(windowsARM.SHA256),
		powerShellQuote("ChatCodex/bin/"+release.Version),
		powerShellQuote(enrollmentURL),
		powerShellQuote(tunnelID), powerShellQuote("chat-codex-"+tunnelID), powerShellQuote(tunnelID),
		powerShellQuote(username), ticket,
		powerShellQuote(tunnelID), powerShellQuote("chat-codex-"+tunnelID),
		powerShellQuote(tunnelID))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
