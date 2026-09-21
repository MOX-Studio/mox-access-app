package harness

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// codexRelease serves a latest-release document and a zip holding one executable named after the target triple, as
// openai/codex publishes (codex-x86_64-pc-windows-msvc.exe.zip → codex-x86_64-pc-windows-msvc.exe).
func codexRelease(t *testing.T, asset string) *httptest.Server {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, _ := zw.Create("codex-x86_64-pc-windows-msvc.exe")
	f.Write([]byte("#!/bin/sh\necho \"codex-cli 9.9.9\"\n"))
	zw.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "rust-v9.9.9", "assets": []map[string]string{
				{"name": "codex-app-server-x86_64-pc-windows-msvc.exe.zip", "browser_download_url": srv.URL + "/dl/other"},
				{"name": asset, "browser_download_url": srv.URL + "/dl/zip"},
			}})
		case "/dl/zip":
			w.Write(zipBuf.Bytes())
		default:
			w.WriteHeader(404)
		}
	}))
	return srv
}

func TestCodexCLIInstallKeepsTheBinaryUnderTheApplication(t *testing.T) {
	dir := t.TempDir()
	srv := codexRelease(t, "codex-test.exe.zip")
	defer srv.Close()
	c := &CodexCLI{Dir: dir, ReleaseAPI: srv.URL + "/releases/latest", HTTP: srv.Client(), Asset: "codex-test.exe.zip"}
	got, err := c.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "bin", codexExe) {
		t.Fatalf("installed at %s", got)
	}
	if info, err := os.Stat(got); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary missing or not executable: %v", err)
	}
	if _, err := os.Stat(got + ".part"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func TestCodexCLIInstallNamesTheMissingAsset(t *testing.T) {
	dir := t.TempDir()
	srv := codexRelease(t, "codex-test.exe.zip")
	defer srv.Close()
	c := &CodexCLI{Dir: dir, ReleaseAPI: srv.URL + "/releases/latest", HTTP: srv.Client(), Asset: "codex-absent.exe.zip"}
	if _, err := c.Install(context.Background()); err == nil || err.Error() != "в выпуске rust-v9.9.9 нет codex-absent.exe.zip" {
		t.Fatalf("err = %v", err)
	}
}
