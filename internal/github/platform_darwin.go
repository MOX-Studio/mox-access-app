//go:build darwin

package github

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	ghName          = "gh"
	assetSuffix     = "_macOS_"
	gitAfterRequest = "нужен git: установите Command Line Tools в открывшемся окне macOS и повторите действие"
)

var systemGh = []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"}

// gitReady: the Command Line Tools are installed when xcode-select knows their path; without them /usr/bin/git is a
// stub that only offers the installation.
func gitReady() bool { return exec.Command("xcode-select", "-p").Run() == nil }

// requestGit opens the system dialog that installs the Command Line Tools.
func requestGit() error { return exec.Command("xcode-select", "--install").Run() }

const pathMarker = "# MOX Access: gh для Codex и скиллов команды"

// exposeOnPath makes the private gh visible to login shells (Codex runs commands through the user's login shell):
// one marked line appended to ~/.zprofile and ~/.bash_profile, once.
func exposeOnPath(binDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	line := pathMarker + "\nexport PATH=\"" + strings.ReplaceAll(binDir, home, "$HOME") + ":$PATH\"\n"
	for _, name := range []string{".zprofile", ".bash_profile"} {
		path := filepath.Join(home, name)
		current, _ := os.ReadFile(path)
		if strings.Contains(string(current), pathMarker) {
			continue
		}
		body := string(current)
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if err := os.WriteFile(path, []byte(body+line), 0o644); err != nil {
			return err
		}
	}
	return nil
}
