// MOX Access — the tray application: corporate Codex on/off, the tunnel to the gateway, migration from the server,
// the team harness. The platform pieces (paths, dialogs, autostart, the Codex operations) live in main_<os>.go.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/MOX-Studio/mox-access-app/internal/app"
	"github.com/MOX-Studio/mox-access-app/internal/ui"
)

// Version is set by the build (-ldflags "-X main.Version=…").
var Version = "dev"

func main() {
	importPath := flag.String("import", "", "импортировать файл .moxaccess и выйти")
	noAutostart := flag.Bool("no-autostart", false, "не регистрировать запуск при входе")
	flag.Parse()
	dir, logPath := appPaths()
	os.MkdirAll(filepath.Dir(logPath), 0o755)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	logger := log.New(lf, "", log.LstdFlags)
	logf := func(s string) { logger.Println(s) }
	a, err := app.New(dir, platformOps(), logf)
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
	a.GitHub.Show = showCode
	logf("MOX Access " + Version + " запущен")
	if !*noAutostart {
		if err := installAutostart(); err != nil {
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
