//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func platformWindowsProcessExists(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return true, nil
		}
		return false, err
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, err
	}
	return exitCode == stillActive, nil
}

func platformProcessesByExecutable(path string) ([]int, error) {
	want := strings.ToLower(filepath.Clean(path))
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	var matches []int
	for {
		pid := int(entry.ProcessID)
		if image, imageErr := windowsProcessImagePath(uint32(pid)); imageErr == nil && strings.EqualFold(filepath.Clean(image), want) {
			matches = append(matches, pid)
		}
		err := windows.Process32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func windowsProcessImagePath(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, windows.MAX_PATH)
	for size := uint32(len(buffer)); ; size = uint32(len(buffer)) {
		if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err == nil {
			return windows.UTF16ToString(buffer[:size]), nil
		} else if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return "", err
		}
		buffer = make([]uint16, len(buffer)*2)
		if len(buffer) > 32768 {
			return "", windows.ERROR_INSUFFICIENT_BUFFER
		}
	}
}

func platformStopWindowsTree(ctx context.Context, pid int, interval time.Duration) error {
	if pid <= 0 {
		return nil
	}
	running, err := platformWindowsProcessExists(pid)
	if err != nil || !running {
		return err
	}
	processes, err := windowsProcessTree(uint32(pid))
	if err != nil {
		return fmt.Errorf("枚举 Windows 进程树失败: %w", err)
	}
	failed := map[uint32]error{}
	for index := len(processes) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		processID := processes[index]
		if err := terminateWindowsProcess(processID); err != nil {
			failed[processID] = err
		}
	}
	if err := WaitForExit(ctx, pid, interval); err != nil {
		return err
	}
	for processID, stopErr := range failed {
		running, checkErr := platformWindowsProcessExists(int(processID))
		if checkErr != nil {
			return checkErr
		}
		if running {
			return fmt.Errorf("终止 Windows 进程 %d 失败: %w", processID, stopErr)
		}
	}
	return nil
}

func windowsProcessTree(root uint32) ([]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	children := map[uint32][]uint32{}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	for {
		children[entry.ParentProcessID] = append(children[entry.ParentProcessID], entry.ProcessID)
		err := windows.Process32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	result := make([]uint32, 0, 8)
	seen := map[uint32]bool{}
	var visit func(uint32)
	visit = func(processID uint32) {
		if processID == 0 || seen[processID] {
			return
		}
		seen[processID] = true
		result = append(result, processID)
		for _, child := range children[processID] {
			visit(child)
		}
	}
	visit(root)
	return result, nil
}

func terminateWindowsProcess(pid uint32) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	if err := windows.TerminateProcess(handle, 1); err != nil {
		var exitCode uint32
		if codeErr := windows.GetExitCodeProcess(handle, &exitCode); codeErr == nil && exitCode != stillActive {
			return nil
		}
		return err
	}
	return nil
}
