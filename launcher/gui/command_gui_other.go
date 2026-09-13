//go:build !windows

package main

import "os/exec"

func configureGUICommand(cmd *exec.Cmd) {
	_ = cmd
}
