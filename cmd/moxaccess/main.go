// MOX Access — the tray application: corporate Codex on/off, the tunnel to the gateway, migration from the server,
// the team harness. The platform pieces (paths, dialogs, autostart, the Codex operations) live in main_<os>.go.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"fyne.io/systray"

	"github.com/MOX-Studio/mox-access-app/internal/app"
	"github.com/MOX-Studio/mox-access-app/internal/instance"
	"github.com/MOX-Studio/mox-access-app/internal/selfupdate"
	"github.com/MOX-Studio/mox-access-app/internal/tunnel"
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
	// One copy per user. After an update the previous one may still be releasing the lock for a moment.
	err = instance.Acquire(dir)
	for tries := 0; errors.Is(err, instance.ErrRunning) && *waitPid > 0 && tries < 25; tries++ {
		time.Sleep(200 * time.Millisecond)
		err = instance.Acquire(dir)
	}
	if errors.Is(err, instance.ErrRunning) {
		logf("уже запущена другая копия — выхожу")
		alert("MOX Access", "MOX Access уже запущен: кольцо "+trayPlace+".\n\nЧтобы перезапустить, нажми в кольце «Выйти» и запусти MOX Access снова.")
		return
	} else if err != nil {
		logf("блокировка второй копии: " + err.Error())
	}
	if !*noAutostart {
		if err := installAutostart(); err != nil {
			logf("автозапуск: " + err.Error())
		}
	}
	// The tunnel comes back in the background: at login the network may not be up yet, so a failed attempt is retried.
	// Started by autostart (login), the application then restarts ChatGPT if it opened before the tunnel.
	atLogin := *noAutostart && *waitPid == 0
	go func() {
		for delay := 5 * time.Second; ; {
			err := a.Resume(context.Background())
			if err == nil {
				break
			}
			logf("восстановление туннеля: " + err.Error())
			if errors.Is(err, tunnel.ErrPortBusy) {
				// A copy from before the lock existed (≤0.5.3) still holds the tunnel port.
				alert("MOX Access", "Туннель не поднялся: порт занят. Скорее всего, рядом работает старая копия MOX Access "+trayPlace+".\n\nВыйди из всех колец MOX Access («Выйти») и запусти MOX Access один раз.")
				return
			}
			time.Sleep(delay)
			if delay < time.Minute {
				delay *= 2
			}
		}
		// The MOX login is checked every minute: a sign-in or sign-out on Codex's own screen replaces it, and then only
		// putting it back helps. Putting it back restarts ChatGPT, so after login the race restart is not repeated.
		for first := true; ; first = false {
			restored, err := a.EnsureLogin()
			if err != nil {
				logf("✗ " + err.Error())
			}
			if first && atLogin && !restored {
				if err := a.RestartCodexAfterLogin(); err != nil {
					logf("✗ перезапуск ChatGPT после входа: " + err.Error())
				}
			}
			time.Sleep(time.Minute)
		}
	}()
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
