//go:build windows

package app

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

func installSSHAuthorizedKey(publicKey string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	account := windowsSSHAccount(home)
	userSID := windowsSSHUserSID()
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$key = '%s'
$userAccount = '%s'
$userSid = '%s'
$userSsh = Join-Path $env:USERPROFILE '.ssh'
$userKeys = Join-Path $userSsh 'authorized_keys'
$adminKeys = Join-Path $env:ProgramData 'ssh\administrators_authorized_keys'
New-Item -ItemType Directory -Force -Path $userSsh | Out-Null
$paths = @($userKeys)
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { $paths += $adminKeys }
$added = $false
foreach ($path in $paths) {
  $existing = if (Test-Path $path) { Get-Content $path -ErrorAction Stop } else { @() }
  if ($existing -notcontains $key) { Add-Content -Path $path -Value $key -Encoding ascii; $added = $true }
}

if ([string]::IsNullOrWhiteSpace($userSid)) {
  $userSid = ([Security.Principal.NTAccount]$userAccount).Translate([Security.Principal.SecurityIdentifier]).Value
}
icacls.exe $userSsh /inheritance:r /grant:r ('*' + $userSid + ':(OI)(CI)F') '*S-1-5-18:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to set the user SSH directory ACL' }
icacls.exe $userKeys /inheritance:r /grant:r ('*' + $userSid + ':F') '*S-1-5-18:F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to set the user authorized_keys ACL' }
if (Test-Path $adminKeys) {
  icacls.exe $adminKeys /inheritance:r /grant:r '*S-1-5-32-544:F' '*S-1-5-18:F' | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Failed to set the administrators_authorized_keys ACL' }
}
if ($added) { Write-Output 'installed' } else { Write-Output 'exists' }`, powerShellSingleQuoted(publicKey), powerShellSingleQuoted(account), powerShellSingleQuoted(userSID))
	output, err := runWindowsElevatedPowerShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(output), "exists"), nil
}

func installSSHManagedAuthorizedKey(publicKey string, expiresAt time.Time) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	userSsh := filepath.Join(home, ".ssh")
	userKeys := filepath.Join(userSsh, "authorized_keys")
	adminKeys := filepath.Join(programData, "ssh", "administrators_authorized_keys")
	managedLine := fmt.Sprintf(`expiry-time="%s" %s %s`, expiresAt.UTC().Format("20060102150405Z"), publicKey, sshManagedAuthorizedKeyComment)
	account := windowsSSHAccount(home)
	userSID := windowsSSHUserSID()
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$key = '%s'
$managedLine = '%s'
$managedComment = '%s'
$userSsh = '%s'
$userKeys = '%s'
$adminKeys = '%s'
$userAccount = '%s'
$userSid = '%s'
New-Item -ItemType Directory -Force -Path $userSsh, (Split-Path -Parent $adminKeys) | Out-Null
$paths = @($userKeys, $adminKeys)
$allAlready = $true
foreach ($path in $paths) {
  $existing = if (Test-Path -LiteralPath $path) { @(Get-Content -LiteralPath $path -ErrorAction Stop) } else { @() }
  if ($existing -notcontains $managedLine) { $allAlready = $false }
  $kept = @($existing | Where-Object { -not ($_.TrimEnd().EndsWith(' ' + $managedComment) -and $_.Contains($key)) })
  $updated = @($kept) + @($managedLine)
  $tmp = $path + '.tmp.' + $PID
  try {
    [IO.File]::WriteAllLines($tmp, $updated, [Text.Encoding]::ASCII)
    Move-Item -Force -LiteralPath $tmp -Destination $path
  } finally { Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $tmp }
}
if ([string]::IsNullOrWhiteSpace($userSid)) {
  $userSid = ([Security.Principal.NTAccount]$userAccount).Translate([Security.Principal.SecurityIdentifier]).Value
}
icacls.exe $userSsh /inheritance:r /grant:r ('*' + $userSid + ':(OI)(CI)F') '*S-1-5-18:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to set the user SSH directory ACL' }
icacls.exe $userKeys /inheritance:r /grant:r ('*' + $userSid + ':F') '*S-1-5-18:F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to set the user authorized_keys ACL' }
icacls.exe $adminKeys /inheritance:r /grant:r '*S-1-5-32-544:F' '*S-1-5-18:F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to set the administrators_authorized_keys ACL' }
if ($allAlready) { Write-Output 'exists' } else { Write-Output 'installed' }`,
		powerShellSingleQuoted(publicKey), powerShellSingleQuoted(managedLine),
		powerShellSingleQuoted(sshManagedAuthorizedKeyComment), powerShellSingleQuoted(userSsh),
		powerShellSingleQuoted(userKeys), powerShellSingleQuoted(adminKeys), powerShellSingleQuoted(account), powerShellSingleQuoted(userSID))
	output, err := runWindowsElevatedPowerShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(output), "exists"), nil
}

func windowsSSHAccount(home string) string {
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		return strings.TrimSpace(current.Username)
	}
	account := strings.TrimSpace(os.Getenv("USERNAME"))
	if account == "" {
		account = filepath.Base(home)
	}
	if domain := strings.TrimSpace(os.Getenv("USERDOMAIN")); domain != "" && !strings.Contains(account, `\`) {
		account = domain + `\` + account
	}
	return account
}

func windowsSSHUserSID() string {
	if current, err := user.Current(); err == nil {
		return strings.TrimSpace(current.Uid)
	}
	return ""
}
