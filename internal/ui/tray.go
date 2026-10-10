package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/MOX-Studio/mox-access-app/internal/selfupdate"
	"github.com/MOX-Studio/mox-access-app/internal/state"
)

// The mark in the menu bar tells the state without words: the ring alone — personal Codex; a check inside — corporate
// Codex on and the tunnel up; an exclamation — corporate on but the tunnel down. The bytes live in icons_<os>.go.

// Tray runs the menu until Quit; it must be called from the main goroutine (systray.Run).
type Tray struct {
	Web     *Web
	Log     func(string)
	Notify  func(title, text string)
	OnQuit  func()
	Updates *Updates
	items   struct{ toggle, tunnel, github, migrate, harness, update, diag, imp, quit *systray.MenuItem }
	icon    *byte // first byte of the icon currently shown, so refresh sets the image only when the state changes
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
	t.items.harness = systray.AddMenuItem("Обновить набор MOX", "Личные и командные правила, скиллы")
	t.items.update = systray.AddMenuItem("Обновить приложение", "Проверить и установить новую версию MOX Access")
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
	if v := t.Updates.Available(); v != "" {
		t.items.update.SetTitle("Обновить приложение до " + v)
		// The dot stays on the ring until the update is installed: a notification passes, the menu bar does not.
		icon, iconColor = withUpdate(icon)
	} else {
		t.items.update.SetTitle("Обновить приложение")
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
	if hint := t.Web.App.Status().Hint; hint != "" && strings.HasPrefix(label, "Выключение") || hint != "" && strings.HasPrefix(label, "Включение") {
		t.Notify("MOX Access", label+" — готово. "+hint)
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
			t.Notify("MOX Access", "Вход в GitHub: проверяю git и gh, затем откроется браузер и окно с кодом")
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
		case <-t.items.update.ClickedCh:
			go func() {
				msg, err := t.Updates.Install(context.Background())
				if err != nil {
					t.Log("✗ Обновление приложения: " + err.Error())
					if !errors.Is(err, selfupdate.ErrNotStarted) {
						t.Notify("MOX Access", "Обновление приложения: "+err.Error())
					}
					return
				}
				if msg != "" && t.Updates.Available() == "" {
					t.Notify("MOX Access", "Обновление приложения: "+msg)
				}
			}()
		case <-t.items.diag.ClickedCh:
			openURL(t.Web.URL())
		case <-t.items.imp.ClickedCh:
			go func() {
				path, err := chooseFile()
				if err != nil || path == "" {
					return
				}
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

// withUpdate is the variant of a ring icon with the update dot.
func withUpdate(icon []byte) ([]byte, []byte) {
	switch &icon[0] {
	case &iconOn[0]:
		return iconOnUpdate, iconOnUpdateColor
	case &iconAttention[0]:
		return iconAttentionUpdate, iconAttentionUpdateColor
	default:
		return iconOffUpdate, iconOffUpdateColor
	}
}
