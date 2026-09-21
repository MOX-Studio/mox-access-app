//go:build darwin

package ui

import (
	"os/exec"
	"strings"
)

// chooseFile opens the system file dialog for the .moxaccess file; an empty path means the employee cancelled.
func chooseFile() (string, error) {
	out, err := exec.Command("osascript", "-e", `POSIX path of (choose file with prompt "Файл .moxaccess от студии")`).Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func openURL(url string) { _ = exec.Command("open", url).Start() }
