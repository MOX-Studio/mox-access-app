// MOX Access — the tray application: corporate Codex on/off, the tunnel to the gateway, migration from the server,
// the team harness. Runs on macOS in this version; Windows follows spike S3.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MOX-Studio/mox-access-app/internal/app"
	"github.com/MOX-Studio/mox-access-app/internal/codex"
	"github.com/MOX-Studio/mox-access-app/internal/ui"
)

// Version is set by the build (-ldflags "-X main.Version=…").
var Version = "dev"

func main() {
	importPath := flag.String("import", "", "импортировать файл .moxaccess и выйти")
	noAutostart := flag.Bool("no-autostart", false, "не регистрировать запуск при входе")
	flag.Parse()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "MOX Access")
	logPath := filepath.Join(home, "Library", "Logs", "mox-access.log")
	os.MkdirAll(filepath.Dir(logPath), 0o755)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	logger := log.New(lf, "", log.LstdFlags)
	logf := func(s string) { logger.Println(s) }
	a, err := app.New(dir, codex.Darwin{}, logf)
	if err != nil {
		log.Fatal(err)
	}
	if *importPath != "" {
		if err := a.Import(*importPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("импортирован доступ для", a.Status().Employee)
		return
	}
	logf("MOX Access " + Version + " запущен")
	if !*noAutostart {
		if err := installAutostart(home); err != nil {
			logf("автозапуск: " + err.Error())
		}
	}
	if err := a.Resume(context.Background()); err != nil {
		logf("восстановление туннеля: " + err.Error())
	}
	web := &ui.Web{App: a, Version: Version, LogPath: logPath}
	if _, err := web.Start(); err != nil {
		log.Fatal(err)
	}
	tray := &ui.Tray{Web: web, Log: logf, Notify: notify, OnQuit: func() { web.Stop(); logf("выход") }}
	tray.Run()
}

func notify(title, text string) {
	_ = exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title %q`, text, title)).Run()
}

// installAutostart registers the running binary as a LaunchAgent so the tunnel is back after a reboot.
func installAutostart(home string) error {
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
