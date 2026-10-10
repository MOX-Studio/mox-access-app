// Package ui is what the employee sees: the tray menu and a local status page. The page is plain HTML served on
// 127.0.0.1 only; it talks to the same App the tray drives.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/MOX-Studio/mox-access-app/internal/app"
)

// The page carries the look of mox-studio.ru: its font (Inter Tight, OFL) is served from the binary, so the window
// works offline; the mark is inline SVG.
//
//go:embed static/index.html static/inter-tight.woff
var static embed.FS

// ownHost answers only requests addressed to the listener itself. A site whose domain is re-pointed at 127.0.0.1 (DNS
// rebinding) becomes same-origin with the page and could read the status and log and send the page's header; its
// requests still carry its own name in Host, and are refused here.
func ownHost(addr string, next http.Handler) http.Handler {
	_, port, _ := net.SplitHostPort(addr)
	allowed := map[string]bool{addr: true, "localhost:" + port: true}
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

// guarded rejects a POST without the page's own header: a custom header makes a browser preflight a cross-site
// request, and this server answers no CORS, so another site cannot press the buttons (or import a file) for the employee.
func guarded(rw http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeJSON(rw, 405, map[string]any{"error": "POST"})
		return false
	}
	if r.Header.Get("X-MOX-Access") != "1" {
		writeJSON(rw, 403, map[string]any{"error": "запрос не из окна MOX Access"})
		return false
	}
	return true
}

type Web struct {
	App     *app.App
	Version string
	LogPath string
	Updates *Updates
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
	w.srv = &http.Server{Handler: ownHost(ln.Addr().String(), w.handler())}
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
	mux.HandleFunc("/inter-tight.woff", func(rw http.ResponseWriter, r *http.Request) {
		data, _ := static.ReadFile("static/inter-tight.woff")
		rw.Header().Set("content-type", "font/woff")
		rw.Header().Set("cache-control", "max-age=86400")
		rw.Write(data)
	})
	mux.HandleFunc("/api/status", func(rw http.ResponseWriter, r *http.Request) {
		s := w.App.Status()
		w.mu.Lock()
		busy := w.busy
		w.mu.Unlock()
		secrets := make([]map[string]any, 0, len(s.Secrets))
		for _, sec := range s.Secrets {
			secrets = append(secrets, map[string]any{"name": sec.Name, "version": sec.Version, "skipped": sec.Skipped})
		}
		writeJSON(rw, 200, map[string]any{"version": w.Version, "mode": s.Mode, "employee": s.Employee, "keyText": s.KeyText, "login": s.Login, "harness": s.Harness, "busy": busy, "update": w.Updates.Available(),
			"platform": runtime.GOOS, "hint": s.Hint, "secrets": secrets, "secretsPending": s.SecretsPending,
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
			if !guarded(rw, r) {
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
	action("restart-codex", func(ctx context.Context) (string, error) {
		return "Codex перезапущен — новые доступы подключены", w.App.RestartCodex()
	})
	// The .moxaccess file chosen in the window: the page sends its bytes, Import reads them from a private temp file.
	mux.HandleFunc("/api/import", func(rw http.ResponseWriter, r *http.Request) {
		if !guarded(rw, r) {
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(rw, 400, map[string]any{"error": "файл не прочитан"})
			return
		}
		tmp, err := os.MkdirTemp("", "mox-import-")
		if err != nil {
			writeJSON(rw, 500, map[string]any{"error": err.Error()})
			return
		}
		defer os.RemoveAll(tmp)
		path := filepath.Join(tmp, "bundle.moxaccess")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			writeJSON(rw, 500, map[string]any{"error": err.Error()})
			return
		}
		if err := w.App.Import(path); err != nil {
			writeJSON(rw, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(rw, 200, map[string]any{"message": "файл принят"})
	})
	action("update", func(ctx context.Context) (string, error) {
		return w.Updates.Install(ctx)
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
