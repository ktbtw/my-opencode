//go:build !windows

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
	statusPath := applyStatusPath(runtimeDir)
	versionPattern := strings.TrimSpace(expectedVersion)
	if versionPattern == "" {
		versionPattern = `"state":"completed"`
	}
	script := fmt.Sprintf(
		"while kill -0 %d 2>/dev/null; do sleep 0.2; done; "+
			"deadline=$(( $(date +%%s) + 300 )); "+
			"while [ $(date +%%s) -lt $deadline ]; do "+
			"if grep -Fq '\"state\":\"failed\"' %s 2>/dev/null; then exit 1; fi; "+
			"if grep -Fq '\"state\":\"completed\"' %s 2>/dev/null && "+
			"grep -Fq %s %s 2>/dev/null; then exec %s; fi; "+
			"sleep 0.3; done; exit 1",
		os.Getpid(),
		shellQuote(statusPath),
		shellQuote(statusPath),
		shellQuote(versionPattern),
		shellQuote(statusPath),
		shellQuote(target),
	)
	cmd := exec.Command("/bin/sh", "-c", script)
	detachCommand(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 GUI 重启等待进程失败: %w", err)
	}
	return cmd.Process.Release()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
