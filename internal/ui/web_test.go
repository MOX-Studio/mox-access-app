package ui

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MOX-Studio/mox-access-app/internal/app"
)

type noOps struct{ home string }

func (n noOps) Home() string                 { return n.home }
func (noOps) TrustCert(string) error         { return nil }
func (noOps) SetEnv(map[string]string) error { return nil }
func (noOps) UnsetEnv([]string) error        { return nil }
func (noOps) QuitCodex() error               { return nil }
func (noOps) LaunchCodex() error             { return nil }
func (noOps) RestartHint() string            { return "" }

func TestStatusAndPage(t *testing.T) {
	dir := t.TempDir()
	a, err := app.New(filepath.Join(dir, "app"), noOps{home: filepath.Join(dir, ".codex")}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	w := &Web{App: a, Version: "test", LogPath: filepath.Join(dir, "log")}
	url, err := w.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	resp, err := http.Get(url + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	json.NewDecoder(resp.Body).Decode(&s)
	if s["mode"] != "personal" || s["version"] != "test" {
		t.Fatalf("status: %v", s)
	}
	resp, _ = http.Get(url + "/")
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), "MOX Access") {
		t.Fatal("page")
	}
	resp, _ = http.Post(url+"/api/enable", "", nil)
	if resp.StatusCode != 500 {
		t.Fatalf("enable without bundle must fail: %d", resp.StatusCode)
	}
}
