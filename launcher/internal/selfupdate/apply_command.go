package selfupdate

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RunApplyCommand(args []string) error {
	flags := flag.NewFlagSet("self-update-apply", flag.ContinueOnError)
	source := flags.String("source", "", "新 launcher 路径")
	target := flags.String("target", "", "当前 launcher 路径")
	parent := flags.Int("parent", 0, "父进程 PID")
	timeout := flags.Int("timeout", 45, "等待替换超时秒数")
	healthTimeout := flags.Int("health-timeout", 45, "等待新版健康检查超时秒数")
	expectedVersion := flags.String("expected-version", "", "期望 launcher 版本")
	healthURL := flags.String("health-url", "", "新版 launcher 健康检查地址")
	lockPath := flags.String("lock", "", "自更新锁文件")
	statusPath := flags.String("status", "", "自更新结果文件")
	cleanupSource := flags.Bool("cleanup-source", false, "结束后清理 updater 独占载荷")
	restart := flags.Bool("restart", false, "替换后重启")
	if err := flags.Parse(args); err != nil {
		return err
	}
	restartArgs := []string{}
	if rest := flags.Args(); len(rest) > 0 {
		if rest[0] == "--" {
			rest = rest[1:]
		}
		restartArgs = append(restartArgs, rest...)
	}
	if strings.TrimSpace(*lockPath) != "" {
		_ = setUpdateLockOwner(strings.TrimSpace(*lockPath), os.Getpid())
		defer os.Remove(strings.TrimSpace(*lockPath))
	}
	if *cleanupSource {
		defer os.Remove(strings.TrimSpace(*source))
	}
	if marker := restartMarkerFromStatusPath(*statusPath); marker != "" {
		defer os.Remove(marker)
	}
	status := ApplyStatus{Version: strings.TrimSpace(*expectedVersion), State: "applying", Message: "正在等待旧版 launcher 退出"}
	_ = WriteApplyStatus(*statusPath, status)
	fail := func(err error) error {
		status.State = "failed"
		status.Message = err.Error()
		status.RolledBack = strings.Contains(err.Error(), "回滚")
		_ = WriteApplyStatus(*statusPath, status)
		return err
	}
	if *parent > 0 {
		if err := waitParentExit(*parent, time.Duration(*timeout)*time.Second); err != nil {
			if terminateErr := terminateParentProcess(*parent, strings.TrimSpace(*target)); terminateErr != nil {
				return fail(errors.Join(err, terminateErr))
			}
			if waitErr := waitParentExit(*parent, 10*time.Second); waitErr != nil {
				return fail(errors.Join(err, waitErr))
			}
		}
	}
	status.Message = "正在替换 launcher 并启动新版本"
	_ = WriteApplyStatus(*statusPath, status)
	err := Apply(ApplyCommand{
		Source:          *source,
		Target:          *target,
		ParentPID:       *parent,
		Restart:         *restart,
		RestartArgs:     restartArgs,
		Timeout:         time.Duration(*timeout) * time.Second,
		HealthTimeout:   time.Duration(*healthTimeout) * time.Second,
		ExpectedVersion: strings.TrimSpace(*expectedVersion),
		HealthURL:       strings.TrimSpace(*healthURL),
	})
	if err != nil {
		return fail(err)
	}
	status.State = "completed"
	status.Message = "launcher 自更新完成并通过健康检查"
	status.RolledBack = false
	return WriteApplyStatus(*statusPath, status)
}

func restartMarkerFromStatusPath(statusPath string) string {
	statusPath = strings.TrimSpace(statusPath)
	if statusPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statusPath), "restart-pending")
}

func waitParentExit(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		exists, err := processExists(pid)
		if err != nil {
			return fmt.Errorf("检测父进程 %d 状态失败: %w", pid, err)
		}
		if !exists {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("等待父进程 %d 退出超时", pid)
		}
		if remaining > 300*time.Millisecond {
			remaining = 300 * time.Millisecond
		}
		time.Sleep(remaining)
	}
}
