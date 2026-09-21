//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MOX-Studio/mox-access-app/internal/app"
	"github.com/MOX-Studio/mox-access-app/internal/codex"
)

func platformOps() app.CodexOps { return codex.Darwin{} }

// appPaths: the application directory and its log, in the places macOS keeps them.
func appPaths() (dir, logPath string) {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "MOX Access"), filepath.Join(home, "Library", "Logs", "mox-access.log")
}

// showCode puts the one-time GitHub code in front of the employee: a dialog that stays until they read it, and a
// notification. gh has already copied the code to the clipboard and opened the browser.
func showCode(code, url string) {
	text := "Код для входа в GitHub: " + code + "\n\nОн уже скопирован. В браузере открылась страница " + url + " — вставь код и войди своим аккаунтом GitHub."
	go exec.Command("osascript", "-e", fmt.Sprintf(`display dialog %q with title "MOX Access" buttons {"OK"} default button 1`, text)).Run()
	notify("MOX Access", "Код для GitHub: "+code+" (скопирован в буфер)")
}

func notify(title, text string) {
	_ = exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title %q`, text, title)).Run()
}

// installAutostart registers the running binary as a LaunchAgent so the tunnel is back after a reboot.
func installAutostart() error {
	home, _ := os.UserHomeDir()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "ru.mox.access.app.plist")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>ru.mox.access.app</string>
  <key>ProgramArguments</key><array><string>` + exe + `</string><string>--no-autostart</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><false/>
</dict></plist>
`
	if current, err := os.ReadFile(plist); err == nil && string(current) == body {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	return os.WriteFile(plist, []byte(body), 0o644)
}
