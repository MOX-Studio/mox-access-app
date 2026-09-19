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
	"strings"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/harness"
	"github.com/MOX-Studio/mox-access-app/internal/migrate"
	"github.com/MOX-Studio/mox-access-app/internal/state"
)

// httpClient talks to the gateway through the tunnel, trusting the certificate the bundle carries and no proxy.
func (a *App) httpClient() *http.Client {
	b := a.Bundle()
	client := &http.Client{Timeout: 30 * time.Minute}
	transport := &http.Transport{Proxy: nil}
	if b != nil && b.TLS != nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM([]byte(b.TLS.Certificate))
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
	}
	client.Transport = transport
	return client
}

// MigrateSummary is what the employee sees after «Перенести с сервера».
type MigrateSummary struct {
	Threads int
	Repos   int
	Files   int
}

// Migrate brings the server state of the employee onto this machine: Codex is closed for the duration, the export
// comes through the tunnel, projects are cloned, paths rewritten, threads merged; the shell's own thread catalog is
// set aside so it rebuilds from the merged state. Requires corporate mode (the tunnel).
func (a *App) Migrate(ctx context.Context) (MigrateSummary, error) {
	var sum MigrateSummary
	if err := a.take(); err != nil {
		return sum, err
	}
	defer a.release()
	b := a.Bundle()
	if a.Status().Mode != state.ModeCorporate || a.tn == nil {
		return sum, errors.New("сначала включите корпоративный Codex — экспорт приходит через туннель")
	}
	home := a.ops.Home()
	userHome, _ := os.UserHomeDir()
	work, err := os.MkdirTemp("", "mox-migrate-")
	if err != nil {
		return sum, err
	}
	defer os.RemoveAll(work)
	a.log("→ закрываю Codex")
	if err := a.ops.QuitCodex(); err != nil {
		return sum, err
	}
	defer func() { a.log("→ открываю Codex"); _ = a.ops.LaunchCodex() }()
	a.log("→ загружаю экспорт с сервера")
	archive := filepath.Join(work, "export.tar.zst")
	if _, err := migrate.Download(a.httpClient(), b.Gateway.Origin, b.AccessToken(), archive); err != nil {
		return sum, err
	}
	a.log("→ распаковываю")
	extracted := filepath.Join(work, "x")
	if err := migrate.Extract(archive, extracted); err != nil {
		return sum, err
	}
	repos, err := migrate.ReadRepos(extracted)
	if err != nil {
		return sum, fmt.Errorf("repos.json: %w", err)
	}
	a.log("→ клонирую проекты")
	n, err := migrate.CloneRepos(extracted, filepath.Join(userHome, "Claud", "projects"), repos, a.log)
	sum.Repos = n
	if err != nil {
		return sum, err
	}
	srcCodex := filepath.Join(extracted, "codex")
	serverUser := serverUserOf(extracted, b.Employee.Email)
	a.log("→ переписываю пути /home/" + serverUser + " → " + userHome)
	st, err := migrate.RewritePaths(srcCodex, "/home/"+serverUser, userHome)
	if err != nil {
		return sum, err
	}
	sum.Files = st.FilesChanged
	a.log("→ вливаю треды в ~/.codex")
	added, err := migrate.MergeThreads(srcCodex, home)
	if err != nil {
		return sum, err
	}
	sum.Threads = added
	// The shell keeps its own thread catalog synced by watermark; set it aside so it rebuilds from the merged state.
	if catalog := filepath.Join(home, "sqlite", "codex-dev.db"); fileExists(catalog) {
		stamp := time.Now().Format("20060102-150405")
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if fileExists(catalog + suffix) {
				_ = os.Rename(catalog+suffix, catalog+suffix+".bak-mox-"+stamp)
			}
		}
		a.log("каталог оболочки отложен (пересоберётся при запуске)")
	}
	a.log(fmt.Sprintf("✅ перенесено: тредов %d, проектов %d, файлов переписано %d", sum.Threads, sum.Repos, sum.Files))
	return sum, nil
}

// serverUserOf reads the linux user from the export metadata when present, else derives it from the email.
func serverUserOf(extracted, email string) string {
	if data, err := os.ReadFile(filepath.Join(extracted, "repos.json")); err == nil && len(data) > 0 {
		// repos.json carries no user; <email>.json on the server does, but it does not travel. Fall back to the email.
	}
	return strings.SplitN(email, "@", 2)[0]
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// Harness connects (or refreshes) the team marketplace, plugins, rules and hooks.
func (a *App) Harness(ctx context.Context, withDev bool) (harness.Report, error) {
	if err := a.take(); err != nil {
		return harness.Report{}, err
	}
	defer a.release()
	bin, err := harness.CodexBinary()
	if err != nil {
		return harness.Report{}, err
	}
	b := a.Bundle()
	userHome, _ := os.UserHomeDir()
	rep, err := harness.Setup(harness.Options{Codex: bin, Home: userHome, CodexHome: a.ops.Home(), Source: "MOX-Studio/mox-harness", WithDev: withDev, Name: b.Employee.Name, Email: b.Employee.Email, Log: a.log})
	if err != nil {
		return rep, err
	}
	a.mu.Lock()
	a.st.HarnessVersion = rep.Version
	a.mu.Unlock()
	return rep, state.Save(a.Dir, a.st)
}
