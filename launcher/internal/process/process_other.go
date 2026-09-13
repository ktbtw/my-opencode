//go:build !windows

package process

import (
	"context"
	"time"
)

func platformWindowsProcessExists(int) (bool, error) {
	return false, nil
}

func platformStopWindowsTree(context.Context, int, time.Duration) error {
	return nil
}

func platformProcessesByExecutable(string) ([]int, error) {
	return nil, nil
}
