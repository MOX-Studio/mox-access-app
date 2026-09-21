// Package github makes the employee's machine able to talk to GitHub without a terminal: a gh binary (the one already
// installed, or a private copy under the application directory), a browser login whose one-time code the application
// shows, git credentials through gh, and the Command Line Tools that provide git. Every step checks first and acts only
// when something is missing.
package github

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/hide"
)

const defaultReleaseAPI = "https://api.github.com/repos/cli/cli/releases/latest"

type Client struct {
	Dir        string                 // application directory; a private gh lives in <Dir>/bin/gh
	Log        func(string)           // progress, one line per step
	Show       func(code, url string) // presents the one-time code to the employee; nil = log only
	ReleaseAPI string                 // latest-release endpoint; tests point it at a fake
	Candidates []string               // where an installed gh may be; nil = PATH, Homebrew, /usr/local/bin
	HTTP       *http.Client
	GitReady   func() bool  // git usable? nil = platform default (xcode-select -p on macOS)
	RequestGit func() error // ask the system to install git; nil = platform default
}

func (c *Client) log(s string) {
	if c.Log != nil {
		c.Log(s)
	}
}

func (c *Client) private() string { return filepath.Join(c.Dir, "bin", ghName) }

// Find returns the gh to use: the private copy, then whatever the machine already has. Empty when there is none.
func (c *Client) Find() string {
	candidates := c.Candidates
	if candidates == nil {
		if p, err := exec.LookPath(ghName); err == nil {
			candidates = append(candidates, p)
		}
		candidates = append(candidates, systemGh...)
	}
	for _, p := range append([]string{c.private()}, candidates...) {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0) {
			return p
		}
	}
	return ""
}

// Ensure returns a working gh, installing a private one when the machine has none. The private copy is also put on
// the PATH of the employee's shells: Codex and the team skills (new-project) call plain `gh`, not this application.
func (c *Client) Ensure(ctx context.Context) (string, error) {
	gh := c.Find()
	if gh == "" {
		var err error
		if gh, err = c.Install(ctx); err != nil {
			return "", err
		}
	}
	if gh == c.private() {
		if err := exposeOnPath(filepath.Dir(gh)); err != nil {
			c.log("gh в PATH оболочек не добавлен: " + err.Error())
		}
	}
	return gh, nil
}

// Install downloads the latest gh release for this platform and keeps only its binary under the application directory:
// no package installer, no administrator password.
func (c *Client) Install(ctx context.Context) (string, error) {
	api := c.ReleaseAPI
	if api == "" {
		api = defaultReleaseAPI
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	c.log("→ gh: скачиваю GitHub CLI")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("список версий GitHub CLI: %w", err)
	}
	defer resp.Body.Close()
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", fmt.Errorf("список версий GitHub CLI: %w", err)
	}
	want := assetSuffix + runtime.GOARCH + ".zip"
	url := ""
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, want) {
			url = a.URL
		}
	}
	if url == "" {
		return "", fmt.Errorf("в выпуске %s нет сборки GitHub CLI для %s", rel.Tag, runtime.GOARCH)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp2, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("скачивание GitHub CLI: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		return "", fmt.Errorf("скачивание GitHub CLI: %s", resp2.Status)
	}
	tmp, err := os.CreateTemp("", "gh-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp2.Body); err != nil {
		return "", fmt.Errorf("скачивание GitHub CLI: %w", err)
	}
	tmp.Close()
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return "", fmt.Errorf("архив GitHub CLI: %w", err)
	}
	defer zr.Close()
	var bin *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/bin/"+ghName) && !f.FileInfo().IsDir() {
			bin = f
		}
	}
	if bin == nil {
		return "", errors.New("архив GitHub CLI: в нём нет bin/gh")
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
	if v, err := hide.Cmd(exec.CommandContext(ctx, dest, "--version")).Output(); err != nil || !strings.Contains(string(v), "gh version") {
		os.Remove(dest)
		return "", fmt.Errorf("скачанный GitHub CLI не запускается: %v", err)
	}
	c.log("gh " + rel.Tag + " установлен: " + dest)
	return dest, nil
}

// LoggedIn asks gh itself; the exit code is the answer (1 when no host is authenticated).
func LoggedIn(gh string) bool { return hide.Cmd(exec.Command(gh, "auth", "status")).Run() == nil }

// User is the login of the active account, for messages; empty when unknown.
func User(gh string) string {
	out, _ := hide.Cmd(exec.Command(gh, "api", "user", "--jq", ".login")).Output()
	return strings.TrimSpace(string(out))
}

// EnsureLogin does nothing for a machine that is logged in; otherwise it runs the browser login.
func (c *Client) EnsureLogin(ctx context.Context, gh string) error {
	if LoggedIn(gh) {
		return nil
	}
	return c.Login(ctx, gh)
}

// gh names the code two ways: "! First copy your one-time code: XXXX-XXXX" and, with --clipboard (2.101),
// "! One-time code (XXXX-XXXX) copied to clipboard". The first live login (Lilya, 2026-09-21) met the second one.
var (
	codeRe = regexp.MustCompile(`(?i)one-time code[: (]+([A-Z0-9]{4}-[A-Z0-9]{4})`)
	urlRe  = regexp.MustCompile(`(https://\S+)`)
)

// Login runs gh's device flow without a terminal: gh prints the one-time code and opens the browser, the application
// shows the code (it is also on the clipboard), and the flow ends when the employee has signed in. Git is then pointed
// at gh for credentials, so private repositories clone under the employee's own account.
func (c *Client) Login(ctx context.Context, gh string) error {
	c.log("→ вход в GitHub через браузер")
	cmd := hide.Cmd(exec.CommandContext(ctx, gh, "auth", "login", "--hostname", "github.com", "--git-protocol", "https", "--web", "--skip-ssh-key", "--clipboard"))
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}
	var code, url, last string
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		last = line
		if m := codeRe.FindStringSubmatch(line); m != nil {
			code = m[1]
		}
		if code != "" && url == "" {
			if m := urlRe.FindStringSubmatch(line); m != nil && strings.Contains(line, "browser") {
				url = m[1]
				c.log("код показан сотруднику; жду вход в браузере")
				if c.Show != nil {
					c.Show(code, url)
				}
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("вход в GitHub не завершён: %v: %s", err, strings.TrimSpace(last))
	}
	if !LoggedIn(gh) {
		return errors.New("вход в GitHub не подтверждён")
	}
	if out, err := hide.Cmd(exec.CommandContext(ctx, gh, "auth", "setup-git")).CombinedOutput(); err != nil {
		return fmt.Errorf("gh auth setup-git: %v: %s", err, strings.TrimSpace(string(out)))
	}
	c.log("✅ вход в GitHub: " + User(gh))
	return nil
}

// EnsureGit checks that git is usable and, when it is not, asks the system to install it (macOS shows its own dialog
// for the Command Line Tools). The employee installs and runs the action again.
func (c *Client) EnsureGit() error {
	ready, request := c.GitReady, c.RequestGit
	if ready == nil {
		ready = gitReady
	}
	if request == nil {
		request = requestGit
	}
	if ready() {
		return nil
	}
	c.log("→ git: не найден, прошу систему установить")
	if err := request(); err != nil {
		return fmt.Errorf("установка git: %w", err)
	}
	if ready() { // the request installed it synchronously (winget on Windows)
		return nil
	}
	return errors.New(gitAfterRequest)
}
