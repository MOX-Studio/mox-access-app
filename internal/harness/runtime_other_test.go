//go:build !windows

package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 2026-09-25: Node from the .pkg (/usr/local/bin) and uv (~/.local/bin) were on the login PATH, yet the application,
// started from Finder with the launchd PATH, kept reporting both as missing.
func TestLoginPathReadsTheLoginShellPastProfileNoise(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "sh")
	script := "#!/bin/sh\necho 'Last login: profile noise'\nPATH=/from/login:/usr/bin\neval \"$2\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	if got := loginPath(); got != "/from/login:/usr/bin" {
		t.Fatalf("login PATH = %q", got)
	}
	t.Setenv("SHELL", filepath.Join(dir, "missing-shell"))
	t.Setenv("PATH", "/process/path")
	if got := loginPath(); got != "/process/path" {
		t.Fatalf("fallback PATH = %q", got)
	}
}

func TestMissingRuntimeLooksInTheGivenPath(t *testing.T) {
	dir := t.TempDir()
	for _, exe := range []string{"npx", "python3", "bash"} {
		if err := os.WriteFile(filepath.Join(dir, exe), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "uvx"), []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := missingRuntime(dir)
	if len(notes) != 1 || !strings.Contains(notes[0], "uv") || strings.Contains(notes[0], "brew") {
		t.Fatalf("notes = %q", notes)
	}
}
