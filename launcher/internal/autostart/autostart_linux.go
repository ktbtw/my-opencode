package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const linuxSystemServicePath = "/etc/systemd/system/" + serviceName + ".service"

type linuxTarget struct {
	Path   string
	Method string
}

func platformStatus() Info {
	target, err := linuxAutostartTarget()
	if err != nil {
		return Info{Method: "systemd-user", Error: err.Error()}
	}
	_, statErr := os.Stat(target.Path)
	info := Info{Enabled: statErr == nil, Method: target.Method, Path: target.Path}
	if statErr != nil && !os.IsNotExist(statErr) {
		info.Error = statErr.Error()
	}
	if statErr == nil && strings.HasPrefix(target.Method, "systemd-") {
		enabled, err := systemdIsEnabled(target)
		info.Enabled = enabled
		if err != nil {
			info.Error = err.Error()
		}
	}
	return info
}

func platformEnable(options Options) (Info, error) {
	target, err := linuxAutostartTarget()
	if err != nil {
		return Info{}, err
	}
	if !options.DryRun {
		if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
			return Info{}, err
		}
		content := linuxSystemdUnit(options, target.Method)
		if target.Method == "xdg-autostart" {
			content = linuxDesktopEntry(options)
		}
		if err := os.WriteFile(target.Path, content, 0o644); err != nil {
			return Info{}, err
		}
		if strings.HasPrefix(target.Method, "systemd-") {
			if err := systemdDaemonReload(target); err != nil {
				return Info{}, err
			}
			if err := systemdEnable(target); err != nil {
				return Info{}, err
			}
			if err := systemdRequireActive(target); err != nil {
				return Info{}, err
			}
		}
	}
	return Info{Enabled: true, Method: target.Method, Path: target.Path, Command: commandString(options.Executable, options.Args)}, nil
}

func platformDisable() (Info, error) {
	target, err := linuxAutostartTarget()
	if err != nil {
		return Info{}, err
	}
	var disableErr error
	if _, statErr := os.Stat(target.Path); statErr == nil && strings.HasPrefix(target.Method, "systemd-") {
		disableErr = systemdDisable(target)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return Info{}, statErr
	}
	if err := os.Remove(target.Path); err != nil && !os.IsNotExist(err) {
		return Info{}, err
	}
	if strings.HasPrefix(target.Method, "systemd-") {
		if err := systemdDaemonReload(target); err != nil && disableErr == nil {
			disableErr = err
		}
	}
	info := Info{Enabled: false, Method: target.Method, Path: target.Path}
	if disableErr != nil {
		info.Error = disableErr.Error()
	}
	return info, disableErr
}

func linuxAutostartTarget() (linuxTarget, error) {
	if _, err := exec.LookPath("systemctl"); err == nil {
		if os.Geteuid() == 0 {
			return linuxTarget{Path: linuxSystemServicePath, Method: "systemd-system"}, nil
		}
		if systemdUserAvailable() {
			base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
			if base == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return linuxTarget{}, err
				}
				base = filepath.Join(home, ".config")
			}
			return linuxTarget{Path: filepath.Join(base, "systemd", "user", serviceName+".service"), Method: "systemd-user"}, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return linuxTarget{}, err
	}
	return linuxTarget{Path: filepath.Join(home, ".config", "autostart", serviceName+".desktop"), Method: "xdg-autostart"}, nil
}

func systemdUserAvailable() bool {
	cmd := exec.Command("systemctl", "--user", "show-environment")
	return cmd.Run() == nil
}

func systemdCommand(target linuxTarget, args ...string) *exec.Cmd {
	if target.Method == "systemd-user" {
		args = append([]string{"--user"}, args...)
	}
	return exec.Command("systemctl", args...)
}

func systemdDaemonReload(target linuxTarget) error {
	return runSystemdCommand(target, "daemon-reload")
}

func systemdEnable(target linuxTarget) error {
	return runSystemdCommand(target, "enable", "--now", filepath.Base(target.Path))
}

func systemdDisable(target linuxTarget) error {
	return runSystemdCommand(target, "disable", "--now", filepath.Base(target.Path))
}

func systemdIsEnabled(target linuxTarget) (bool, error) {
	out, err := systemdCommand(target, "is-enabled", filepath.Base(target.Path)).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil {
		return strings.EqualFold(text, "enabled"), nil
	}
	lower := strings.ToLower(text)
	if text == "" || lower == "disabled" || strings.Contains(lower, "not-found") || strings.Contains(lower, "no such") {
		return false, nil
	}
	return false, fmt.Errorf("systemctl is-enabled 失败: %s", text)
}

func systemdRequireActive(target linuxTarget) error {
	var last string
	for i := 0; i < 20; i++ {
		out, err := systemdCommand(target, "is-active", filepath.Base(target.Path)).CombinedOutput()
		text := strings.TrimSpace(string(out))
		lower := strings.ToLower(text)
		if err == nil && lower == "active" {
			return nil
		}
		if text == "" && err != nil {
			text = err.Error()
		}
		last = text
		if lower == "activating" || lower == "unknown" {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		break
	}
	if last == "" {
		last = "unknown"
	}
	return fmt.Errorf("systemctl is-active 失败: %s", last)
}

func runSystemdCommand(target linuxTarget, args ...string) error {
	out, err := systemdCommand(target, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = err.Error()
	}
	return errors.New(text)
}

func linuxServicePath() (string, string, error) {
	target, err := linuxAutostartTarget()
	if err != nil {
		return "", "", err
	}
	return target.Path, target.Method, nil
}

func linuxUserServicePath() (string, string, error) {
	if _, err := exec.LookPath("systemctl"); err == nil {
		base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", "", err
			}
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "systemd", "user", serviceName+".service"), "systemd-user", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".config", "autostart", serviceName+".desktop"), "xdg-autostart", nil
}

func linuxSystemdUnit(options Options, method string) []byte {
	var buf bytes.Buffer
	env := copyEnv(options.Env)
	if method == "systemd-system" {
		if _, ok := env["HOME"]; !ok {
			if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
				env["HOME"] = home
			}
		}
	}
	buf.WriteString("[Unit]\n")
	buf.WriteString("Description=Chat Codex Launcher\n")
	buf.WriteString("After=network-online.target\n\n")
	buf.WriteString("[Service]\n")
	buf.WriteString("Type=simple\n")
	buf.WriteString("ExecStart=")
	buf.WriteString(systemdQuote(options.Executable))
	for _, arg := range options.Args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		buf.WriteByte(' ')
		buf.WriteString(systemdQuote(arg))
	}
	buf.WriteByte('\n')
	if len(env) > 0 {
		for _, key := range sortedEnvKeys(env) {
			buf.WriteString("Environment=")
			buf.WriteString(systemdQuote(key + "=" + env[key]))
			buf.WriteByte('\n')
		}
	}
	buf.WriteString("Restart=always\n")
	buf.WriteString("RestartSec=3\n\n")
	buf.WriteString("[Install]\n")
	if method == "systemd-system" {
		buf.WriteString("WantedBy=multi-user.target\n")
	} else {
		buf.WriteString("WantedBy=default.target\n")
	}
	return buf.Bytes()
}

func copyEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}

func linuxDesktopEntry(options Options) []byte {
	var buf bytes.Buffer
	buf.WriteString("[Desktop Entry]\n")
	buf.WriteString("Type=Application\n")
	buf.WriteString("Name=Chat Codex Launcher\n")
	buf.WriteString("Exec=")
	buf.WriteString(shellQuote(options.Executable))
	for _, arg := range options.Args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		buf.WriteByte(' ')
		buf.WriteString(shellQuote(arg))
	}
	buf.WriteByte('\n')
	buf.WriteString("X-GNOME-Autostart-enabled=true\n")
	return buf.Bytes()
}

func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key, value := range env {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func systemdQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
