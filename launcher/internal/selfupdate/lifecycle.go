package selfupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrUpdateInProgress = errors.New("launcher 自更新正在执行")

type ApplyStatus struct {
	Version    string    `json:"version,omitempty"`
	State      string    `json:"state"`
	Message    string    `json:"message,omitempty"`
	RolledBack bool      `json:"rolled_back,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type updateLockRecord struct {
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
}

type updateLock struct {
	path string
}

func acquireUpdateLock(runtimeDir string) (*updateLock, error) {
	path := filepath.Join(runtimeDir, "self-updates", "update.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			record := updateLockRecord{PID: os.Getpid(), CreatedAt: time.Now().UTC()}
			encodeErr := json.NewEncoder(file).Encode(record)
			closeErr := file.Close()
			if encodeErr != nil || closeErr != nil {
				_ = os.Remove(path)
				return nil, errors.Join(encodeErr, closeErr)
			}
			return &updateLock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		stale, staleErr := updateLockStale(path)
		if staleErr != nil || !stale {
			return nil, ErrUpdateInProgress
		}
		_ = os.Remove(path)
	}
	return nil, ErrUpdateInProgress
}

func updateLockStale(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.Is(err, os.ErrNotExist), err
	}
	var record updateLockRecord
	if err := json.Unmarshal(data, &record); err != nil || record.PID <= 0 {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return false, statErr
		}
		return time.Since(info.ModTime()) > 10*time.Minute, nil
	}
	exists, err := processExists(record.PID)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

func setUpdateLockOwner(path string, pid int) error {
	if strings.TrimSpace(path) == "" || pid <= 0 {
		return nil
	}
	data, err := json.Marshal(updateLockRecord{PID: pid, CreatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (l *updateLock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *updateLock) Release() {
	if l != nil && strings.TrimSpace(l.path) != "" {
		_ = os.Remove(l.path)
	}
}

func applyStatusPath(runtimeDir string) string {
	return filepath.Join(strings.TrimSpace(runtimeDir), "self-updates", "apply-status.json")
}

func WriteApplyStatus(path string, status ApplyStatus) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	status.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if retryErr := os.Rename(tmp, path); retryErr != nil {
			_ = os.Remove(tmp)
			return retryErr
		}
	}
	return nil
}

func ReadApplyStatus(runtimeDir string) (ApplyStatus, bool) {
	data, err := os.ReadFile(applyStatusPath(runtimeDir))
	if err != nil {
		return ApplyStatus{}, false
	}
	var status ApplyStatus
	if err := json.Unmarshal(data, &status); err != nil || strings.TrimSpace(status.State) == "" {
		return ApplyStatus{}, false
	}
	return status, true
}

func waitLauncherHealthy(rawURL string, expectedVersion string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(rawURL)
		if err == nil {
			var state struct {
				Status          string `json:"status"`
				LauncherVersion string `json:"launcher_version"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&state)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil && strings.TrimSpace(state.LauncherVersion) == strings.TrimSpace(expectedVersion) {
				return nil
			}
			lastErr = fmt.Errorf("控制口返回版本 %q，期望 %q", state.LauncherVersion, expectedVersion)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("控制口未恢复")
	}
	return lastErr
}

func rollbackExecutable(replacement executableReplacement) error {
	if strings.TrimSpace(replacement.Target) == "" {
		return nil
	}
	_ = os.Remove(replacement.Staged)
	_ = os.Remove(replacement.Target)
	if _, err := os.Stat(replacement.Backup); err == nil {
		if err := os.Rename(replacement.Backup, replacement.Target); err != nil {
			return fmt.Errorf("恢复旧版 launcher 失败: %w", err)
		}
	}
	_ = os.Chmod(replacement.Target, 0o755)
	return nil
}

func finalizeReplacement(replacement executableReplacement) {
	_ = os.Remove(replacement.Backup)
	_ = os.Remove(replacement.Staged)
	_ = os.Remove(replacement.Source)
}

func restartExecutable(target string, args []string) error {
	if _, err := startDetachedCommand(target, args); err != nil {
		return fmt.Errorf("回滚后重启旧版 launcher 失败: %w", err)
	}
	return nil
}

func CleanupAsync(runtimeDir string) {
	for _, delay := range []time.Duration{3 * time.Second, 30 * time.Second} {
		delay := delay
		time.AfterFunc(delay, func() {
			_ = Cleanup(runtimeDir)
		})
	}
}

func Cleanup(runtimeDir string) error {
	root := filepath.Join(strings.TrimSpace(runtimeDir), "self-updates")
	active, err := activeUpdateTransaction(root)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var cleanupErr error
	for _, entry := range entries {
		name := entry.Name()
		if name == "apply-status.json" {
			continue
		}
		path := filepath.Join(root, name)
		if err := os.RemoveAll(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func activeUpdateTransaction(root string) (bool, error) {
	lockPath := filepath.Join(root, "update.lock")
	if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	stale, err := updateLockStale(lockPath)
	if err != nil {
		return false, err
	}
	if !stale {
		return true, nil
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return false, nil
}

func UpdateInProgress(runtimeDir string) (bool, error) {
	root := filepath.Join(strings.TrimSpace(runtimeDir), "self-updates")
	return activeUpdateTransaction(root)
}

func ApplyStatusAppliesToVersion(status ApplyStatus, currentVersion string) bool {
	statusVersion := strings.TrimSpace(status.Version)
	currentVersion = strings.TrimSpace(currentVersion)
	if statusVersion == "" || currentVersion == "" {
		return true
	}
	comparison, ok := compareDottedVersion(statusVersion, currentVersion)
	if !ok {
		return !strings.EqualFold(strings.TrimSpace(status.State), "completed") || statusVersion == currentVersion
	}
	if strings.EqualFold(strings.TrimSpace(status.State), "completed") {
		return comparison == 0
	}
	return comparison >= 0
}
