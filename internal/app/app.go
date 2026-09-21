// Package app is the toggle: corporate Codex on and off as an ordered, reversible sequence of steps, plus the state
// and the bundle behind it. OS-level effects go through CodexOps so the sequence is testable without a Mac.
package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/bundle"
	"github.com/MOX-Studio/mox-access-app/internal/codex"
	"github.com/MOX-Studio/mox-access-app/internal/github"
	"github.com/MOX-Studio/mox-access-app/internal/state"
	"github.com/MOX-Studio/mox-access-app/internal/tunnel"
)

// CodexOps are the steps that touch the operating system; codex.Darwin implements them on a Mac.
type CodexOps interface {
	Home() string
	TrustCert(pemPath string) error
	SetEnv(vars map[string]string) error
	UnsetEnv(keys []string) error
	QuitCodex() error
	LaunchCodex() error
}

type Snapshot struct {
	Mode     string
	Employee string
	Tunnel   tunnel.Status
	KeyText  string // last E2E check: "ключ действует" / "ключ отозван" / ""
	Harness  string
	Imported time.Time
}

type App struct {
	Dir    string
	GitHub *github.Client // gh, browser login, git — main sets how the one-time code is shown
	ops    CodexOps
	log    func(string)
	mu     sync.Mutex
	b      *bundle.Bundle
	st     state.State
	tn     *tunnel.Tunnel
	busy   bool
}

func New(dir string, ops CodexOps, log func(string)) (*App, error) {
	if log == nil {
		log = func(string) {}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	a := &App{Dir: dir, ops: ops, log: log, st: state.Load(dir), GitHub: &github.Client{Dir: dir, Log: log}}
	if raw, err := os.ReadFile(filepath.Join(dir, "bundle.moxaccess")); err == nil {
		if b, err := bundle.Parse(raw); err == nil {
			a.b = b
		} else {
			log("сохранённый .moxaccess не читается: " + err.Error())
		}
	}
	return a, nil
}

func (a *App) Bundle() *bundle.Bundle { a.mu.Lock(); defer a.mu.Unlock(); return a.b }

func (a *App) Status() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Snapshot{Mode: a.st.Mode, Employee: a.st.Employee, KeyText: a.st.LastCheckText, Harness: a.st.HarnessVersion, Imported: a.st.ImportedAt}
	if a.tn != nil {
		s.Tunnel = a.tn.Status()
	}
	return s
}

// Import validates the file, keeps a 0600 copy in the application directory and remembers the employee. It never
// touches Codex: that is what Enable is for.
func (a *App) Import(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("файл не открывается: %w", err)
	}
	b, err := bundle.Parse(raw)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "bundle.moxaccess"), raw, 0o600); err != nil {
		return fmt.Errorf("сохранение файла: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.b = b
	a.st.Employee = b.Employee.Email
	a.st.ImportedAt = time.Now()
	a.log("импортирован доступ для " + b.Employee.Email)
	return state.Save(a.Dir, a.st)
}

func (a *App) pemPath() string { return filepath.Join(a.ops.Home(), "mox-access.pem") }

func (a *App) take() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return errors.New("предыдущее действие ещё выполняется")
	}
	if a.b == nil {
		return errors.New("сначала импортируйте файл .moxaccess от студии")
	}
	a.busy = true
	return nil
}

func (a *App) release() { a.mu.Lock(); a.busy = false; a.mu.Unlock() }

// Enable turns corporate Codex on. The order matters: trust first (a password dialog the employee dismisses ends the
// sequence before the login is touched), then the login, the config, the environment, the tunnel, the restart, and
// finally a real request through the tunnel proves the key is alive. A step that fails undoes the steps before it:
// the employee is left with the personal Codex they had, never with a login that points at a gateway nothing serves.
func (a *App) Enable(ctx context.Context) error {
	if err := a.take(); err != nil {
		return err
	}
	defer a.release()
	b := a.Bundle()
	home := a.ops.Home()
	var undo []func() error
	step := func(name string, fn func() error, revert func() error) error {
		a.log("→ " + name)
		if err := fn(); err != nil {
			return fmt.Errorf("шаг «%s»: %w", name, err)
		}
		if revert != nil {
			undo = append(undo, revert)
		}
		return nil
	}
	rollback := func(cause error) error {
		if len(undo) == 0 {
			return cause
		}
		a.log("↩ откат: возвращаю личный Codex")
		for i := len(undo) - 1; i >= 0; i-- {
			if err := undo[i](); err != nil {
				a.log("откат: " + err.Error())
			}
		}
		return fmt.Errorf("%w — изменения отменены, личный Codex не тронут", cause)
	}
	if b.TLS != nil {
		// Trust stays after a rollback: a trusted certificate asks no password next time and harms nothing.
		if err := step("сертификат шлюза", func() error {
			if err := os.MkdirAll(home, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(a.pemPath(), []byte(b.TLS.Certificate), 0o600); err != nil {
				return err
			}
			return a.ops.TrustCert(a.pemPath())
		}, nil); err != nil {
			return rollback(err)
		}
	}
	if err := step("вход MOX (auth.json)", func() error { return codex.InstallAuth(home, b.Auth) }, func() error { return codex.RestoreAuth(home) }); err != nil {
		return rollback(err)
	}
	if err := step("config.toml", func() error { return a.writeConfig(home, b.Config.Fragment, true) }, func() error { return a.writeConfig(home, b.Config.Fragment, false) }); err != nil {
		return rollback(err)
	}
	envVars := b.EnvVars(a.pemPath())
	if err := step("окружение Codex", func() error { return a.ops.SetEnv(envVars) }, func() error { return a.ops.UnsetEnv(sortedKeys(envVars)) }); err != nil {
		return rollback(err)
	}
	if b.Relay != nil {
		// A VPN or a flaky network can drop the very first handshake (Shadowrocket re-originates every connection and stalls
		// for a moment when it reconnects); three attempts cover a hiccup without hiding a real outage.
		if err := step("туннель к шлюзу", func() error {
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				if err = a.startTunnel(ctx, b); err == nil {
					return nil
				}
				if attempt < 3 {
					a.log(fmt.Sprintf("туннель: попытка %d не удалась (%v), повторяю", attempt, err))
					select {
					case <-time.After(4 * time.Second):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			return err
		}, func() error { a.stopTunnel(); return nil }); err != nil {
			return rollback(err)
		}
	}
	if err := step("перезапуск Codex", func() error {
		if err := a.ops.QuitCodex(); err != nil {
			return err
		}
		undo = append(undo, a.ops.LaunchCodex) // Codex is closed now: whatever happens next, it comes back
		return a.ops.LaunchCodex()
	}, nil); err != nil {
		return rollback(err)
	}
	a.mu.Lock()
	a.st.Mode = state.ModeCorporate
	a.mu.Unlock()
	if err := state.Save(a.Dir, a.st); err != nil {
		return err
	}
	if err := step("проверка ключа", func() error { return a.check(ctx) }, nil); err != nil {
		a.log(err.Error())
	}
	return nil
}

// Disable turns corporate Codex off in reverse order and brings the personal login back.
func (a *App) Disable(ctx context.Context) error {
	if err := a.take(); err != nil {
		return err
	}
	defer a.release()
	b := a.Bundle()
	home := a.ops.Home()
	a.log("→ туннель")
	a.stopTunnel()
	a.log("→ окружение Codex")
	if err := a.ops.UnsetEnv(sortedKeys(b.EnvVars(a.pemPath()))); err != nil {
		return fmt.Errorf("шаг «окружение Codex»: %w", err)
	}
	a.log("→ config.toml")
	if err := a.writeConfig(home, b.Config.Fragment, false); err != nil {
		return fmt.Errorf("шаг «config.toml»: %w", err)
	}
	a.log("→ личный вход (auth.json)")
	if err := codex.RestoreAuth(home); err != nil {
		return fmt.Errorf("шаг «личный вход»: %w", err)
	}
	a.log("→ перезапуск Codex")
	if err := a.ops.QuitCodex(); err != nil {
		return fmt.Errorf("шаг «перезапуск Codex»: %w", err)
	}
	if err := a.ops.LaunchCodex(); err != nil {
		return fmt.Errorf("шаг «перезапуск Codex»: %w", err)
	}
	a.mu.Lock()
	a.st.Mode = state.ModePersonal
	a.st.LastCheckText = ""
	a.mu.Unlock()
	return state.Save(a.Dir, a.st)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (a *App) writeConfig(home, fragment string, install bool) error {
	path := filepath.Join(home, "config.toml")
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next := codex.ApplyConfig(string(current), fragment, install)
	if next == string(current) {
		return nil
	}
	if err == nil {
		if err := os.WriteFile(path+".bak-mox-"+time.Now().Format("20060102-150405"), current, 0o600); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(next), 0o600)
}

func (a *App) startTunnel(ctx context.Context, b *bundle.Bundle) error {
	a.mu.Lock()
	if a.tn != nil {
		a.mu.Unlock()
		return nil
	}
	tn := tunnel.New(tunnel.Config{User: b.Relay.User, Host: b.Relay.Host, Port: b.Relay.Port, HostKey: b.Relay.HostKey, PrivateKey: b.Relay.PrivateKey, LocalPort: b.Gateway.LocalPort, Remote: "127.0.0.1:" + strconv.Itoa(b.Gateway.LocalPort), Log: a.log})
	a.tn = tn
	a.mu.Unlock()
	if err := tn.Start(ctx); err != nil {
		a.mu.Lock()
		a.tn = nil
		a.mu.Unlock()
		return err
	}
	return nil
}

func (a *App) stopTunnel() {
	a.mu.Lock()
	tn := a.tn
	a.tn = nil
	a.mu.Unlock()
	if tn != nil {
		tn.Stop()
	}
}

// Resume re-establishes the tunnel after the application itself restarted while corporate mode was on.
func (a *App) Resume(ctx context.Context) error {
	b := a.Bundle()
	if b == nil || a.Status().Mode != state.ModeCorporate || b.Relay == nil {
		return nil
	}
	return a.startTunnel(ctx, b)
}

// check is the one proof that counts: a request through the tunnel with the key. 200/404 — tunnel and key alive;
// 401 — the key was revoked in the panel.
func (a *App) check(ctx context.Context) error {
	b := a.Bundle()
	client := &http.Client{Timeout: 10 * time.Second}
	if b.TLS != nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM([]byte(b.TLS.Certificate))
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, Proxy: nil}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, b.Gateway.Origin+"/mox/export", nil)
	req.Header.Set("Authorization", "Bearer "+b.AccessToken())
	resp, err := client.Do(req)
	text, retErr := "", error(nil)
	switch {
	case err != nil:
		text, retErr = "шлюз недоступен", fmt.Errorf("шлюз не отвечает через туннель: %w", err)
	case resp.StatusCode == 401:
		text, retErr = "ключ отозван", errors.New("ключ MOX отозван — попросите у студии новый файл .moxaccess")
	case resp.StatusCode == 200 || resp.StatusCode == 404:
		text = "ключ действует"
	default:
		text, retErr = "ответ "+strconv.Itoa(resp.StatusCode), fmt.Errorf("неожиданный ответ шлюза: %d", resp.StatusCode)
	}
	if resp != nil {
		resp.Body.Close()
	}
	a.mu.Lock()
	a.st.LastCheck, a.st.LastCheckText = time.Now(), text
	a.mu.Unlock()
	_ = state.Save(a.Dir, a.st)
	return retErr
}

// Check runs the E2E probe on demand (menu «Диагностика»).
func (a *App) Check(ctx context.Context) error {
	if a.Bundle() == nil {
		return errors.New("файл .moxaccess не импортирован")
	}
	return a.check(ctx)
}
