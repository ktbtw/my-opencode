//go:build windows

package main

import "os/exec"

// configureGUICommand leaves the child process in the interactive desktop.
// CREATE_NO_WINDOW is reserved for the background service; applying it here
// makes a desktop handoff process survive without presenting a Wails window.
func configureGUICommand(cmd *exec.Cmd) {
	_ = cmd
}
