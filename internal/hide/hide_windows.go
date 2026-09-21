//go:build windows

// Package hide keeps child processes off the screen: the application has no console, and on Windows a console child
// (gh, git, codex, powershell) would otherwise open its own window for a moment.
package hide

import (
	"os/exec"
	"syscall"
)

func Cmd(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c
}
