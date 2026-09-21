//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/MOX-Studio/mox-access-app/internal/app"
	"github.com/MOX-Studio/mox-access-app/internal/codex"
)

func platformOps() app.CodexOps { return codex.Windows{} }

// appPaths: %LOCALAPPDATA%\MOX Access holds the bundle, the state and the log — the folder the installer of 2026-09-18 used.
func appPaths() (dir, logPath string) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Local")
	}
	dir = filepath.Join(base, "MOX Access")
	return dir, filepath.Join(dir, "mox-access.log")
}

func powershellAsync(script string) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

// notify shows a message box: Windows has no notification the application could post without a package identity, and
// a box that waits for OK is also the surest way the note about logging out is read.
func notify(title, text string) {
	powershellAsync("Add-Type -AssemblyName PresentationFramework; [System.Windows.MessageBox]::Show(" + codex.PSQuote(text) + ", " + codex.PSQuote(title) + ") | Out-Null")
}

// showCode puts the one-time GitHub code in front of the employee; gh has already copied it and opened the browser.
func showCode(code, url string) {
	notify("MOX Access", "Код для входа в GitHub: "+code+"\n\nОн уже скопирован. В браузере открылась страница "+url+": вставьте код и войдите своим аккаунтом GitHub.")
}

// installAutostart registers the binary in the user's Run key so the tunnel is back after the next logon.
func installAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
		"Set-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' -Name 'MOX Access' -Value "+codex.PSQuote(`"`+exe+`" --no-autostart`))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
