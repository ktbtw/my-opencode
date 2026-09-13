//go:build windows

package app

import (
	"strings"
	"testing"
)

func TestElevatedIDAInstallerPowerShellUsesRunAsAndWaits(t *testing.T) {
	script := elevatedIDAInstallerPowerShell(`C:\Fixture's Dir\ida-pro.exe`, []string{
		"--mode", "unattended",
		"--prefix", `C:\Managed Runtime\ida\9.2`,
		"--install_python", "1",
	})
	for _, expected := range []string{
		"$info.Verb = 'runas'",
		"ProcessWindowStyle]::Normal",
		"Launcher is already elevated",
		"Requesting Windows administrator confirmation",
		"native_code=",
		"$process.WaitForExit()",
		`C:\Fixture''s Dir\ida-pro.exe`,
		`"C:\Managed Runtime\ida\9.2"`,
		"--install_python 1",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("elevated installer script missing %q:\n%s", expected, script)
		}
	}
	if strings.Contains(script, "ProcessWindowStyle]::Hidden") {
		t.Fatalf("elevated installer must not hide its interactive window:\n%s", script)
	}
}
