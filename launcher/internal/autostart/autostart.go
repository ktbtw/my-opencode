package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const serviceName = "chat-codex-launcher"

type Info struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method"`
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Options struct {
	Executable string
	Args       []string
	Env        map[string]string
	DryRun     bool
	KeepAlive  bool
}

type statusCache struct {
	mu     sync.Mutex
	loaded bool
	info   Info
}

var cachedStatus statusCache

func Status() Info {
	return cachedStatus.load(platformStatus, false)
}

func Refresh() Info {
	return cachedStatus.load(platformStatus, true)
}

func Enable(options Options) (Info, error) {
	executable, err := normalizeExecutable(options.Executable)
	if err != nil {
		return Info{}, err
	}
	if IsUpdaterExecutablePath(executable) {
		return Info{}, errors.New("不能将 launcher 临时更新进程写入自启动")
	}
	options.Executable = executable
	return cachedStatus.mutate(func() (Info, error) {
		return platformEnable(options)
	}, !options.DryRun)
}

func Disable() (Info, error) {
	return cachedStatus.mutate(platformDisable, true)
}

func (c *statusCache) load(loader func() Info, refresh bool) Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded && !refresh {
		return c.info
	}
	c.info = loader()
	c.loaded = true
	return c.info
}

func (c *statusCache) mutate(operation func() (Info, error), cacheResult bool) (Info, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info, err := operation()
	if err == nil && cacheResult {
		c.info = info
		c.loaded = true
	}
	return info, err
}

func normalizeExecutable(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("launcher 可执行文件路径不能为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("launcher 可执行文件路径不能是目录")
	}
	return abs, nil
}

func commandString(executable string, args []string) string {
	parts := []string{executable}
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

func IsUpdaterExecutablePath(path string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	base := strings.ToLower(filepath.Base(normalized))
	return strings.HasPrefix(base, "launcher-updater-")
}

func CommandUsesUpdaterExecutable(command string) bool {
	return IsUpdaterExecutablePath(firstCommandToken(command))
}

func CommandUsesTerminalCompat(command string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(command), "\\", "/"))
	return strings.Contains(normalized, "/usr/bin/open") &&
		strings.Contains(normalized, "terminal") &&
		strings.Contains(normalized, "start-terminal-compat.command")
}

func firstCommandToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] != '"' {
		if idx := strings.IndexFunc(command, func(r rune) bool { return r == ' ' || r == '\t' }); idx >= 0 {
			return command[:idx]
		}
		return command
	}
	var b strings.Builder
	for _, r := range command[1:] {
		if r == '"' {
			return b.String()
		}
		b.WriteRune(r)
	}
	return b.String()
}
