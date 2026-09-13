//go:build !windows

package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func installSSHAuthorizedKey(publicKey string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == publicKey {
			return true, nil
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer file.Close()
	if _, err := file.WriteString(publicKey + "\n"); err != nil {
		return false, err
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	return false, os.Chmod(path, 0o600)
}

func installSSHManagedAuthorizedKey(publicKey string, expiresAt time.Time) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	managedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return false, err
	}
	desired := `expiry-time="` + expiresAt.UTC().Format("20060102150405Z") + `" ` + publicKey + " " + sshManagedAuthorizedKeyComment
	alreadyExists := false
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			kept = append(kept, line)
			continue
		}
		key, comment, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(trimmed))
		isManagedKey := parseErr == nil && bytes.Equal(key.Marshal(), managedKey.Marshal()) &&
			(comment == sshManagedAuthorizedKeyComment || strings.HasSuffix(trimmed, " "+sshManagedAuthorizedKeyComment))
		if isManagedKey {
			alreadyExists = alreadyExists || trimmed == desired
			continue
		}
		kept = append(kept, line)
	}
	content := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if content != "" {
		content += "\n"
	}
	content += desired + "\n"
	tmp, err := os.CreateTemp(dir, "authorized_keys.tmp-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return false, err
	}
	if _, err := tmp.WriteString(content); err != nil {
		cleanup()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return false, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return false, err
	}
	return alreadyExists, os.Chmod(path, 0o600)
}
