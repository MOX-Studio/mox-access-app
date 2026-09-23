package harness

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallWorkspace keeps the editable instruction in ~/AI. Codex still requires a mirror in ~/.codex;
// Claude reads ~/AI/CLAUDE.md, which imports the same canonical AGENTS.md.
func InstallWorkspace(home, codexHome, body, version, profile string) (string, error) {
	ai := filepath.Join(home, "AI")
	for _, dir := range []string{ai, filepath.Join(ai, "Project"), filepath.Join(ai, "Project", "MOX"), filepath.Join(ai, "Project", "Personal")} {
		if err := plainDir(dir); err != nil {
			return "", err
		}
	}
	canonical := filepath.Join(ai, "AGENTS.md")
	claude := filepath.Join(ai, "CLAUDE.md")
	mirror := filepath.Join(codexHome, "AGENTS.md")
	marker := filepath.Join(ai, ".codex-agents-mirror.sha256")
	for _, path := range []string{canonical, claude, mirror, marker} {
		if err := plainFileOrMissing(path); err != nil {
			return "", err
		}
	}
	source, sourceErr := os.ReadFile(mirror)
	before, beforeErr := os.ReadFile(canonical)
	recorded, markerErr := os.ReadFile(marker)
	if sourceErr != nil && !os.IsNotExist(sourceErr) || beforeErr != nil && !os.IsNotExist(beforeErr) || markerErr != nil && !os.IsNotExist(markerErr) {
		return "", fmt.Errorf("не удалось прочитать инструкции рабочей области AI")
	}
	if sourceErr == nil && beforeErr == nil {
		if markerErr == nil && string(hash(source)) != strings.TrimSpace(string(recorded)) && !bytes.Equal(source, before) {
			return "", fmt.Errorf("%s изменён вне ~/AI; сначала перенесите правки в %s", mirror, canonical)
		}
		if os.IsNotExist(markerErr) && !bytes.Equal(source, before) {
			return "", fmt.Errorf("%s и %s различаются; сначала сведите инструкции", mirror, canonical)
		}
	}
	if os.IsNotExist(beforeErr) && sourceErr == nil {
		if err := atomicFile(canonical, source); err != nil {
			return "", err
		}
	}
	if err := ApplyAgentsBlockWithProfile(canonical, body, version, profile); err != nil {
		return "", err
	}
	current, err := os.ReadFile(canonical)
	if err != nil {
		return "", err
	}
	claudeText, err := os.ReadFile(claude)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if os.IsNotExist(err) {
		claudeText = []byte("# Инструкция AI\n\n@AGENTS.md\n")
	} else if !bytes.Contains(claudeText, []byte("@AGENTS.md")) {
		claudeText = append(bytes.TrimRight(claudeText, "\n"), []byte("\n\n@AGENTS.md\n")...)
	}
	if err := atomicFile(claude, claudeText); err != nil {
		return "", err
	}
	if err := atomicFile(mirror, current); err != nil {
		return "", err
	}
	if err := atomicFile(marker, []byte(hash(current)+"\n")); err != nil {
		return "", err
	}
	return canonical, nil
}

func hash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func plainDir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: нужен обычный каталог", path)
	}
	return nil
}

func plainFileOrMissing(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: нужен обычный файл без ссылки", path)
	}
	return nil
}

func atomicFile(path string, data []byte) error {
	if err := plainDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mox-ai-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
