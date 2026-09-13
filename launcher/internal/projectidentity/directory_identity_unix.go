//go:build !windows

package projectidentity

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func directoryIdentity(root string) (canonical, volumeID, fileID string, err error) {
	canonical, err = filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(canonical); resolveErr == nil {
		canonical = resolved
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", "", "", err
	}
	if !info.IsDir() {
		return "", "", "", fmt.Errorf("project root is not a directory: %s", canonical)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return canonical, "", "", nil
	}
	return canonical, fmt.Sprintf("%d", stat.Dev), fmt.Sprintf("%d", stat.Ino), nil
}
