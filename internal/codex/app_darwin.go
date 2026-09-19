//go:build darwin

package codex

import (
	"os/exec"
	"strings"
	"time"
)

// CodexRunning reports whether ChatGPT.app (which hosts Codex) is running.
func CodexRunning() bool {
	out, _ := exec.Command("pgrep", "-x", "ChatGPT").Output()
	return strings.TrimSpace(string(out)) != ""
}

// QuitCodex asks the application to quit and waits up to ten seconds; a stubborn process is killed, since the point is
// a full restart that re-reads auth.json, config.toml and the environment.
func QuitCodex() error {
	if !CodexRunning() {
		return nil
	}
	_ = exec.Command("osascript", "-e", `tell application "ChatGPT" to quit`).Run()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !CodexRunning() {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-x", "ChatGPT").Run()
	time.Sleep(time.Second)
	return nil
}

func LaunchCodex() error { return run("open", "-a", "ChatGPT") }
