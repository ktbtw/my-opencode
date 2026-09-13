//go:build windows

package selfupdate

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

const createNoWindow = 0x08000000
const createBreakawayFromJob = 0x01000000

func detachCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow | createBreakawayFromJob,
		HideWindow:    true,
	}
}

func startDetachedCommand(executable string, args []string) (int, error) {
	cmd := exec.Command(executable, args...)
	detachCommand(cmd)
	if err := cmd.Start(); err == nil {
		pid := cmd.Process.Pid
		return pid, cmd.Process.Release()
	} else if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return 0, err
	}
	return startScheduledTaskCommand(executable, args)
}

func startScheduledTaskCommand(executable string, args []string) (int, error) {
	taskName := fmt.Sprintf("ChatCodexDetached-%d-%d", syscall.Getpid(), time.Now().UnixNano())
	argLine := make([]string, 0, len(args))
	for _, arg := range args {
		argLine = append(argLine, syscall.EscapeArg(arg))
	}
	script := scheduledTaskScript(taskName, executable, strings.Join(argLine, " "))
	encoded := encodePowerShellScript(script)
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encoded,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
		HideWindow:    true,
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("通过计划任务启动独立进程失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	const marker = "CHAT_CODEX_PID="
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, marker)))
		if parseErr == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("计划任务已提交，但未获取到新进程 PID: %s", strings.TrimSpace(string(output)))
}

func scheduledTaskScript(taskName string, executable string, argLine string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$taskName = %s
$executable = %s
$arguments = %s
$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$before = @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $executable } | ForEach-Object { [int]$_.ProcessId })
$action = New-ScheduledTaskAction -Execute $executable -Argument $arguments
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(5)
$trigger.EndBoundary = (Get-Date).AddMinutes(15).ToString('s')
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -DeleteExpiredTaskAfter (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
$deadline = (Get-Date).AddSeconds(8)
while ((Get-Date) -lt $deadline) {
  $candidate = Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $executable -and $before -notcontains [int]$_.ProcessId } | Sort-Object CreationDate -Descending | Select-Object -First 1
  if ($candidate) {
    Write-Output ('CHAT_CODEX_PID=' + $candidate.ProcessId)
    exit 0
  }
  Start-Sleep -Milliseconds 100
}
throw '计划任务未在等待时间内创建目标进程'`,
		powershellString(taskName),
		powershellString(executable),
		powershellString(argLine),
	)
}

func encodePowerShellScript(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		data[index*2] = byte(unit)
		data[index*2+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(data)
}
