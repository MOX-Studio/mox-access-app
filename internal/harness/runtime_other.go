//go:build !windows

package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const codexExe = "codex"

const pathMark = "__MOX_PATH__"

// EnsureRuntime on macOS only reports what is missing: Node's installer asks for the employee's password, so the
// studio account walks them through it.
func EnsureRuntime(log func(string)) []string {
	return missingRuntime(loginPath())
}

func missingRuntime(path string) []string {
	var notes []string
	for _, t := range []struct{ exe, name string }{
		{"npx", "Node.js: MCP context7, playwright, chrome-devtools не запустятся (установщик LTS с nodejs.org)"},
		{"uvx", "uv: MCP fetch не запустится (curl -LsSf https://astral.sh/uv/install.sh | sh)"},
		{"python3", "Python: скилл end и хук классов не запустятся"},
		{"bash", "bash"},
	} {
		if !inPath(path, t.exe) {
			notes = append(notes, "нет "+t.name)
		}
	}
	return notes
}

// loginPath is the PATH of the user's login shell. The application starts from Finder with the launchd PATH
// (/usr/bin:/bin:/usr/sbin:/sbin), without /usr/local/bin (Node .pkg), /opt/homebrew/bin and ~/.local/bin (uv),
// while Codex starts its MCP servers with the login shell's PATH. The marker keeps profile output out of the
// answer; a shell that fails or hangs leaves the PATH of the process.
func loginPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lc", `printf '`+pathMark+`%s' "$PATH"`)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if i := strings.LastIndex(string(out), pathMark); err == nil && i >= 0 {
		return string(out[i+len(pathMark):])
	}
	return os.Getenv("PATH")
}

func inPath(path, exe string) bool {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, exe)); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
