package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func platformStatus() Info {
	path, err := launchAgentPath()
	if err != nil {
		return Info{Method: "launchd", Error: err.Error()}
	}
	_, statErr := os.Stat(path)
	info := Info{Enabled: statErr == nil, Method: "launchd", Path: path}
	if statErr != nil && !os.IsNotExist(statErr) {
		info.Error = statErr.Error()
	}
	if data, err := os.ReadFile(path); err == nil {
		args := launchAgentProgramArguments(data)
		if len(args) > 0 {
			info.Command = commandString(args[0], args[1:])
		}
	}
	return info
}

func platformEnable(options Options) (Info, error) {
	path, err := launchAgentPath()
	if err != nil {
		return Info{}, err
	}
	content, err := launchAgentPlist(options)
	if err != nil {
		return Info{}, err
	}
	if !options.DryRun {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Info{}, err
		}
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) && launchAgentLoaded() {
			return Info{Enabled: true, Method: "launchd", Path: path, Command: commandString(options.Executable, options.Args)}, nil
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return Info{}, err
		}
		domain := "gui/" + userID()
		_ = exec.Command("launchctl", "bootout", domain, path).Run()
		if err := runLaunchctl("bootstrap", domain, path); err != nil {
			return Info{}, err
		}
		if err := runLaunchctl("enable", domain+"/com.chatcodex.launcher"); err != nil {
			return Info{}, err
		}
	}
	return Info{Enabled: true, Method: "launchd", Path: path, Command: commandString(options.Executable, options.Args)}, nil
}

func launchAgentLoaded() bool {
	uid := userID()
	if uid == "" {
		return false
	}
	return exec.Command("launchctl", "print", "gui/"+uid+"/com.chatcodex.launcher").Run() == nil
}

func launchAgentProgramArguments(data []byte) []string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	inProgramArguments := false
	inKey := false
	inString := false
	var currentKey string
	var args []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "key":
				inKey = true
				currentKey = ""
			case "array":
				if currentKey == "ProgramArguments" {
					inProgramArguments = true
				}
			case "string":
				inString = true
			}
		case xml.EndElement:
			switch item.Name.Local {
			case "key":
				inKey = false
			case "array":
				if inProgramArguments {
					return args
				}
			case "string":
				inString = false
			}
		case xml.CharData:
			value := strings.TrimSpace(string(item))
			if value == "" {
				continue
			}
			if inKey {
				currentKey = value
				continue
			}
			if !inString {
				continue
			}
			if inProgramArguments {
				args = append(args, value)
				continue
			}
		}
	}
	return args
}

func platformDisable() (Info, error) {
	path, err := launchAgentPath()
	if err != nil {
		return Info{}, err
	}
	_ = exec.Command("launchctl", "bootout", "gui/"+userID(), path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return Info{}, err
	}
	return Info{Enabled: false, Method: "launchd", Path: path}, nil
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.chatcodex.launcher.plist"), nil
}

func launchAgentPlist(options Options) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buf.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistKeyString(&buf, "Label", "com.chatcodex.launcher")
	writePlistKeyArray(&buf, "ProgramArguments", append([]string{options.Executable}, options.Args...))
	writePlistKeyBool(&buf, "RunAtLoad", true)
	writePlistKeyBool(&buf, "KeepAlive", options.KeepAlive)
	if len(options.Env) > 0 {
		writePlistEnv(&buf, options.Env)
	}
	buf.WriteString("</dict>\n</plist>\n")
	return buf.Bytes(), nil
}

func writePlistKeyString(buf *bytes.Buffer, key string, value string) {
	buf.WriteString("  <key>")
	xml.EscapeText(buf, []byte(key))
	buf.WriteString("</key>\n  <string>")
	xml.EscapeText(buf, []byte(value))
	buf.WriteString("</string>\n")
}

func writePlistKeyArray(buf *bytes.Buffer, key string, values []string) {
	buf.WriteString("  <key>")
	xml.EscapeText(buf, []byte(key))
	buf.WriteString("</key>\n  <array>\n")
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		buf.WriteString("    <string>")
		xml.EscapeText(buf, []byte(value))
		buf.WriteString("</string>\n")
	}
	buf.WriteString("  </array>\n")
}

func writePlistKeyBool(buf *bytes.Buffer, key string, value bool) {
	buf.WriteString("  <key>")
	xml.EscapeText(buf, []byte(key))
	if value {
		buf.WriteString("</key>\n  <true/>\n")
	} else {
		buf.WriteString("</key>\n  <false/>\n")
	}
}

func writePlistEnv(buf *bytes.Buffer, env map[string]string) {
	keys := make([]string, 0, len(env))
	for key, value := range env {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buf.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	for _, key := range keys {
		buf.WriteString("    <key>")
		xml.EscapeText(buf, []byte(key))
		buf.WriteString("</key>\n    <string>")
		xml.EscapeText(buf, []byte(env[key]))
		buf.WriteString("</string>\n")
	}
	buf.WriteString("  </dict>\n")
}

func userID() string {
	out, err := exec.Command("id", "-u").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runLaunchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = err.Error()
	}
	return errors.New(text)
}
