//go:build windows

package app

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"launcher/internal/model"
)

func TestWrapWindowsElevatedPowerShellScriptCapturesOutputAndErrors(t *testing.T) {
	script := wrapWindowsElevatedPowerShellScript("Write-Output 'ok'", `C:\Temp\stdout.txt`, `C:\Temp\error.txt`)
	for _, expected := range []string{
		`C:\Temp\stdout.txt`, `C:\Temp\error.txt`, "Write-Output 'ok'",
		"Format-List * -Force", "InvocationInfo.PositionMessage", "exit 1",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("wrapped elevated script missing %q: %s", expected, script)
		}
	}
}

func TestWriteWindowsPowerShellScriptUsesUTF16LEWithBOM(t *testing.T) {
	path, err := writeWindowsPowerShellScript("Write-Output '三闻鱼'")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xfe {
		t.Fatalf("PowerShell script has no UTF-16LE BOM: %x", data)
	}
	units := make([]uint16, (len(data)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2+i*2:])
	}
	if got := string(utf16.Decode(units)); got != "Write-Output '三闻鱼'" {
		t.Fatalf("unexpected PowerShell script content: %q", got)
	}
}

func TestWindowsSSHAccountIncludesDomain(t *testing.T) {
	account := windowsSSHAccount(`C:\Users\三闻鱼`)
	if strings.TrimSpace(account) == "" {
		t.Fatal("Windows SSH account is empty")
	}
	if sid := windowsSSHUserSID(); !strings.HasPrefix(sid, "S-") {
		t.Fatalf("unexpected Windows SSH SID: %q", sid)
	}
}

func TestWindowsManagedAuthorizedKeyIntegration(t *testing.T) {
	keyFile := strings.TrimSpace(os.Getenv("CHAT_CODEX_TEST_PUBLIC_KEY_FILE"))
	if keyFile == "" {
		t.Skip("set CHAT_CODEX_TEST_PUBLIC_KEY_FILE to run the elevated Windows integration test")
	}
	publicKey, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	result := (&service{}).SSHInstallManagedAuthorizedKey(model.SSHManagedAuthorizedKeyInput{
		PublicKey: strings.TrimSpace(string(publicKey)),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if !result.Installed || result.Error != "" {
		t.Fatalf("managed Windows authorized key install failed: %+v", result)
	}
}

func TestWindowsElevatedPowerShellIntegration(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CHAT_CODEX_TEST_ELEVATION")) == "" {
		t.Skip("set CHAT_CODEX_TEST_ELEVATION to run the elevated Windows integration test")
	}
	output, err := runWindowsElevatedPowerShell("Write-Output 'ok'")
	if err != nil || strings.TrimSpace(output) != "ok" {
		t.Fatalf("elevated PowerShell smoke test failed: output=%q err=%v", output, err)
	}
}
