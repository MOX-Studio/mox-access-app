package ui

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/selfupdate"
)

// Updates is the application's own update, shared by the ring and the status page: a check at start and every few
// hours, and an install on request — never by itself, so a running Codex turn is not cut by the restart.
type Updates struct {
	U      *selfupdate.Updater
	Notify func(title, text string) // a passing note
	Alert  func(title, text string) // a note that waits for OK: the manual-start instructions must be read
	Quit   func()                   // ends this process once the new copy runs
	mu     sync.Mutex
	latest *selfupdate.Release
	told   string
	busy   bool
}

// Available is the newer version found by the last check, "" when there is none.
func (u *Updates) Available() string {
	if u == nil {
		return ""
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.latest == nil {
		return ""
	}
	return u.latest.Version
}

// Check asks GitHub; the first time a version shows up the employee is told once.
func (u *Updates) Check(ctx context.Context) (*selfupdate.Release, error) {
	rel, err := u.U.Check(ctx)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.latest = rel
	tell := rel != nil && u.told != rel.Version
	if tell {
		u.told = rel.Version
	}
	u.mu.Unlock()
	if tell {
		u.Notify("MOX Access", "Вышла новая версия MOX Access "+rel.Version+". Кольцо → «Обновить приложение».")
	}
	return rel, nil
}

// Watch checks shortly after start and then every interval until ctx ends; a failed check is logged and retried later.
func (u *Updates) Watch(ctx context.Context, first, every time.Duration) {
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if _, err := u.Check(ctx); err != nil {
			u.U.Log("✗ " + err.Error())
		}
		wait = every
	}
}

// Install checks again, installs the newest version and hands over to it. It returns a message when there was nothing
// to do; on success the process quits and the call does not come back to the caller in any useful way.
func (u *Updates) Install(ctx context.Context) (string, error) {
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return "", errors.New("обновление уже идёт")
	}
	u.busy = true
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }()
	rel, err := u.Check(ctx)
	if err != nil {
		return "", err
	}
	if rel == nil {
		return "у тебя последняя версия, " + u.U.Current, nil
	}
	u.Notify("MOX Access", "Обновляю MOX Access до "+rel.Version+". Приложение перезапустится, рабочий режим и туннель вернутся сами.")
	err = u.U.Apply(context.WithoutCancel(ctx), rel)
	switch {
	case err == nil:
		u.U.Log("обновление: " + rel.Version + " запущена, выхожу")
		go u.Quit()
		return "обновлено до " + rel.Version + ", приложение перезапускается", nil
	case errors.Is(err, selfupdate.ErrNotStarted):
		u.U.Log("✗ обновление: " + err.Error())
		selfupdate.OpenManualStart()
		u.Alert("MOX Access", selfupdate.ManualStart)
		go u.Quit()
		return "", err
	default:
		return "", err
	}
}
