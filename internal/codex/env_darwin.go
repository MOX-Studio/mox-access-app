//go:build darwin

package codex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const envLabel = "ru.mox.access.env"

func envPlistPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", envLabel+".plist")
}

// renderEnvPlist is the LaunchAgent that re-applies the variables at every login: Codex Desktop is a GUI application and
// reads its environment from launchd, never from a shell profile. launchd expands nothing, so values are final paths.
func renderEnvPlist(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cmds := make([]string, 0, len(keys))
	for _, k := range keys {
		cmds = append(cmds, "launchctl setenv "+k+" "+shellQuote(vars[k]))
	}
	return strings.Join([]string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		`<plist version="1.0">`, `<dict>`,
		`  <key>Label</key><string>` + envLabel + `</string>`,
		`  <key>ProgramArguments</key>`,
		`  <array>`, `    <string>/bin/sh</string>`, `    <string>-c</string>`,
		`    <string>` + xmlEscape(strings.Join(cmds, "; ")) + `</string>`,
		`  </array>`,
		`  <key>RunAtLoad</key><true/>`, `</dict>`, `</plist>`, ``,
	}, "\n")
}

// shellQuote wraps a value in single quotes for /bin/sh; a quote inside becomes '\”.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetEnv applies the variables to the running launchd session now and installs the agent for the next logins.
func SetEnv(vars map[string]string) error {
	for _, k := range sortedKeys(vars) {
		if err := run("launchctl", "setenv", k, vars[k]); err != nil {
			return err
		}
	}
	path := envPlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(renderEnvPlist(vars)), 0o644); err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	_ = run("launchctl", "unload", path)
	return run("launchctl", "load", path)
}

// UnsetEnv removes the variables from the session and the agent from disk.
func UnsetEnv(keys []string) error {
	for _, k := range keys {
		_ = run("launchctl", "unsetenv", k)
	}
	path := envPlistPath()
	_ = run("launchctl", "unload", path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
