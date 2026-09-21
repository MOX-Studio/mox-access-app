//go:build !darwin

package github

import (
	"errors"
	"os/exec"
)

func gitReady() bool { return exec.Command("git", "--version").Run() == nil }

func requestGit() error { return errors.New("установите git") }
