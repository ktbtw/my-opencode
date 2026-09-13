package launcherlog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const maxLogSize = 8 << 20

var (
	mu          sync.Mutex
	currentPath string
	currentFile *os.File
)

func Configure(runtimeDir string) error {
	path := filepath.Join(runtimeDir, "logs", "launcher.log")
	mu.Lock()
	defer mu.Unlock()
	if path == currentPath && currentFile != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if stat, err := os.Stat(path); err == nil && stat.Size() >= maxLogSize {
		_ = os.Remove(path + ".1")
		if err := os.Rename(path, path+".1"); err != nil {
			return fmt.Errorf("rotate launcher log: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if currentFile != nil {
		_ = currentFile.Close()
	}
	currentPath = path
	currentFile = file
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	return nil
}

func Path(runtimeDir string) string {
	return filepath.Join(runtimeDir, "logs", "launcher.log")
}
