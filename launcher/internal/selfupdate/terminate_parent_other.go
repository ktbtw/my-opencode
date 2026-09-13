//go:build !windows

package selfupdate

import "fmt"

func terminateParentProcess(pid int, expectedExecutable string) error {
	return fmt.Errorf("旧 launcher 进程 %d 优雅退出超时", pid)
}
