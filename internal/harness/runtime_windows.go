//go:build windows

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const codexExe = "codex.exe"

// EnsureRuntime puts on the machine what the set of the team runs on and Windows lacks out of the box: Node (npx) for
// the MCP servers, uv (uvx) for the fetch server, Python for the end skill and the class hook — through winget, the
// package manager Windows 10 1809+ and 11 carry — and bash from Git for Windows on the user's Path, for the skill
// scripts. Nothing is reinstalled; each note says what was done or what is still missing.
func EnsureRuntime(log func(string)) []string {
	var notes []string
	if p := gitBashDir(); p != "" {
		if err := prependUserPath(p); err != nil {
			notes = append(notes, "bash (Git for Windows) не добавлен в Path: "+err.Error())
		} else {
			notes = append(notes, "bash: "+p)
		}
	} else {
		notes = append(notes, "bash не найден: скилл new-project и end не запустятся — установите Git for Windows (кнопка «Войти в GitHub» ставит его)")
	}
	for _, t := range []struct{ exe, id, name string }{
		{"npx.cmd", "OpenJS.NodeJS.LTS", "Node.js (MCP context7, playwright, chrome-devtools)"},
		{"uvx.exe", "astral-sh.uv", "uv (MCP fetch)"},
		{"python.exe", "Python.Python.3.12", "Python (скилл end, хук классов)"},
	} {
		if _, err := exec.LookPath(t.exe); err == nil {
			continue
		}
		if log != nil {
			log("→ winget: " + t.name)
		}
		if err := winget(t.id); err != nil {
			notes = append(notes, t.name+" не установлен: "+err.Error())
		} else {
			notes = append(notes, t.name+" установлен (действует в новых окнах Codex)")
		}
	}
	return notes
}

// gitBashDir is where Git for Windows keeps bash.exe; empty when Git is not installed.
func gitBashDir() string {
	for _, d := range []string{`C:\Program Files\Git\bin`, filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin")} {
		if _, err := os.Stat(filepath.Join(d, "bash.exe")); err == nil {
			return d
		}
	}
	return ""
}

func winget(id string) error {
	if _, err := exec.LookPath("winget.exe"); err != nil {
		return os.ErrNotExist
	}
	cmd := exec.Command("winget", "install", "--id", id, "-e", "--source", "winget", "--silent", "--accept-source-agreements", "--accept-package-agreements")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return &installError{strings.TrimSpace(string(out))}
	}
	return nil
}

type installError struct{ out string }

func (e *installError) Error() string {
	lines := strings.Split(e.out, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// prependUserPath adds dir to the user's Path (registry + change broadcast), once.
func prependUserPath(dir string) error {
	script := "$dir = '" + strings.ReplaceAll(dir, "'", "''") + "'\n" +
		"$p = [Environment]::GetEnvironmentVariable('Path', 'User')\n" +
		"if (($p -split ';') -contains $dir) { exit 0 }\n" +
		"[Environment]::SetEnvironmentVariable('Path', ($dir + ';' + $p).TrimEnd(';'), 'User')\n"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeUTF16Base64(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return &installError{strings.TrimSpace(string(out))}
	}
	return nil
}
