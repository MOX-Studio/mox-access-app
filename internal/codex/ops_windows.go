//go:build windows

package codex

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Windows is the macOS Darwin's counterpart: the same steps the PowerShell installer of 2026-09-18 performed, run from
// the application. What differs is the shell: ChatGPT/Codex on Windows is a packaged (MSIX) application that reads its
// environment at logon, so the last step is a logout, not a relaunch — until spike S3 finds a way around it.
type Windows struct{}

// powershell runs a script without a window (the application itself has no console) and returns its output.
func powershell(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("powershell: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (Windows) Home() string { return Home() }

// TrustCert adds the gateway certificate to the user's Root store, where Chromium looks. Windows confirms a root with
// its own dialog; a thumbprint already present asks nothing, and a refused dialog is reported as the missing trust.
func (Windows) TrustCert(pemPath string) error {
	thumb, err := Thumbprint(pemPath)
	if err != nil {
		return err
	}
	script := "$thumb = " + PSQuote(thumb) + "\n" +
		"if (Get-ChildItem Cert:\\CurrentUser\\Root | Where-Object Thumbprint -eq $thumb) { exit 0 }\n" +
		"Import-Certificate -FilePath " + PSQuote(pemPath) + " -CertStoreLocation Cert:\\CurrentUser\\Root | Out-Null\n" +
		"if (Get-ChildItem Cert:\\CurrentUser\\Root | Where-Object Thumbprint -eq $thumb) { exit 0 } else { exit 3 }\n"
	if _, err := powershell(script); err != nil {
		return errors.New("доверие сертификату шлюза не установлено: подтвердите добавление сертификата «localhost» в диалоге Windows и повторите")
	}
	return nil
}

// SetEnv writes the variables for the user: the registry plus the change broadcast, which is what
// [Environment]::SetEnvironmentVariable does. The lowercase no_proxy stays out, as in the installer: the engine on
// Windows reads the proxy from the registry and NO_PROXY is the name it honors.
func (Windows) SetEnv(vars map[string]string) error {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		if k != "no_proxy" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	_, err := powershell(EnvScript(vars, keys, false))
	return err
}

func (Windows) UnsetEnv(keys []string) error {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	_, err := powershell(EnvScript(nil, sorted, true))
	return err
}

// QuitCodex stops every process of the OpenAI.Codex package, whatever its executable is called; the point is that the
// next start reads auth.json and config.toml afresh.
func (Windows) QuitCodex() error {
	stop := "Get-Process | Where-Object { $_.Path -like '*\\WindowsApps\\OpenAI.Codex_*' } | Stop-Process -Force -ErrorAction SilentlyContinue"
	if _, err := powershell(stop); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := powershell("(Get-Process | Where-Object { $_.Path -like '*\\WindowsApps\\OpenAI.Codex_*' } | Measure-Object).Count")
		if strings.TrimSpace(out) == "0" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// LaunchCodex does not start the application: a packaged app started now would still carry the environment of the
// logon and show the previous account (the Windows lesson of 2026-09-18). The employee logs out and in; RestartHint says so.
func (Windows) LaunchCodex() error { return nil }

func (Windows) RestartHint() string {
	return "Выйдите из Windows и войдите снова (или перезагрузите), затем откройте «ChatGPT» из Пуска: приложение из пакета читает переменные окружения только при входе в систему."
}
