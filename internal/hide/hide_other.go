//go:build !windows

// Package hide keeps child processes off the screen; on macOS there is nothing to hide.
package hide

import "os/exec"

func Cmd(c *exec.Cmd) *exec.Cmd { return c }
