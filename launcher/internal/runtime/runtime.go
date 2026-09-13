package runtime

import (
	"os"
	"path/filepath"
)

func Ensure(runtimeDir string) error {
	paths := []string{
		runtimeDir,
		filepath.Join(runtimeDir, "agents"),
		filepath.Join(runtimeDir, "caches"),
		filepath.Join(runtimeDir, "downloads"),
		filepath.Join(runtimeDir, "logs"),
		filepath.Join(runtimeDir, "runtimes"),
		filepath.Join(runtimeDir, "versions"),
		filepath.Join(runtimeDir, "work"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}
