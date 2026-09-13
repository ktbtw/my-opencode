//go:build !windows

package app

import "fmt"

func runWindowsElevatedPowerShell(string) (string, error) {
	return "", fmt.Errorf("Windows OpenSSH setup is only available on Windows")
}
