//go:build !windows

package process

import "os/exec"

func configureBackgroundCommand(cmd *exec.Cmd) {
	_ = cmd
}
