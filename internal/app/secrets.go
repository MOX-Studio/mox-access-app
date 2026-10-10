package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/codex"
	"github.com/MOX-Studio/mox-access-app/internal/state"
)

// SyncSecrets brings the personal service keys the gateway grants to this employee (GET /mox/secrets) into Codex's
// config.toml as marked [mcp_servers.<name>] tables, and takes away the ones withdrawn. A revoked MOX key takes away all
// of them. Codex is not restarted: an answer in progress would be cut off, so the window asks the employee to restart it
// when convenient (state.SecretsPending). Values never reach the log, the state file or a backup. Runs only in corporate
// mode with the tunnel up; changed tells whether config.toml was rewritten.
func (a *App) SyncSecrets(ctx context.Context) (changed bool, err error) {
	b := a.Bundle()
	if b == nil || a.Status().Mode != state.ModeCorporate || !a.Status().Tunnel.Connected {
		return false, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.Gateway.Origin+"/mox/secrets", nil)
	req.Header.Set("Authorization", "Bearer "+b.AccessToken())
	client := a.httpClient()
	client.Timeout = 15 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("ключи сервисов: шлюз не отвечает: %w", err)
	}
	defer resp.Body.Close()
	var granted []codex.Secret
	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Secrets []codex.Secret `json:"secrets"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return false, errors.New("ключи сервисов: ответ шлюза не читается")
		}
		granted = body.Secrets
	case http.StatusUnauthorized:
		// The key was revoked in the panel: nothing granted to it may stay in Codex.
		a.mu.Lock()
		a.st.LastCheck, a.st.LastCheckText = time.Now(), "ключ отозван"
		a.mu.Unlock()
	case http.StatusNotFound:
		return false, nil // a gateway from before /mox/secrets
	default:
		return false, fmt.Errorf("ключи сервисов: неожиданный ответ шлюза %d", resp.StatusCode)
	}
	return a.applySecrets(granted)
}

func (a *App) applySecrets(granted []codex.Secret) (bool, error) {
	if a.take() != nil {
		return false, nil // ВКЛ/ВЫКЛ or another action writes config.toml now; the next minute tries again
	}
	defer a.release()
	path := filepath.Join(a.ops.Home(), "config.toml")
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	next, skipped := codex.ApplySecrets(string(current), granted)
	changed := next != string(current)
	if changed {
		// No backup: the previous file holds the previous values, and the marked tables are all this step changes.
		if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
			return false, fmt.Errorf("ключи сервисов: config.toml: %w", err)
		}
	}
	infos := make([]state.Secret, 0, len(granted))
	names := make([]string, 0, len(granted))
	for _, s := range granted {
		skip := contains(skipped, s.Name)
		infos = append(infos, state.Secret{Name: s.Name, Version: s.Version, Skipped: skip})
		names = append(names, s.Name+" v"+strconv.Itoa(s.Version))
		if skip {
			a.log("⚠ ключ «" + s.Name + "» не записан: в Codex уже есть свой сервер с таким именем или ключ не прошёл проверку")
		}
	}
	a.mu.Lock()
	a.st.Secrets = infos
	if changed {
		a.st.SecretsPending = true
		if len(names) == 0 {
			a.log("→ ключи сервисов: все сняты из Codex")
		} else {
			a.log("→ ключи сервисов: " + strings.Join(names, ", ") + " — записаны в Codex")
		}
	}
	a.mu.Unlock()
	return changed, state.Save(a.Dir, a.st)
}

// RestartCodex is the window's «Перезапустить Codex» after new keys: done only on the employee's word.
func (a *App) RestartCodex() error {
	if err := a.restartCodex("ключи сервисов", "по просьбе сотрудника"); err != nil {
		return err
	}
	a.mu.Lock()
	a.st.SecretsPending = false
	a.mu.Unlock()
	return state.Save(a.Dir, a.st)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
