package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launcher/internal/binarymeta"
	"launcher/internal/fileutil"
)

type Manifest struct {
	Version    string `json:"version"`
	Channel    string `json:"channel,omitempty"`
	Executable string `json:"executable"`
}

const currentVersionFile = "current-version"

var ErrExecutableNotFound = errors.New("未找到 opencode 可执行文件")

func Versions(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}

func Current(root string) (string, error) {
	path := filepath.Join(root, currentVersionFile)
	data, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", err
}

func CurrentExecutable(root string) (string, string, error) {
	current, err := Current(root)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(current) == "" {
		return "", "", os.ErrNotExist
	}
	path, err := ExecutablePath(root, current)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("current executable is a directory: %s", path)
	}
	return current, path, nil
}

func ManifestFor(root string, version string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "versions", version, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Executable == "" {
		return nil, errors.New("manifest 缺少 executable")
	}
	return &manifest, nil
}

func ExecutablePath(root string, version string) (string, error) {
	manifest, err := ManifestFor(root, version)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "versions", version, manifest.Executable), nil
}

func WriteManifest(root string, version string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, "versions", version, "manifest.json")
	return fileutil.AtomicWriteFile(path, append(data, '\n'), 0o644)
}

func Switch(root string, version string) error {
	target := filepath.Join(root, "versions", version)
	if _, err := os.Stat(target); err != nil {
		return err
	}
	data := []byte(version + "\n")
	return fileutil.AtomicWriteFile(filepath.Join(root, currentVersionFile), data, 0o644)
}

func EnsureLocal(root string, binary string) (string, error) {
	current, _, err := CurrentExecutable(root)
	if err == nil && current != "" {
		return current, nil
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrExecutableNotFound, binary)
	}
	version := "local"
	dir := filepath.Join(root, "versions", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := binarymeta.ExecutableName(filepath.Base(path))
	target := filepath.Join(dir, name)
	if err := copyFile(path, target); err != nil {
		return "", err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return "", err
	}
	if err := WriteManifest(root, version, Manifest{
		Version:    version,
		Channel:    "local",
		Executable: name,
	}); err != nil {
		return "", err
	}
	if err := Switch(root, version); err != nil {
		return "", err
	}
	return version, nil
}

func copyFile(src string, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
