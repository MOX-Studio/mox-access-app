package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWorkspaceMigratesLegacyAndKeepsPersonalEdits(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(codexHome, "AGENTS.md")
	if err := os.WriteFile(legacy, []byte("# Мои прежние правила\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := "<!-- mox-personal:begin -->\nЯ — Катя.\n<!-- mox-personal:end -->\n"
	canonical, err := InstallWorkspace(home, codexHome, "# Правила MOX", "0.4.0", profile)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(canonical)
	if !strings.Contains(string(first), "Мои прежние правила") || !strings.Contains(string(first), "Я — Катя") {
		t.Fatalf("legacy/profile missing: %s", first)
	}
	if err := os.WriteFile(canonical, append(first, []byte("\nМой выбор: кратко.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallWorkspace(home, codexHome, "# Правила MOX v2", "0.4.1", profile); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(canonical)
	mirror, _ := os.ReadFile(legacy)
	if string(second) != string(mirror) || !strings.Contains(string(second), "Мой выбор: кратко.") || !strings.Contains(string(second), "Правила MOX v2") || strings.Count(string(second), "mox-personal:begin") != 1 {
		t.Fatalf("update did not preserve canonical edits: %s", second)
	}
	for _, category := range []string{"MOX", "Personal"} {
		if info, err := os.Stat(filepath.Join(home, "AI", "Project", category)); err != nil || !info.IsDir() {
			t.Fatalf("category %s missing", category)
		}
	}
	if err := os.WriteFile(legacy, []byte("manual mirror edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallWorkspace(home, codexHome, "# Правила MOX v3", "0.4.2", profile); err == nil {
		t.Fatal("changed Codex mirror overwritten")
	}
}

func TestInstallWorkspaceRejectsLinkedRoot(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "AI")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := InstallWorkspace(home, filepath.Join(home, ".codex"), "body", "1", ""); err == nil {
		t.Fatal("linked AI root accepted")
	}
}
