//go:build windows

package backgroundcmd

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func Configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
