//go:build windows

package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func ScheduleGUIRestart(runtimeDir, target, expectedVersion string) error {
	runtimeDir = strings.TrimSpace(runtimeDir)
	target = strings.TrimSpace(target)
	if runtimeDir == "" || target == "" {
		return errors.New("GUI 重启路径不能为空")
	}
	script := windowsGUIRestartScript(
		os.Getpid(),
		target,
		applyStatusPath(runtimeDir),
		expectedVersion,
	)
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle",
		"Hidden",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
	if _, err := startDetachedCommand(cmd.Path, cmd.Args[1:]); err != nil {
		return fmt.Errorf("启动 GUI 重启等待进程失败: %w", err)
	}
	return nil
}

func windowsGUIRestartScript(parentPID int, target, statusPath, expectedVersion string) string {
	return fmt.Sprintf(`$target = %s
$parent = %d
$status = %s
$expected = %s
$deadline = (Get-Date).AddMinutes(5)
while ((Get-Process -Id $parent -ErrorAction SilentlyContinue) -and (Get-Date) -lt $deadline) {
  Start-Sleep -Milliseconds 200
}
while ((Get-Date) -lt $deadline) {
  if (Test-Path -LiteralPath $status) {
    try {
      $result = Get-Content -LiteralPath $status -Raw | ConvertFrom-Json
      if ($result.state -eq 'failed') { exit 1 }
      if ($result.state -eq 'completed' -and ($expected -eq '' -or $result.version -eq $expected)) {
        Start-Process -FilePath $target
        exit 0
      }
    } catch {}
  }
  Start-Sleep -Milliseconds 300
}
exit 1`,
		powershellString(target),
		parentPID,
		powershellString(statusPath),
		powershellString(strings.TrimSpace(expectedVersion)),
	)
}

func powershellString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
