package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCodex records every invocation and answers `plugin marketplace list` from a file the test controls.
func fakeCodex(t *testing.T, dir, root string) string {
	t.Helper()
	bin := filepath.Join(dir, "codex")
	script := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(dir, "calls") + "\n" +
		"if [ \"$1 $2 $3\" = \"plugin marketplace list\" ]; then if [ -f " + filepath.Join(dir, "has-mox") + " ]; then printf 'MARKETPLACE  ROOT\\nmox          " + root + "\\n'; else printf 'MARKETPLACE  ROOT\\n'; fi; fi\n" +
		"if [ \"$1 $2 $3\" = \"plugin marketplace add\" ]; then touch " + filepath.Join(dir, "has-mox") + "; fi\n" +
		"if [ \"$1 $2\" = \"plugin list\" ]; then printf 'mox-core@mox  not installed\\n'; fi\nexit 0\n"
	os.WriteFile(bin, []byte(script), 0o755)
	return bin
}

func TestSetupOrder(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "marketplace")
	os.MkdirAll(filepath.Join(root, "hooks"), 0o755)
	os.WriteFile(filepath.Join(root, "catalog.json"), []byte(`{"version":"0.3.0"}`), 0o644)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Правила\n"), 0o644)
	home := filepath.Join(dir, "home")
	repo := filepath.Join(home, "MOX", "projects", "demo")
	os.MkdirAll(repo, 0o755)
	if err := exec.Command("git", "init", "-q", repo).Run(); err != nil {
		t.Fatal(err)
	}
	bin := fakeCodex(t, dir, root)
	rep, err := Setup(Options{Codex: bin, Home: home, CodexHome: filepath.Join(home, ".codex"), Source: "MOX-Studio/mox-harness", WithDev: true, Name: "Лиля", Email: "l@x.com", SkipGitHub: true, Log: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	want := "plugin marketplace list\nplugin marketplace add MOX-Studio/mox-harness\nplugin marketplace list\nplugin list\nplugin add mox-core@mox\nplugin add mox-dev@mox\n"
	if string(calls) != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", calls, want)
	}
	if rep.Version != "0.3.0" || rep.Root != root || rep.Repos != 1 {
		t.Fatalf("report: %+v", rep)
	}
	agents, _ := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if !strings.Contains(string(agents), "Правила") {
		t.Fatal("AGENTS block missing")
	}
	emp, _ := os.ReadFile(filepath.Join(home, ".mox", "employee"))
	if string(emp) != "name=Лиля\nemail=l@x.com\n" {
		t.Fatalf("employee: %q", emp)
	}
	cfg, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if !strings.Contains(string(cfg), "hooksPath = "+filepath.Join(root, "hooks")) {
		t.Fatalf("hooksPath not set:\n%s", cfg)
	}
	// Second run: marketplace exists → upgrade, plugins already installed → no add.
	os.WriteFile(filepath.Join(dir, "calls"), nil, 0o644)
	os.WriteFile(bin, []byte(strings.Replace(mustRead(t, bin), "not installed", "installed, enabled  0.3.0", 1)), 0o755)
	if _, err := Setup(Options{Codex: bin, Home: home, CodexHome: filepath.Join(home, ".codex"), Source: "MOX-Studio/mox-harness", SkipGitHub: true, Log: func(string) {}}); err != nil {
		t.Fatal(err)
	}
	calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
	if string(calls) != "plugin marketplace list\nplugin marketplace upgrade\nplugin marketplace list\nplugin list\n" {
		t.Fatalf("second run calls:\n%s", calls)
	}
}

func mustRead(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
