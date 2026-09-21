//go:build windows

package github

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	ghName          = "gh.exe"
	assetSuffix     = "_windows_"
	gitAfterRequest = "нужен git: дождитесь конца установки Git for Windows и повторите действие"
)

var systemGh = []string{`C:\Program Files\GitHub CLI\gh.exe`}

// gitDirs are the places Git for Windows lands in; a fresh install is not on the PATH of a process that was already
// running, so a known location is put there by hand.
func gitDirs() []string {
	return []string{`C:\Program Files\Git\cmd`, filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "cmd")}
}

func gitReady() bool {
	if _, err := exec.LookPath("git.exe"); err == nil {
		return true
	}
	for _, d := range gitDirs() {
		if _, err := os.Stat(filepath.Join(d, "git.exe")); err == nil {
			os.Setenv("PATH", d+";"+os.Getenv("PATH"))
			return true
		}
	}
	return false
}

// requestGit installs Git for Windows through winget (present on Windows 10 1809+ and 11); without winget the employee
// installs it from git-scm.com.
func requestGit() error {
	if _, err := exec.LookPath("winget.exe"); err != nil {
		return errors.New("winget не найден: установите Git for Windows с git-scm.com и повторите")
	}
	cmd := exec.Command("winget", "install", "--id", "Git.Git", "-e", "--source", "winget", "--silent", "--accept-source-agreements", "--accept-package-agreements")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("winget: " + strings.TrimSpace(string(out)))
	}
	return nil
}
