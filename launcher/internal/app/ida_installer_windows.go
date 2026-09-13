//go:build windows

package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"launcher/internal/backgroundcmd"
)

func runElevatedIDAWindowsInstaller(ctx context.Context, installer string, args []string) (string, error) {
	script := elevatedIDAInstallerPowerShell(installer, args)
	cmd := exec.CommandContext(ctx,
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodePowerShellUTF16LE(script),
	)
	backgroundcmd.Configure(cmd)
	cmd.Dir = filepath.Dir(installer)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(output), errors.New("IDA 安装器执行超时")
	}
	return string(output), err
}

func elevatedIDAInstallerPowerShell(installer string, args []string) string {
	commandLine := make([]string, 0, len(args))
	for _, arg := range args {
		commandLine = append(commandLine, syscall.EscapeArg(arg))
	}
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
try {
  $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = [System.Security.Principal.WindowsPrincipal]::new($identity)
  $alreadyElevated = $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)
  if ($alreadyElevated) {
    Write-Output 'Launcher is already elevated; Windows will not show another UAC prompt.'
  } else {
    Write-Output 'Requesting Windows administrator confirmation for the IDA installer.'
  }
  $info = [System.Diagnostics.ProcessStartInfo]::new()
  $info.FileName = '%s'
  $info.Arguments = '%s'
  $info.WorkingDirectory = '%s'
  $info.UseShellExecute = $true
  $info.Verb = 'runas'
  $info.WindowStyle = [System.Diagnostics.ProcessWindowStyle]::Normal
  $process = [System.Diagnostics.Process]::Start($info)
  if ($null -eq $process) { throw 'IDA installer did not start' }
  $process.WaitForExit()
  Write-Output ('IDA installer exit code: ' + $process.ExitCode)
  exit $process.ExitCode
} catch {
  $nativeCode = $_.Exception.NativeErrorCode
  $hresult = $_.Exception.HResult
  [Console]::Error.WriteLine(('Failed to launch elevated IDA installer: {0}; native_code={1}; hresult=0x{2:X8}' -f $_.Exception.Message, $nativeCode, $hresult))
  exit 1
}
`, powerShellSingleQuoted(installer), powerShellSingleQuoted(strings.Join(commandLine, " ")), powerShellSingleQuoted(filepath.Dir(installer)))
}

func powerShellSingleQuoted(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func encodePowerShellUTF16LE(value string) string {
	codeUnits := utf16.Encode([]rune(value))
	data := make([]byte, len(codeUnits)*2)
	for index, codeUnit := range codeUnits {
		data[index*2] = byte(codeUnit)
		data[index*2+1] = byte(codeUnit >> 8)
	}
	return base64.StdEncoding.EncodeToString(data)
}
