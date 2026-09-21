package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Options struct {
	Codex      string // path to the codex binary
	Home       string // $HOME
	CodexHome  string // ~/.codex
	Source     string // "MOX-Studio/mox-harness"
	WithDev    bool
	Name       string
	Email      string
	SkipGitHub bool // tests only: no gh auth status check
	Log        func(string)
}

type Report struct {
	Version string
	Root    string
	Plugins []string
	Repos   int
}

// CodexBinary is the engine inside ChatGPT.app, or codex on PATH.
func CodexBinary() (string, error) {
	if p := "/Applications/ChatGPT.app/Contents/Resources/codex"; fileExists(p) {
		return p, nil
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p, nil
	}
	return "", errors.New("Codex не найден: установите ChatGPT.app")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func (o Options) codex(args ...string) (string, error) {
	cmd := exec.Command(o.Codex, args...)
	cmd.Env = append(os.Environ(), "HOME="+o.Home, "CODEX_HOME="+o.CodexHome)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("codex %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (o Options) marketplaceRoot() string {
	out, _ := o.codex("plugin", "marketplace", "list")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "mox" {
			return f[1]
		}
	}
	return ""
}

// Setup performs the steps of setup.sh in order; each step is logged and an error names the step.
func Setup(o Options) (Report, error) {
	if o.Log == nil {
		o.Log = func(string) {}
	}
	var rep Report
	if !o.SkipGitHub {
		o.Log("→ gh")
		if exec.Command("gh", "auth", "status").Run() != nil {
			return rep, errors.New("шаг «gh»: войдите в GitHub своим аккаунтом: gh auth login")
		}
	}
	o.Log("→ marketplace mox (" + o.Source + ")")
	if o.marketplaceRoot() != "" {
		if _, err := o.codex("plugin", "marketplace", "upgrade"); err != nil {
			return rep, fmt.Errorf("шаг «marketplace»: %w", err)
		}
	} else if _, err := o.codex("plugin", "marketplace", "add", o.Source); err != nil {
		return rep, fmt.Errorf("шаг «marketplace»: %w", err)
	}
	rep.Root = o.marketplaceRoot()
	if rep.Root == "" || !fileExists(filepath.Join(rep.Root, "catalog.json")) {
		return rep, errors.New("шаг «marketplace»: клон harness не найден после установки")
	}
	var cat struct {
		Version string `json:"version"`
	}
	data, _ := os.ReadFile(filepath.Join(rep.Root, "catalog.json"))
	_ = json.Unmarshal(data, &cat)
	rep.Version = cat.Version
	o.Log("→ плагины")
	installed, _ := o.codex("plugin", "list")
	want := []string{"mox-core"}
	if o.WithDev {
		want = append(want, "mox-dev")
	}
	for _, p := range want {
		if !strings.Contains(installed, p+"@mox  installed") && !strings.Contains(installed, p+"@mox installed") {
			if _, err := o.codex("plugin", "add", p+"@mox"); err != nil {
				return rep, fmt.Errorf("шаг «плагины»: %w", err)
			}
		}
		rep.Plugins = append(rep.Plugins, p)
	}
	o.Log("→ правила команды → " + filepath.Join(o.CodexHome, "AGENTS.md"))
	body, err := os.ReadFile(filepath.Join(rep.Root, "AGENTS.md"))
	if err != nil {
		return rep, fmt.Errorf("шаг «правила»: %w", err)
	}
	if err := ApplyAgentsBlock(filepath.Join(o.CodexHome, "AGENTS.md"), string(body), rep.Version); err != nil {
		return rep, fmt.Errorf("шаг «правила»: %w", err)
	}
	if o.Name != "" || o.Email != "" {
		o.Log("→ сотрудник → ~/.mox/employee")
		if err := os.MkdirAll(filepath.Join(o.Home, ".mox"), 0o700); err != nil {
			return rep, err
		}
		if err := os.WriteFile(filepath.Join(o.Home, ".mox", "employee"), []byte("name="+o.Name+"\nemail="+o.Email+"\n"), 0o600); err != nil {
			return rep, fmt.Errorf("шаг «сотрудник»: %w", err)
		}
	}
	o.Log("→ хук классов в репо ~/MOX/projects")
	hooks := filepath.Join(rep.Root, "hooks")
	dirs, _ := filepath.Glob(filepath.Join(o.Home, "MOX", "projects", "*", ".git"))
	for _, g := range dirs {
		repo := filepath.Dir(g)
		if err := exec.Command("git", "-C", repo, "config", "core.hooksPath", hooks).Run(); err == nil {
			rep.Repos++
		}
	}
	o.Log(fmt.Sprintf("✅ harness %s подключён: %s (репо с хуком: %d)", rep.Version, rep.Root, rep.Repos))
	return rep, nil
}
