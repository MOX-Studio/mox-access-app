package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.5.3", "0.5.2", true}, {"0.10.0", "0.9.9", true}, {"1.0.0", "0.99.99", true}, {"0.5.2", "0.5.2", false},
		{"0.5.1", "0.5.2", false}, {"0.5.3", "dev", false}, {"dev", "0.5.2", false}, {"0.5", "0.4.9", false}} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// bundleZip is a release archive with a bundle directory "MOX Access.app" holding one binary.
func bundleZip(t *testing.T, content string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.Create("MOX Access.app/")
	h := &zip.FileHeader{Name: "MOX Access.app/Contents/MacOS/moxaccess", Method: zip.Deflate}
	h.SetMode(0o755)
	w, _ := zw.CreateHeader(h)
	w.Write([]byte(content))
	zw.Close()
	return buf.Bytes()
}

// release serves a latest-release answer for tag and the archive it points to; digest "" omits it.
func release(t *testing.T, tag string, archive []byte, digest string) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive.zip" {
			w.Write(archive)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []map[string]any{
			{"name": "MOX-Access-other.zip", "browser_download_url": srv.URL + "/nope"},
			{"name": AssetName(runtime.GOOS, runtime.GOARCH), "browser_download_url": srv.URL + "/archive.zip", "digest": digest, "size": len(archive)},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sum(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }

func installed(t *testing.T, content string) (dir, target string) {
	dir = t.TempDir()
	target = filepath.Join(dir, "Applications", "MOX Access.app")
	os.MkdirAll(filepath.Join(target, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(target, "Contents", "MacOS", "moxaccess"), []byte(content), 0o755)
	return dir, target
}

func TestCheckReportsOnlyNewer(t *testing.T) {
	srv := release(t, "v0.5.2", nil, "")
	u := &Updater{Current: "0.5.2", API: srv.URL}
	if rel, err := u.Check(context.Background()); err != nil || rel != nil {
		t.Fatalf("same version: %v %v", rel, err)
	}
	u.Current = "0.5.1"
	rel, err := u.Check(context.Background())
	if err != nil || rel == nil || rel.Version != "0.5.2" || !strings.HasSuffix(rel.URL, "/archive.zip") {
		t.Fatalf("newer version: %+v %v", rel, err)
	}
}

func TestApplySwapsStartsAndWaitsForTheNewCopy(t *testing.T) {
	archive := bundleZip(t, "new")
	srv := release(t, "v0.5.3", archive, sum(archive))
	dir, target := installed(t, "old")
	var started string
	u := &Updater{Current: "0.5.2", Dir: filepath.Join(dir, "app"), Target: target, API: srv.URL, Wait: 2 * time.Second,
		Start: func(tg string) error { started = tg; return MarkStarted(filepath.Join(dir, "app"), "0.5.3") }}
	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	if started != target {
		t.Fatalf("started %q", started)
	}
	bin := filepath.Join(target, "Contents", "MacOS", "moxaccess")
	if b, _ := os.ReadFile(bin); string(b) != "new" {
		t.Fatalf("binary after update: %q", b)
	}
	if st, _ := os.Stat(bin); st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("binary lost its exec bit: %v", st.Mode())
	}
	if b, _ := os.ReadFile(filepath.Join(target+".old", "Contents", "MacOS", "moxaccess")); string(b) != "old" {
		t.Fatal("previous copy must stay aside until the new process cleans it up")
	}
	Cleanup(target)
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatal("cleanup left the old copy")
	}
}

func TestApplyRefusesADigestMismatchAndKeepsTheInstalledCopy(t *testing.T) {
	archive := bundleZip(t, "tampered")
	srv := release(t, "v0.5.3", archive, sum([]byte("something else")))
	dir, target := installed(t, "old")
	u := &Updater{Current: "0.5.2", Dir: filepath.Join(dir, "app"), Target: target, API: srv.URL,
		Start: func(string) error { t.Fatal("must not start"); return nil }}
	rel, _ := u.Check(context.Background())
	if err := u.Apply(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "контрольной суммой") {
		t.Fatalf("expected digest error, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "Contents", "MacOS", "moxaccess")); string(b) != "old" {
		t.Fatal("installed copy changed")
	}
}

func TestApplyReportsACopyThatDidNotStart(t *testing.T) {
	archive := bundleZip(t, "new")
	srv := release(t, "v0.5.3", archive, sum(archive))
	dir, target := installed(t, "old")
	u := &Updater{Current: "0.5.2", Dir: filepath.Join(dir, "app"), Target: target, API: srv.URL, Wait: 300 * time.Millisecond,
		Start: func(string) error { return nil }}
	rel, _ := u.Check(context.Background())
	if err := u.Apply(context.Background(), rel); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("expected ErrNotStarted, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "Contents", "MacOS", "moxaccess")); string(b) != "new" {
		t.Fatal("the new copy must be in place for a manual start")
	}
}

// Windows ships a single executable at the archive root.
func TestApplyReplacesASingleExecutable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("MOX Access.exe")
	w.Write([]byte("new"))
	zw.Close()
	archive := buf.Bytes()
	srv := release(t, "v0.5.3", archive, "")
	dir := t.TempDir()
	target := filepath.Join(dir, "MOX Access.exe")
	os.WriteFile(target, []byte("old"), 0o755)
	u := &Updater{Current: "0.5.2", Dir: filepath.Join(dir, "app"), Target: target, API: srv.URL, Wait: 2 * time.Second,
		Start: func(string) error { return MarkStarted(filepath.Join(dir, "app"), "0.5.3") }}
	rel, _ := u.Check(context.Background())
	if err := u.Apply(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("exe after update: %q", b)
	}
}

func TestTargetFindsTheBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("bundle layout is macOS only")
	}
	if got := Target("/Applications/MOX Access.app/Contents/MacOS/moxaccess"); got != "/Applications/MOX Access.app" {
		t.Fatalf("Target = %q", got)
	}
	if got := Target("/tmp/moxaccess"); got != "" {
		t.Fatalf("bare binary: %q", got)
	}
}
