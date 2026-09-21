package harness

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/hide"
)

// CodexCLI finds or installs the codex binary the harness manages plugins with. On macOS it is the engine inside
// ChatGPT.app; on Windows the engine sits inside the MSIX package, whose folder a user cannot read, so a private copy of
// the standalone CLI from openai/codex releases lives under the application directory — the way gh does.
type CodexCLI struct {
	Dir        string       // application directory; the binary goes to Dir/bin
	ReleaseAPI string       // latest-release document; empty = openai/codex on GitHub
	HTTP       *http.Client // nil = a client with a long timeout (the archive is ~100 MB)
	Asset      string       // release asset name; empty = the platform default (Windows: codex-<arch>-pc-windows-msvc.exe.zip)
	Log        func(string)
}

const defaultCodexReleaseAPI = "https://api.github.com/repos/openai/codex/releases/latest"

func (c *CodexCLI) log(s string) {
	if c.Log != nil {
		c.Log(s)
	}
}

func (c *CodexCLI) private() string { return filepath.Join(c.Dir, "bin", codexExe) }

// Ensure returns a usable codex: the engine of ChatGPT.app, one on PATH, the private copy, or — Windows only — a
// fresh download. macOS without ChatGPT.app is an error: the application is the engine there.
func (c *CodexCLI) Ensure(ctx context.Context) (string, error) {
	if p, err := CodexBinary(); err == nil {
		return p, nil
	}
	if fileExists(c.private()) {
		return c.private(), nil
	}
	if runtime.GOOS != "windows" && c.Asset == "" {
		return "", errors.New("Codex не найден: установите ChatGPT.app")
	}
	return c.Install(ctx)
}

// Install downloads the standalone CLI for this platform and keeps only its binary under the application directory.
func (c *CodexCLI) Install(ctx context.Context) (string, error) {
	api := c.ReleaseAPI
	if api == "" {
		api = defaultCodexReleaseAPI
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	want := c.Asset
	if want == "" {
		want = codexAsset()
	}
	c.log("→ codex: скачиваю Codex CLI (" + want + ")")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("список версий Codex CLI: %w", err)
	}
	defer resp.Body.Close()
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return "", fmt.Errorf("список версий Codex CLI: %w", err)
	}
	url := ""
	for _, a := range rel.Assets {
		if a.Name == want {
			url = a.URL
		}
	}
	if url == "" {
		return "", fmt.Errorf("в выпуске %s нет %s", rel.Tag, want)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp2, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("скачивание Codex CLI: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		return "", fmt.Errorf("скачивание Codex CLI: %s", resp2.Status)
	}
	tmp, err := os.CreateTemp("", "codex-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp2.Body); err != nil {
		return "", fmt.Errorf("скачивание Codex CLI: %w", err)
	}
	tmp.Close()
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return "", fmt.Errorf("архив Codex CLI: %w", err)
	}
	defer zr.Close()
	// The archive holds one executable named after the target triple (codex-x86_64-pc-windows-msvc.exe).
	var bin *zip.File
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if !f.FileInfo().IsDir() && strings.HasPrefix(base, "codex") && !strings.Contains(base, "app-server") {
			bin = f
		}
	}
	if bin == nil {
		return "", errors.New("архив Codex CLI: в нём нет бинарника codex")
	}
	dest := c.private()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	rc, err := bin.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return "", err
	}
	out.Close()
	if err := os.Rename(dest+".part", dest); err != nil {
		return "", err
	}
	if v, err := hide.Cmd(exec.CommandContext(ctx, dest, "--version")).Output(); err != nil || !strings.Contains(string(v), "codex") {
		os.Remove(dest)
		return "", fmt.Errorf("скачанный Codex CLI не запускается: %v", err)
	}
	c.log("codex " + rel.Tag + " установлен: " + dest)
	return dest, nil
}

// codexAsset names the standalone CLI archive of this platform in openai/codex releases.
func codexAsset() string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	switch runtime.GOOS {
	case "windows":
		return "codex-" + arch + "-pc-windows-msvc.exe.zip"
	default:
		return "codex-" + arch + "-apple-darwin.tar.gz" // not used: ChatGPT.app carries the engine
	}
}
