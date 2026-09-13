//go:build windows

package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

func runWindowsElevatedPowerShell(script string) (string, error) {
	outputFile, err := os.CreateTemp("", "chat-codex-elevated-output-*.txt")
	if err != nil {
		return "", err
	}
	outputPath := outputFile.Name()
	if err := outputFile.Close(); err != nil {
		_ = os.Remove(outputPath)
		return "", err
	}
	defer os.Remove(outputPath)
	errorFile, err := os.CreateTemp("", "chat-codex-elevated-error-*.txt")
	if err != nil {
		return "", err
	}
	errorPath := errorFile.Name()
	if err := errorFile.Close(); err != nil {
		_ = os.Remove(errorPath)
		return "", err
	}
	defer os.Remove(errorPath)
	scriptPath, err := writeWindowsPowerShellScript(wrapWindowsElevatedPowerShellScript(script, outputPath, errorPath))
	if err != nil {
		return "", err
	}
	defer os.Remove(scriptPath)
	args := fmt.Sprintf("-NoLogo -NoProfile -ExecutionPolicy Bypass -File %s", syscall.EscapeArg(scriptPath))
	launcher := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$info = [System.Diagnostics.ProcessStartInfo]::new()
$info.FileName = 'powershell.exe'
$info.Arguments = '%s'
$info.UseShellExecute = $true
$info.Verb = 'runas'
$info.WindowStyle = [System.Diagnostics.ProcessWindowStyle]::Normal
$process = [System.Diagnostics.Process]::Start($info)
if ($null -eq $process) { throw 'OpenSSH setup did not start' }
$process.WaitForExit()
if ($process.ExitCode -ne 0) { throw ('Elevated PowerShell exited with code ' + $process.ExitCode) }`, powerShellSingleQuoted(args))
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", launcher)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	parentOutput, runErr := cmd.CombinedOutput()
	childOutput, _ := os.ReadFile(outputPath)
	childError, _ := os.ReadFile(errorPath)
	output := strings.TrimSpace(string(childOutput))
	if runErr == nil && len(childError) == 0 {
		return output, nil
	}
	detail := strings.TrimSpace(string(childError))
	if detail == "" {
		detail = strings.TrimSpace(string(parentOutput))
	}
	if detail == "" && runErr != nil {
		detail = runErr.Error()
	}
	if len(detail) > 4096 {
		detail = detail[:4096] + "..."
	}
	if runErr != nil {
		return output, fmt.Errorf("Windows 提权 PowerShell 执行失败: %s: %w", detail, runErr)
	}
	return output, fmt.Errorf("Windows 提权 PowerShell 执行失败: %s", detail)
}

func writeWindowsPowerShellScript(script string) (string, error) {
	file, err := os.CreateTemp("", "chat-codex-elevated-*.ps1")
	if err != nil {
		return "", err
	}
	path := file.Name()
	units := utf16.Encode([]rune(script))
	data := make([]byte, 2+len(units)*2)
	data[0], data[1] = 0xff, 0xfe
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[2+i*2:], unit)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func wrapWindowsElevatedPowerShellScript(script, outputPath, errorPath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$chatCodexOutputPath = '%s'
$chatCodexErrorPath = '%s'
try {
  $chatCodexOutput = & {
%s
  } *>&1 | Out-String
  [IO.File]::WriteAllText($chatCodexOutputPath, [string]$chatCodexOutput, [Text.UTF8Encoding]::new($false))
} catch {
  $chatCodexDetail = ($_ | Format-List * -Force | Out-String) + [Environment]::NewLine + $_.InvocationInfo.PositionMessage + [Environment]::NewLine + $_.ScriptStackTrace
  [IO.File]::WriteAllText($chatCodexErrorPath, $chatCodexDetail, [Text.UTF8Encoding]::new($false))
  exit 1
}`, powerShellSingleQuoted(outputPath), powerShellSingleQuoted(errorPath), script)
}
