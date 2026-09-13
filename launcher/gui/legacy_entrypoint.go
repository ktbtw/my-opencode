package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func removeLegacyGUIEntrypointsWithRetry(source string, candidates []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = removeLegacyGUIEntrypoints(source, candidates)
		if lastErr == nil || time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func removeLegacyGUIEntrypoints(source string, candidates []string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	var removeErrs []error
	seen := map[string]struct{}{}
	for _, target := range candidates {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(target))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		info, statErr := os.Stat(target)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			removeErrs = append(removeErrs, statErr)
			continue
		}
		if !info.Mode().IsRegular() || sameExecutable(source, target) {
			continue
		}
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			removeErrs = append(removeErrs, fmt.Errorf("删除旧桌面 Launcher %s: %w", target, removeErr))
		}
	}
	return errors.Join(removeErrs...)
}
