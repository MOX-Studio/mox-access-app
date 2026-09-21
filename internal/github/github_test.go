package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeGh writes a shell script that plays gh: `auth status` succeeds once <dir>/logged-in exists, `auth login` prints
// the device-flow lines to stderr and then logs in, `auth setup-git` and `--version` are recorded.
func fakeGh(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "gh")
	script := `#!/bin/sh
case "$1 $2" in
  "auth status") [ -e "` + dir + `/logged-in" ] ;;
  "auth login") echo "" >&2; echo "! One-time code (ABCD-1234) copied to clipboard" >&2; echo "Open this URL to continue in your web browser: https://github.com/login/device" >&2; sleep 0.3; touch "` + dir + `/logged-in" ;;
  "auth setup-git") touch "` + dir + `/setup-git" ;;
  "--version ") echo "gh version 9.9.9 (test)" ;;
  *) exit 2 ;;
esac
`
	os.WriteFile(p, []byte(script), 0o755)
	return p
}

func TestFindTakesTheInstalledGh(t *testing.T) {
	dir := t.TempDir()
	gh := fakeGh(t, dir)
	c := &Client{Dir: filepath.Join(dir, "app"), Candidates: []string{filepath.Join(dir, "nowhere", "gh"), gh}}
	if got := c.Find(); got != gh {
		t.Fatalf("Find = %q, want %q", got, gh)
	}
	c = &Client{Dir: filepath.Join(dir, "app"), Candidates: []string{filepath.Join(dir, "nowhere", "gh")}}
	if got := c.Find(); got != "" {
		t.Fatalf("Find on an empty machine = %q, want empty", got)
	}
}

// release serves a latest-release document and the zip it points to, shaped like cli/cli on GitHub.
func release(t *testing.T) *httptest.Server {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, _ := zw.Create("gh_9.9.9_macOS_" + runtime.GOARCH + "/bin/gh")
	f.Write([]byte("#!/bin/sh\ncase \"$1\" in --version) echo \"gh version 9.9.9 (test)\";; esac\n"))
	zw.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9", "assets": []map[string]string{
				{"name": "gh_9.9.9_macOS_universal.pkg", "browser_download_url": srv.URL + "/dl/pkg"},
				{"name": "gh_9.9.9_macOS_" + runtime.GOARCH + ".zip", "browser_download_url": srv.URL + "/dl/zip"},
			}})
		case "/dl/zip":
			w.Write(zipBuf.Bytes())
		default:
			w.WriteHeader(404)
		}
	}))
	return srv
}

func TestInstallPutsAPrivateGhUnderTheApplication(t *testing.T) {
	dir := t.TempDir()
	srv := release(t)
	defer srv.Close()
	var logs []string
	c := &Client{Dir: filepath.Join(dir, "app"), Candidates: []string{filepath.Join(dir, "nowhere", "gh")}, ReleaseAPI: srv.URL + "/releases/latest", Log: func(s string) { logs = append(logs, s) }}
	gh, err := c.Ensure(context.Background())
	if err != nil {
		t.Fatalf("ensure: %v (%v)", err, logs)
	}
	if gh != filepath.Join(dir, "app", "bin", "gh") {
		t.Fatalf("installed at %q", gh)
	}
	if info, err := os.Stat(gh); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("not executable: %v", err)
	}
	if got := c.Find(); got != gh {
		t.Fatalf("Find after install = %q", got)
	}
	// A second Ensure changes nothing: the private copy is found first.
	again, err := c.Ensure(context.Background())
	if err != nil || again != gh {
		t.Fatalf("second ensure: %q %v", again, err)
	}
}

func TestLoginShowsTheCodeAndSetsUpGit(t *testing.T) {
	dir := t.TempDir()
	gh := fakeGh(t, dir)
	if LoggedIn(gh) {
		t.Fatal("must start logged out")
	}
	var shownCode, shownURL string
	c := &Client{Dir: dir, Show: func(code, url string) { shownCode, shownURL = code, url }}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.EnsureLogin(ctx, gh); err != nil {
		t.Fatalf("login: %v", err)
	}
	if shownCode != "ABCD-1234" || !strings.Contains(shownURL, "github.com/login/device") {
		t.Fatalf("shown %q %q", shownCode, shownURL)
	}
	if !LoggedIn(gh) {
		t.Fatal("not logged in after the flow")
	}
	if _, err := os.Stat(filepath.Join(dir, "setup-git")); err != nil {
		t.Fatal("git credentials not set up through gh")
	}
	// Logged in already: EnsureLogin asks nothing.
	shownCode = ""
	if err := c.EnsureLogin(ctx, gh); err != nil || shownCode != "" {
		t.Fatalf("second EnsureLogin: %v, shown %q", err, shownCode)
	}
}

func TestEnsureGitAsksForTheToolsOnce(t *testing.T) {
	ready := false
	requested := 0
	c := &Client{GitReady: func() bool { return ready }, RequestGit: func() error { requested++; return nil }}
	if err := c.EnsureGit(); err == nil || requested != 1 {
		t.Fatalf("missing tools: err=%v requested=%d", err, requested)
	}
	ready = true
	if err := c.EnsureGit(); err != nil || requested != 1 {
		t.Fatalf("tools present: err=%v requested=%d", err, requested)
	}
}

// Both spellings gh uses for the code are recognised; a line without a code is not.
func TestCodeLineFormats(t *testing.T) {
	for line, want := range map[string]string{
		"! First copy your one-time code: ABCD-1234":                                     "ABCD-1234",
		"! One-time code (7XYZ-0K9Q) copied to clipboard":                                "7XYZ-0K9Q",
		"Open this URL to continue in your web browser: https://github.com/login/device": "",
	} {
		got := ""
		if m := codeRe.FindStringSubmatch(line); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q → %q, want %q", line, got, want)
		}
	}
}
