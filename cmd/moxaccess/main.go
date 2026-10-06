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
	"strconv"
	"time"

	"fyne.io/systray"

	"github.com/MOX-Studio/mox-access-app/internal/app"
	"github.com/MOX-Studio/mox-access-app/internal/selfupdate"
	"github.com/MOX-Studio/mox-access-app/internal/ui"
)

// Version is set by the build (-ldflags "-X main.Version=…").
var Version = "dev"

func main() {
	importPath := flag.String("import", "", "импортировать файл .moxaccess и выйти")
	noAutostart := flag.Bool("no-autostart", false, "не регистрировать запуск при входе")
	waitPid := flag.Int("wait-pid", 0, "после обновления: дождаться выхода прежней версии")
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
	exe, _ := os.Executable()
	target := selfupdate.Target(exe)
	if *waitPid > 0 {
		// Started by the previous version's update: say so first (it waits for this), then let it free the tunnel port.
		if err := selfupdate.MarkStarted(dir, Version); err != nil {
			logf("обновление: метка запуска: " + err.Error())
		}
		selfupdate.WaitExit(*waitPid, 30*time.Second)
		selfupdate.Cleanup(target)
		logf("обновление до " + Version + " завершено")
		notify("MOX Access", "MOX Access обновлён до "+Version+".")
	}
	if !*noAutostart {
		if err := installAutostart(); err != nil {
			logf("автозапуск: " + err.Error())
		}
	}
	if err := a.Resume(context.Background()); err != nil {
		logf("восстановление туннеля: " + err.Error())
	}
	// The new copy keeps this run's options; --wait-pid tells it to let this process go first.
	relaunch := []string{"--wait-pid", strconv.Itoa(os.Getpid())}
	if *noAutostart {
		relaunch = append(relaunch, "--no-autostart")
	}
	updates := &ui.Updates{
		U:      &selfupdate.Updater{Current: Version, Dir: dir, Target: target, Log: logf, Start: selfupdate.Starter(relaunch)},
		Notify: notify, Alert: alert, Quit: systray.Quit,
	}
	go updates.Watch(context.Background(), time.Minute, 6*time.Hour)
	web := &ui.Web{App: a, Version: Version, LogPath: logPath, Updates: updates}
	if _, err := web.Start(); err != nil {
		log.Fatal(err)
	}
	tray := &ui.Tray{Web: web, Log: logf, Notify: notify, Updates: updates, OnQuit: func() { web.Stop(); logf("выход") }}
	tray.Run()
}
