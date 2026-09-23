package migrate

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Meta mirrors <email>.json written by ops/mox-export-user.sh, as far as the application needs it.
type Meta struct {
	Employee string `json:"employee"`
	User     string `json:"user"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	Repos    []Repo `json:"repos"`
}

type Repo struct {
	Name     string  `json:"name"`
	Origin   *string `json:"origin"`
	Category string  `json:"category,omitempty"`
	Branch   string  `json:"branch"`
	Pushed   bool    `json:"pushed"`
	WIP      *string `json:"wip"`
}

// Download fetches /mox/export through the tunnel with the MOX key and verifies the checksum the gateway sends.
func Download(client *http.Client, origin, token, dst string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, origin+"/mox/export", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("шлюз не отвечает через туннель: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 401:
		return "", errors.New("ключ MOX отозван — попросите у студии новый файл .moxaccess")
	case 404:
		return "", errors.New("экспорта для вас ещё нет — напишите в аккаунт студии, его сделают на сервере")
	default:
		return "", fmt.Errorf("неожиданный ответ шлюза: %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), resp.Body); err != nil {
		out.Close()
		return "", fmt.Errorf("загрузка экспорта: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if want := resp.Header.Get("x-mox-sha256"); want != "" && want != sum {
		return "", errors.New("экспорт повреждён при загрузке (контрольная сумма не сошлась)")
	}
	return sum, nil
}

// Extract unpacks a .tar.zst into dir, refusing any entry that would land outside it.
func Extract(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	root, _ := filepath.Abs(dir)
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("архив повреждён: %w", err)
		}
		target := filepath.Join(root, h.Name)
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("архив содержит путь за пределами каталога: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode).Perm()|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

// ReadRepos reads repos.json from an extracted export.
func ReadRepos(dir string) ([]Repo, error) {
	data, err := os.ReadFile(filepath.Join(dir, "repos.json"))
	if err != nil {
		return nil, err
	}
	var repos []Repo
	return repos, json.Unmarshal(data, &repos)
}
