// Package ui is what the employee sees: the tray menu and a local status page. The page is plain HTML served on
// 127.0.0.1 only; it talks to the same App the tray drives.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/MOX-Studio/mox-access-app/internal/app"
)

//go:embed static/index.html
var static embed.FS

type Web struct {
	App     *app.App
	Version string
	LogPath string
	mu      sync.Mutex
	busy    bool
	srv     *http.Server
	url     string
}

func (w *Web) URL() string { return w.url }

// Start listens on a free loopback port and returns the URL of the page.
func (w *Web) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	w.url = "http://" + ln.Addr().String()
	w.srv = &http.Server{Handler: w.handler()}
	go w.srv.Serve(ln)
	return w.url, nil
}

func (w *Web) Stop() {
	if w.srv != nil {
		w.srv.Close()
	}
}

func (w *Web) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(rw, r)
			return
		}
		data, _ := static.ReadFile("static/index.html")
		rw.Header().Set("content-type", "text/html; charset=utf-8")
		rw.Write(data)
	})
	mux.HandleFunc("/api/status", func(rw http.ResponseWriter, r *http.Request) {
		s := w.App.Status()
		w.mu.Lock()
		busy := w.busy
		w.mu.Unlock()
		writeJSON(rw, 200, map[string]any{"version": w.Version, "mode": s.Mode, "employee": s.Employee, "keyText": s.KeyText, "harness": s.Harness, "busy": busy,
			"tunnel": map[string]any{"connected": s.Tunnel.Connected, "reconnects": s.Tunnel.Reconnects, "lastError": s.Tunnel.LastError}})
	})
	mux.HandleFunc("/api/log", func(rw http.ResponseWriter, r *http.Request) {
		data, _ := os.ReadFile(w.LogPath)
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) > 200 {
			lines = lines[len(lines)-200:]
		}
		rw.Header().Set("content-type", "text/plain; charset=utf-8")
		rw.Write([]byte(strings.Join(lines, "\n")))
	})
	action := func(name string, fn func(ctx context.Context) (string, error)) {
		mux.HandleFunc("/api/"+name, func(rw http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				writeJSON(rw, 405, map[string]any{"error": "POST"})
				return
			}
			w.mu.Lock()
			if w.busy {
				w.mu.Unlock()
				writeJSON(rw, 409, map[string]any{"error": "предыдущее действие ещё выполняется"})
				return
			}
			w.busy = true
			w.mu.Unlock()
			msg, err := fn(r.Context())
			w.mu.Lock()
			w.busy = false
			w.mu.Unlock()
			if err != nil {
				writeJSON(rw, 500, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(rw, 200, map[string]any{"message": msg})
		})
	}
	action("enable", func(ctx context.Context) (string, error) {
		return "корпоративный Codex включён", w.App.Enable(context.Background())
	})
	action("disable", func(ctx context.Context) (string, error) {
		return "личный Codex возвращён", w.App.Disable(context.Background())
	})
	action("check", func(ctx context.Context) (string, error) {
		if err := w.App.Check(ctx); err != nil {
			return "", err
		}
		return w.App.Status().KeyText, nil
	})
	action("github", func(ctx context.Context) (string, error) {
		user, err := w.App.GitHubLogin(ctx)
		return "вход в GitHub: " + user, err
	})
	action("harness", func(ctx context.Context) (string, error) {
		rep, err := w.App.Harness(ctx, true)
		if err != nil {
			return "", err
		}
		return "набор MOX " + rep.Version + " подключён — перезапустите Codex", nil
	})
	action("migrate", func(ctx context.Context) (string, error) {
		sum, err := w.App.Migrate(context.Background())
		if err != nil {
			return "", err
		}
		return "перенесено: тредов " + itoa(sum.Threads) + ", проектов " + itoa(sum.Repos), nil
	})
	return mux
}

func itoa(i int) string { return json.Number(strings.TrimSpace(string(mustJSON(i)))).String() }

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("content-type", "application/json; charset=utf-8")
	rw.WriteHeader(status)
	json.NewEncoder(rw).Encode(v)
}
