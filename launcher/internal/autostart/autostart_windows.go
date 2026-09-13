package autostart

import (
	"errors"
	"os/exec"
	"strings"

	"launcher/internal/backgroundcmd"
)

const windowsRunKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const windowsValueName = "ChatCodexLauncher"

func platformStatus() Info {
	out, err := windowsRegistryCommand("query", windowsRunKey, "/v", windowsValueName).CombinedOutput()
	info := Info{Enabled: err == nil, Method: "registry-run", Path: windowsRunKey + `\` + windowsValueName}
	if err == nil {
		info.Command = parseWindowsRegCommand(string(out))
		return info
	}
	text := strings.TrimSpace(string(out))
	if text != "" && !strings.Contains(strings.ToLower(text), "unable to find") {
		info.Error = text
	}
	return info
}

func platformEnable(options Options) (Info, error) {
	command := windowsCommand(options.Executable, options.Args)
	if !options.DryRun {
		out, err := windowsRegistryCommand("add", windowsRunKey, "/v", windowsValueName, "/t", "REG_SZ", "/d", command, "/f").CombinedOutput()
		if err != nil {
			return Info{}, errors.New(strings.TrimSpace(string(out)))
		}
	}
	return Info{Enabled: true, Method: "registry-run", Path: windowsRunKey + `\` + windowsValueName, Command: command}, nil
}

func platformDisable() (Info, error) {
	out, err := windowsRegistryCommand("delete", windowsRunKey, "/v", windowsValueName, "/f").CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" && !strings.Contains(strings.ToLower(text), "unable to find") {
			return Info{}, errors.New(text)
		}
	}
	return Info{Enabled: false, Method: "registry-run", Path: windowsRunKey + `\` + windowsValueName}, nil
}

func windowsRegistryCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("reg", args...)
	backgroundcmd.Configure(cmd)
	return cmd
}

func windowsCommand(executable string, args []string) string {
	parts := []string{windowsQuote(executable)}
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		parts = append(parts, windowsQuote(arg))
	}
	return strings.Join(parts, " ")
}

func windowsQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func parseWindowsRegCommand(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 && strings.EqualFold(fields[0], windowsValueName) {
			return strings.Join(fields[2:], " ")
		}
	}
	return ""
}
