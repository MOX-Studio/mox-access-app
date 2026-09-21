//go:build darwin

package github

import "os/exec"

// gitReady: the Command Line Tools are installed when xcode-select knows their path; without them /usr/bin/git is a
// stub that only offers the installation.
func gitReady() bool { return exec.Command("xcode-select", "-p").Run() == nil }

// requestGit opens the system dialog that installs the Command Line Tools.
func requestGit() error { return exec.Command("xcode-select", "--install").Run() }
