// Package selfupdate replaces the running application with the latest GitHub release of MOX-Studio/mox-access-app:
// the asset for this platform is downloaded, checked against the digest the release lists, unpacked next to the
// installed copy, swapped in by rename and started; the old process then quits. The new process proves it runs by
// writing a marker — without it the old one tells the employee how to start the application by hand.
package selfupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultAPI = "https://api.github.com/repos/MOX-Studio/mox-access-app/releases/latest"

// MarkerName is the file the new process writes into the application directory once it has started.
const MarkerName = "updated"

// Release is the newest published version and the archive for this platform.
type Release struct {
	Version string // "0.5.3", without the leading v
	URL     string
	Digest  string // "sha256:<hex>" as the release lists it; empty when GitHub gave none
	Size    int64
}

type Updater struct {
	Current string // the running version, "0.5.2"; "dev" never updates
	Dir     string // application directory: staging and the marker
	Target  string // the installed copy to replace: "…/MOX Access.app" or "…\MOX Access.exe"
	API     string // latest-release endpoint; tests point it at a fake
	HTTP    *http.Client
	Log     func(string)
	Start   func(target string) error // starts the new copy; tests replace it
	Wait    time.Duration             // how long to wait for the new copy's marker
}

func (u *Updater) log(s string) {
	if u.Log != nil {
		u.Log(s)
	}
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Check returns the latest release when it is newer than the running version, nil when there is nothing to install.
func (u *Updater) Check(ctx context.Context) (*Release, error) {
	api := u.API
	if api == "" {
		api = defaultAPI
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MOX-Access/"+u.Current)
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("проверка обновления: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("проверка обновления: GitHub ответил %s", resp.Status)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("проверка обновления: %w", err)
	}
	version := strings.TrimPrefix(rel.Tag, "v")
	if !Newer(version, u.Current) {
		return nil, nil
	}
	want := AssetName(runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		if a.Name == want {
			return &Release{Version: version, URL: a.URL, Digest: a.Digest, Size: a.Size}, nil
		}
	}
	return nil, fmt.Errorf("в выпуске %s нет сборки %s", rel.Tag, want)
}

// AssetName is the archive the release workflow publishes for a platform.
func AssetName(goos, goarch string) string {
	if goos == "darwin" {
		return "MOX-Access-mac-" + goarch + ".zip"
	}
	return "MOX-Access-" + goos + "-" + goarch + ".zip"
}

// Newer reports whether version a is above b; a development build ("dev" or anything unparsable) is never updated.
func Newer(a, b string) bool {
	pa, okA := parse(a)
	pb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Apply installs rel over Target and starts it. It returns nil once the new copy has written its marker — the caller
// then quits — and ErrNotStarted when the copy is installed but did not report in time.
// ReleaseName is the application's name inside a release archive, without the extension.
const ReleaseName = "MOX Access"

func (u *Updater) Apply(ctx context.Context, rel *Release) error {
	if u.Target == "" {
		return errors.New("не найдено, где установлено приложение")
	}
	if err := translocated(u.Target); err != nil {
		return err
	}
	u.log("→ обновление: скачиваю " + rel.Version)
	archive, err := u.download(ctx, rel)
	if err != nil {
		return err
	}
	defer os.Remove(archive)
	staged := u.Target + ".new"
	os.RemoveAll(staged)
	// The release always ships "MOX Access.app" / "MOX Access.exe"; the installed copy may carry another name — Finder
	// calls a duplicate "MOX Access 3.app", a browser a second download "MOX Access (1).exe" (Lilya, 2026-10-07). The
	// archive is read by the release name and put in place of the copy that runs, whatever it is called.
	if err := unpack(archive, ReleaseName+filepath.Ext(u.Target), staged); err != nil {
		os.RemoveAll(staged)
		return fmt.Errorf("архив обновления: %w", err)
	}
	if err := prepare(staged); err != nil {
		os.RemoveAll(staged)
		return err
	}
	old := u.Target + ".old"
	os.RemoveAll(old)
	if err := os.Rename(u.Target, old); err != nil {
		os.RemoveAll(staged)
		return fmt.Errorf("не удалось заменить приложение (%s): %w", filepath.Dir(u.Target), err)
	}
	if err := os.Rename(staged, u.Target); err != nil {
		os.Rename(old, u.Target)
		os.RemoveAll(staged)
		return fmt.Errorf("не удалось заменить приложение: %w", err)
	}
	u.log("обновление: " + rel.Version + " установлено, запускаю")
	marker := filepath.Join(u.Dir, MarkerName)
	os.Remove(marker)
	if err := u.Start(u.Target); err != nil {
		return fmt.Errorf("%w: %v", ErrNotStarted, err)
	}
	wait := u.Wait
	if wait == 0 {
		wait = 20 * time.Second
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == rel.Version {
			return nil
		}
	}
	return ErrNotStarted
}

// ErrNotStarted: the new copy is in place but did not report — the employee has to start it by hand.
var ErrNotStarted = errors.New("новая версия установлена, но не запустилась сама")

func (u *Updater) download(ctx context.Context, rel *Release) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	req.Header.Set("User-Agent", "MOX-Access/"+u.Current)
	resp, err := u.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("скачивание обновления: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("скачивание обновления: %s", resp.Status)
	}
	if err := os.MkdirAll(u.Dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(u.Dir, "update-*.zip")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, 200<<20))
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("скачивание обновления: %w", err)
	}
	if want, ok := strings.CutPrefix(rel.Digest, "sha256:"); ok && !strings.EqualFold(want, hex.EncodeToString(h.Sum(nil))) {
		os.Remove(tmp.Name())
		return "", errors.New("скачанный архив не совпал с контрольной суммой выпуска — обновление отменено")
	}
	return tmp.Name(), nil
}

// unpack extracts the entry named root (a file, or a directory with everything under it) to dest.
func unpack(archive, root, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		var rel string
		switch {
		case name == root:
			rel = ""
		case strings.HasPrefix(name, root+"/"):
			rel = strings.TrimPrefix(name, root+"/")
		default:
			continue
		}
		if strings.Contains(rel, "..") {
			return fmt.Errorf("недопустимый путь в архиве: %s", f.Name)
		}
		found = true
		out := filepath.Join(dest, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := writeFile(f, out); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("в архиве нет %s", root)
	}
	return nil
}

func writeFile(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := f.Mode().Perm() | 0o600
	w, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// Cleanup removes what the previous version left behind; the new process calls it once the old one is gone.
func Cleanup(target string) {
	if target != "" {
		os.RemoveAll(target + ".old")
		os.RemoveAll(target + ".new")
	}
}

// MarkStarted is the new process's proof of life for the old one.
func MarkStarted(dir, version string) error {
	return os.WriteFile(filepath.Join(dir, MarkerName), []byte(version), 0o600)
}
