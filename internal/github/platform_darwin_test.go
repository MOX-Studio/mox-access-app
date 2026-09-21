//go:build darwin

package github

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The private gh goes on the PATH of login shells: one marked block per profile, never twice, $HOME kept symbolic.
func TestExposeOnPathAddsTheBlockOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.WriteFile(filepath.Join(home, ".zprofile"), []byte("export FOO=1"), 0o644) // no trailing newline
	bin := filepath.Join(home, "Library", "Application Support", "MOX Access", "bin")
	for i := 0; i < 2; i++ {
		if err := exposeOnPath(bin); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".zprofile", ".bash_profile"} {
		data, _ := os.ReadFile(filepath.Join(home, name))
		if strings.Count(string(data), pathMarker) != 1 || !strings.Contains(string(data), `export PATH="$HOME/Library/Application Support/MOX Access/bin:$PATH"`) {
			t.Fatalf("%s:\n%s", name, data)
		}
	}
	z, _ := os.ReadFile(filepath.Join(home, ".zprofile"))
	if !strings.HasPrefix(string(z), "export FOO=1\n"+pathMarker) {
		t.Fatalf(".zprofile lost its content or the newline:\n%s", z)
	}
}
