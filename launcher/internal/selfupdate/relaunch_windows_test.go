//go:build windows

package selfupdate

import (
	"strings"
	"testing"
)

func TestWindowsGUIRestartScriptWaitsForCompletedTargetVersion(t *testing.T) {
	script := windowsGUIRestartScript(
		1234,
		`C:\Program Files\Chat Codex\chat-codex-launcher.exe`,
		`C:\Users\tester\AppData\Local\Chat Codex\apply-status.json`,
		`0.1.135`,
	)
	for _, expected := range []string{
		"Get-Process -Id $parent",
		"$result.state -eq 'failed'",
		"$result.state -eq 'completed'",
		"$result.version -eq $expected",
		"Start-Process -FilePath $target",
		"0.1.135",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("restart script is missing %q: %s", expected, script)
		}
	}
}

func TestPowerShellStringEscapesSingleQuotes(t *testing.T) {
	if got := powershellString(`C:\Users\O'Brien\launcher.exe`); got != `'C:\Users\O''Brien\launcher.exe'` {
		t.Fatalf("unexpected quoted path: %s", got)
	}
}
