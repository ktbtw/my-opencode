//go:build !windows

package backgroundcmd

import "os/exec"

func Configure(cmd *exec.Cmd) {
	_ = cmd
}
