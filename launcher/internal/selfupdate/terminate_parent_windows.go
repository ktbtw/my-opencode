//go:build windows

package selfupdate

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func terminateParentProcess(pid int, expectedExecutable string) error {
	if pid <= 0 || strings.TrimSpace(expectedExecutable) == "" {
		return fmt.Errorf("旧 launcher 进程或目标路径为空")
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return fmt.Errorf("打开旧 launcher 进程 %d 失败: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return fmt.Errorf("读取旧 launcher 进程 %d 路径失败: %w", pid, err)
	}
	actual := filepath.Clean(windows.UTF16ToString(buffer[:size]))
	expected := filepath.Clean(expectedExecutable)
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("旧进程路径 %q 与更新目标 %q 不一致", actual, expected)
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("终止超时的旧 launcher 进程 %d 失败: %w", pid, err)
	}
	return nil
}
