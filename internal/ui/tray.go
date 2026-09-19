package ui

import (
	"context"
	_ "embed"
	"os/exec"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/MOX-Studio/mox-access-app/internal/state"
)

//go:embed icon_template.png
var iconTemplate []byte

//go:embed icon.png
var iconRegular []byte

// Tray runs the menu until Quit; it must be called from the main goroutine (systray.Run).
type Tray struct {
	Web    *Web
	Log    func(string)
	Notify func(title, text string)
	OnQuit func()
	items  struct{ toggle, tunnel, migrate, harness, diag, imp, quit *systray.MenuItem }
}

func (t *Tray) Run() { systray.Run(t.ready, t.exit) }

func (t *Tray) ready() {
	systray.SetTemplateIcon(iconTemplate, iconRegular)
	systray.SetTooltip("MOX Access")
	t.items.toggle = systray.AddMenuItem("Корпоративный Codex: …", "Включить или выключить вход через MOX")
	t.items.tunnel = systray.AddMenuItem("Туннель: —", "")
	t.items.tunnel.Disable()
	systray.AddSeparator()
	t.items.migrate = systray.AddMenuItem("Перенести с сервера…", "Треды и проекты с vps6")
	t.items.harness = systray.AddMenuItem("Обновить набор MOX", "Правила, скиллы, MCP команды")
	t.items.diag = systray.AddMenuItem("Диагностика…", "Открыть страницу состояния")
	systray.AddSeparator()
	t.items.imp = systray.AddMenuItem("Импортировать .moxaccess…", "Файл доступа от Дениса")
	t.items.quit = systray.AddMenuItem("Выйти", "")
	go t.loop()
	go func() {
		for {
			t.refresh()
			time.Sleep(3 * time.Second)
		}
	}()
}

func (t *Tray) refresh() {
	s := t.Web.App.Status()
	on := s.Mode == state.ModeCorporate
	if on {
		t.items.toggle.SetTitle("Корпоративный Codex: ВКЛ — выключить")
		if s.Tunnel.Connected {
			t.items.tunnel.SetTitle("Туннель: подключён · " + s.KeyText)
			systray.SetTitle("MOX ●")
		} else {
			t.items.tunnel.SetTitle("Туннель: нет соединения")
			systray.SetTitle("MOX !")
		}
	} else {
		t.items.toggle.SetTitle("Корпоративный Codex: ВЫКЛ — включить")
		t.items.tunnel.SetTitle("Туннель: —")
		systray.SetTitle("")
	}
	if s.Employee == "" {
		t.items.toggle.Disable()
		t.items.migrate.Disable()
		t.items.harness.Disable()
	} else {
		t.items.toggle.Enable()
		t.items.migrate.Enable()
		t.items.harness.Enable()
	}
}

func (t *Tray) run(label string, fn func() error) {
	t.items.toggle.Disable()
	defer func() { t.items.toggle.Enable(); t.refresh() }()
	if err := fn(); err != nil {
		t.Log("✗ " + label + ": " + err.Error())
		t.Notify("MOX Access", label+": "+err.Error())
		return
	}
	t.Notify("MOX Access", label+" — готово")
}

func (t *Tray) loop() {
	for {
		select {
		case <-t.items.toggle.ClickedCh:
			if t.Web.App.Status().Mode == state.ModeCorporate {
				go t.run("Выключение корпоративного Codex", func() error { return t.Web.App.Disable(context.Background()) })
			} else {
				t.Notify("MOX Access", "Включаю корпоративный Codex. macOS может попросить пароль — это доверие сертификату шлюза.")
				go t.run("Включение корпоративного Codex", func() error { return t.Web.App.Enable(context.Background()) })
			}
		case <-t.items.migrate.ClickedCh:
			go t.run("Перенос с сервера", func() error { _, err := t.Web.App.Migrate(context.Background()); return err })
		case <-t.items.harness.ClickedCh:
			go t.run("Обновление набора MOX", func() error { _, err := t.Web.App.Harness(context.Background(), true); return err })
		case <-t.items.diag.ClickedCh:
			exec.Command("open", t.Web.URL()).Start()
		case <-t.items.imp.ClickedCh:
			go func() {
				out, err := exec.Command("osascript", "-e", `POSIX path of (choose file with prompt "Файл .moxaccess от Дениса")`).Output()
				if err != nil {
					return
				}
				path := strings.TrimSpace(string(out))
				if err := t.Web.App.Import(path); err != nil {
					t.Notify("MOX Access", "Файл не принят: "+err.Error())
					return
				}
				t.Notify("MOX Access", "Доступ импортирован: "+t.Web.App.Status().Employee+". Теперь включите корпоративный Codex.")
				t.refresh()
			}()
		case <-t.items.quit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (t *Tray) exit() {
	if t.OnQuit != nil {
		t.OnQuit()
	}
}
