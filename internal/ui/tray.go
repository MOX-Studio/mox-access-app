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

// The mark in the menu bar tells the state without words: the ring alone — personal Codex; a check inside — corporate
// Codex on and the tunnel up; an exclamation — corporate on but the tunnel down. Template PNGs for macOS, colored for the rest.
var (
	//go:embed icon_template.png
	iconOff []byte
	//go:embed icon.png
	iconOffColor []byte
	//go:embed icon_template-on.png
	iconOn []byte
	//go:embed icon-on.png
	iconOnColor []byte
	//go:embed icon_template-attention.png
	iconAttention []byte
	//go:embed icon-attention.png
	iconAttentionColor []byte
)

// Tray runs the menu until Quit; it must be called from the main goroutine (systray.Run).
type Tray struct {
	Web    *Web
	Log    func(string)
	Notify func(title, text string)
	OnQuit func()
	items  struct{ toggle, tunnel, github, migrate, harness, diag, imp, quit *systray.MenuItem }
	icon   *byte // first byte of the icon currently shown, so refresh sets the image only when the state changes
}

func (t *Tray) Run() { systray.Run(t.ready, t.exit) }

func (t *Tray) ready() {
	systray.SetTemplateIcon(iconOff, iconOffColor)
	systray.SetTooltip("MOX Access")
	t.items.toggle = systray.AddMenuItem("Корпоративный Codex: …", "Включить или выключить вход через MOX")
	t.items.tunnel = systray.AddMenuItem("Туннель: —", "")
	t.items.tunnel.Disable()
	systray.AddSeparator()
	t.items.github = systray.AddMenuItem("Войти в GitHub", "Свой аккаунт GitHub: проекты и набор команды")
	t.items.migrate = systray.AddMenuItem("Перенести с сервера…", "Треды и проекты с vps6")
	t.items.harness = systray.AddMenuItem("Обновить набор MOX", "Правила, скиллы, MCP команды")
	t.items.diag = systray.AddMenuItem("Диагностика…", "Открыть страницу состояния")
	systray.AddSeparator()
	t.items.imp = systray.AddMenuItem("Импортировать .moxaccess…", "Файл доступа от студии")
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
	icon, iconColor := iconOff, iconOffColor
	if on {
		t.items.toggle.SetTitle("Корпоративный Codex: ВКЛ — выключить")
		if s.Tunnel.Connected {
			t.items.tunnel.SetTitle("Туннель: подключён · " + s.KeyText)
			icon, iconColor = iconOn, iconOnColor
		} else {
			t.items.tunnel.SetTitle("Туннель: нет соединения")
			icon, iconColor = iconAttention, iconAttentionColor
		}
	} else {
		t.items.toggle.SetTitle("Корпоративный Codex: ВЫКЛ — включить")
		t.items.tunnel.SetTitle("Туннель: —")
	}
	if t.icon != &icon[0] {
		systray.SetTemplateIcon(icon, iconColor)
		t.icon = &icon[0]
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
		case <-t.items.github.ClickedCh:
			go t.run("Вход в GitHub", func() error {
				user, err := t.Web.App.GitHubLogin(context.Background())
				if err == nil && user != "" {
					t.Log("GitHub: " + user)
				}
				return err
			})
		case <-t.items.migrate.ClickedCh:
			go t.run("Перенос с сервера", func() error { _, err := t.Web.App.Migrate(context.Background()); return err })
		case <-t.items.harness.ClickedCh:
			go t.run("Обновление набора MOX", func() error { _, err := t.Web.App.Harness(context.Background(), true); return err })
		case <-t.items.diag.ClickedCh:
			exec.Command("open", t.Web.URL()).Start()
		case <-t.items.imp.ClickedCh:
			go func() {
				out, err := exec.Command("osascript", "-e", `POSIX path of (choose file with prompt "Файл .moxaccess от студии")`).Output()
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
