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
