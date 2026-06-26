//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow prevents the child process from opening a console window.
// Required when the tray itself is built with -H=windowsgui.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
