//go:build darwin

package codex

import (
	"fmt"
	"os"
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

// LaunchCodex starts ChatGPT.app with an environment stripped of every variable the login manages: `open` passes the
// caller's environment to the application, and a stale CODEX_API_BASE_URL sends the engine to a gateway that is not
// there (the sign-in screen of 2026-09-19). What the shell must see comes from launchd (SetEnv), not from here.
func LaunchCodex() error {
	cmd := exec.Command("open", "-a", "ChatGPT")
	var env []string
	for _, kv := range os.Environ() {
		name := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(name, "CODEX_") || name == "NO_PROXY" || name == "no_proxy" || name == "NODE_EXTRA_CA_CERTS" {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("open -a ChatGPT: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
