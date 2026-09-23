package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsBlock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(target, []byte("# Мой AGENTS\n\nЯ — Лиля.\n"), 0o644)
	if err := ApplyAgentsBlock(target, "# Правила v1\n", "0.1.0"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	s := string(got)
	if !strings.Contains(s, "Я — Лиля") || !strings.Contains(s, beginMarker) || !strings.Contains(s, "Правила v1") {
		t.Fatalf("first insert:\n%s", s)
	}
	if err := ApplyAgentsBlock(target, "# Правила v2\n", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(target)
	s = string(got)
	if strings.Count(s, beginMarker) != 1 || !strings.Contains(s, "Правила v2") || strings.Contains(s, "Правила v1") || !strings.Contains(s, "Я — Лиля") {
		t.Fatalf("update in place:\n%s", s)
	}
	before := s
	ApplyAgentsBlock(target, "# Правила v2\n", "0.2.0")
	got, _ = os.ReadFile(target)
	if string(got) != before {
		t.Fatal("not idempotent")
	}
	fresh := filepath.Join(dir, "new", "AGENTS.md")
	if err := ApplyAgentsBlock(fresh, "# Правила v2\n", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(fresh); !strings.Contains(string(got), "Правила v2") {
		t.Fatal("create when missing")
	}
	bad := filepath.Join(dir, "bad.md")
	os.WriteFile(bad, []byte(beginMarker+"\nbroken\n"), 0o644)
	if err := ApplyAgentsBlock(bad, "x", "1"); err == nil {
		t.Fatal("malformed markers must error")
	}
}

func TestPersonalProfilePreserved(t *testing.T) {
	target := filepath.Join(t.TempDir(), "AGENTS.md")
	profile := "<!-- mox-personal:begin -->\nЯ — Динара.\n<!-- mox-personal:end -->\n"
	if err := ApplyAgentsBlockWithProfile(target, "командные v1", "0.3.0", profile); err != nil {
		t.Fatal(err)
	}
	first := mustRead(t, target)
	if !strings.Contains(first, "Я — Динара") || !strings.Contains(first, "командные v1") {
		t.Fatal("личный или командный блок не установлен")
	}
	changed := strings.Replace(first, "Я — Динара.", "Я — Динара. Пиши кратко.", 1)
	os.WriteFile(target, []byte(changed), 0o644)
	if err := ApplyAgentsBlockWithProfile(target, "командные v2", "0.3.1", profile); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, target)
	if strings.Count(got, "mox-personal:begin") != 1 || !strings.Contains(got, "Пиши кратко") || !strings.Contains(got, "командные v2") {
		t.Fatalf("обновление стерло личный текст: %s", got)
	}
	if profileFor("Вова", t.TempDir()) != "" || !strings.HasSuffix(profileFor("Dinara Bekman", "/tmp/harness"), filepath.Join("profiles", "dinara.md")) {
		t.Fatal("неверный выбор личного профиля")
	}
}

func TestPersonalProfileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.md")
	link := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(source, []byte("сохранить"), 0o644)
	if err := os.Symlink(source, link); err != nil {
		t.Skipf("symlink недоступен: %v", err)
	}
	if err := ApplyAgentsBlockWithProfile(link, "rules", "0.3.0", "личное"); err == nil {
		t.Fatal("ссылка принята")
	}
	if got := mustRead(t, source); got != "сохранить" {
		t.Fatal("цель ссылки изменена")
	}
}
