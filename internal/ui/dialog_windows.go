//go:build windows

package ui

import (
	"os/exec"
	"strings"
	"syscall"
)

// chooseFile opens the Windows file dialog for the .moxaccess file; an empty path means the employee cancelled.
func chooseFile() (string, error) {
	script := "Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.OpenFileDialog; " +
		"$d.Title = 'Файл .moxaccess от студии'; $d.Filter = 'MOX Access (*.moxaccess)|*.moxaccess|Все файлы (*.*)|*.*'; " +
		"if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.FileName }"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func openURL(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}
