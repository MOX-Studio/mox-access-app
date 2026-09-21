//go:build !windows

package harness

import "os/exec"

const codexExe = "codex"

// EnsureRuntime on macOS only reports what is missing: Node and uv are Homebrew territory, which needs the employee at
// the terminal — the studio account tells them what to install.
func EnsureRuntime(log func(string)) []string {
	var notes []string
	for _, t := range []struct{ exe, name string }{
		{"npx", "Node.js: MCP context7, playwright, chrome-devtools не запустятся (brew install node)"},
		{"uvx", "uv: MCP fetch не запустится (brew install uv)"},
		{"python3", "Python: скилл end и хук классов не запустятся"},
		{"bash", "bash"},
	} {
		if _, err := exec.LookPath(t.exe); err != nil {
			notes = append(notes, "нет "+t.name)
		}
	}
	return notes
}
