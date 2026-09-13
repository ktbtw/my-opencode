package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"launcher/internal/model"

	"golang.org/x/crypto/ssh"
)

const (
	sshLoopbackAddress             = "127.0.0.1:22"
	sshManagedAuthorizedKeyComment = "chat-codex-agent"
	sshManagedAuthorizedKeyTTL     = 24 * time.Hour
)

var sshAuthorizedKeysMu sync.Mutex

func (s *service) SSHStatus() model.SSHStatus {
	status := model.SSHStatus{Platform: runtime.GOOS, ListenAddress: sshLoopbackAddress}
	status.Installed = sshServerInstalled()
	conn, err := net.DialTimeout("tcp", sshLoopbackAddress, 1500*time.Millisecond)
	if err == nil {
		status.Listening = true
		status.Running = true
		_ = conn.Close()
	}
	status.HostKeyFingerprint = sshHostKeyFingerprint()
	if !status.Installed {
		status.Message = "OpenSSH Server 未安装"
	} else if !status.Listening {
		status.Message = "OpenSSH Server 未监听 127.0.0.1:22"
	} else {
		status.Message = "OpenSSH Server 已就绪"
	}
	return status
}

func (s *service) SSHSetup() model.SSHSetupResult {
	if status := s.SSHStatus(); status.Listening {
		return model.SSHSetupResult{Status: status, Action: "already_ready"}
	}
	var output string
	var err error
	switch runtime.GOOS {
	case "windows":
		output, err = setupWindowsOpenSSH()
	case "darwin":
		return model.SSHSetupResult{
			Status:             s.SSHStatus(),
			RequiresUserAction: true,
			Action:             "enable_remote_login",
			Error:              "请在本机系统设置中开启远程登录（Remote Login）",
		}
	case "linux":
		output, err = setupLinuxOpenSSH()
	default:
		return model.SSHSetupResult{Status: s.SSHStatus(), Error: "当前平台暂不支持 OpenSSH 自动初始化"}
	}
	status := s.SSHStatus()
	result := model.SSHSetupResult{Status: status, Output: trimSSHOutput(output)}
	if err != nil {
		result.Error = err.Error()
	} else if !status.Listening {
		result.Error = status.Message
	}
	return result
}

func (s *service) SSHInstallAuthorizedKey(raw string) model.SSHAuthorizedKeyResult {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(raw)))
	if err != nil {
		return model.SSHAuthorizedKeyResult{Error: "SSH 公钥格式无效，请粘贴 ssh-ed25519、ecdsa 或 ssh-rsa 公钥"}
	}
	canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	fingerprint := ssh.FingerprintSHA256(key)
	sshAuthorizedKeysMu.Lock()
	defer sshAuthorizedKeysMu.Unlock()
	alreadyExists, err := installSSHAuthorizedKey(canonical)
	if err != nil {
		return model.SSHAuthorizedKeyResult{Fingerprint: fingerprint, Error: err.Error()}
	}
	message := "客户端 SSH 公钥已安装"
	if alreadyExists {
		message = "客户端 SSH 公钥已存在"
	}
	return model.SSHAuthorizedKeyResult{
		Installed: true, AlreadyExists: alreadyExists, Fingerprint: fingerprint, Message: message,
	}
}

func (s *service) SSHInstallManagedAuthorizedKey(input model.SSHManagedAuthorizedKeyInput) model.SSHManagedAuthorizedKeyResult {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(input.PublicKey)))
	if err != nil {
		return model.SSHManagedAuthorizedKeyResult{Error: "SSH 公钥格式无效"}
	}
	now := time.Now().UTC()
	expiresAt := input.ExpiresAt.UTC()
	if expiresAt.IsZero() || !expiresAt.After(now) {
		return model.SSHManagedAuthorizedKeyResult{Error: "SSH 公钥有效期已过期"}
	}
	if maximum := now.Add(sshManagedAuthorizedKeyTTL); expiresAt.After(maximum) {
		expiresAt = maximum
	}
	canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	fingerprint := ssh.FingerprintSHA256(key)
	sshAuthorizedKeysMu.Lock()
	alreadyExists, err := installSSHManagedAuthorizedKey(canonical, expiresAt)
	sshAuthorizedKeysMu.Unlock()
	if err != nil {
		return model.SSHManagedAuthorizedKeyResult{Fingerprint: fingerprint, ExpiresAt: expiresAt, Error: err.Error()}
	}
	message := "Agent SSH 临时公钥已安装"
	if alreadyExists {
		message = "Agent SSH 临时公钥已登记"
	}
	return model.SSHManagedAuthorizedKeyResult{
		Installed: true, AlreadyExists: alreadyExists, Fingerprint: fingerprint,
		ExpiresAt: expiresAt, Message: message,
	}
}

func setupLinuxOpenSSH() (string, error) {
	if _, err := exec.LookPath("sshd"); err != nil {
		return "", fmt.Errorf("未找到 sshd，请先安装 openssh-server")
	}
	cmd := exec.Command("sh", "-c", "systemctl enable --now sshd 2>/dev/null || systemctl enable --now ssh 2>/dev/null")
	data, err := cmd.CombinedOutput()
	return string(data), err
}

func setupWindowsOpenSSH() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("Windows OpenSSH setup called on %s", runtime.GOOS)
	}
	script := `$ErrorActionPreference = 'Stop'
$capability = Get-WindowsCapability -Online | Where-Object Name -like 'OpenSSH.Server*'
if ($capability -and $capability.State -ne 'Installed') {
  Add-WindowsCapability -Online -Name $capability.Name | Out-Host
}
$sshdConfig = Join-Path $env:ProgramData 'ssh\sshd_config'
if (Test-Path $sshdConfig) {
  $lines = Get-Content $sshdConfig | Where-Object { $_ -notmatch '^\s*ListenAddress\s+' }
  $lines += 'ListenAddress 127.0.0.1'
  $lines += 'PasswordAuthentication no'
  $lines += 'PubkeyAuthentication yes'
  Set-Content -Path $sshdConfig -Value $lines -Encoding ascii
}
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
Write-Output 'OpenSSH Server initialized with loopback-only listening.'`
	return runWindowsElevatedPowerShell(script)
}

func trimSSHOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		return value[:4096] + "..."
	}
	return value
}

func sshServerInstalled() bool {
	if _, err := exec.LookPath("sshd"); err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		root := strings.TrimSpace(os.Getenv("SystemRoot"))
		if root == "" {
			root = `C:\Windows`
		}
		_, err := os.Stat(filepath.Join(root, "System32", "OpenSSH", "sshd.exe"))
		return err == nil
	}
	return false
}

func sshHostKeyFingerprint() string {
	for _, path := range sshHostPublicKeyPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(fields[1])
		if err != nil {
			continue
		}
		sum := sha256.Sum256(decoded)
		return fmt.Sprintf("SHA256:%s", base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	return ""
}

func sshHostPublicKeyPaths() []string {
	if runtime.GOOS == "windows" {
		programData := strings.TrimSpace(os.Getenv("ProgramData"))
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return []string{
			filepath.Join(programData, "ssh", "ssh_host_ed25519_key.pub"),
			filepath.Join(programData, "ssh", "ssh_host_ecdsa_key.pub"),
			filepath.Join(programData, "ssh", "ssh_host_rsa_key.pub"),
		}
	}
	return []string{
		"/etc/ssh/ssh_host_ed25519_key.pub",
		"/etc/ssh/ssh_host_ecdsa_key.pub",
		"/etc/ssh/ssh_host_rsa_key.pub",
	}
}
