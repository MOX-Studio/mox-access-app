//go:build darwin

package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Target is the .app bundle the running binary lives in; empty for a bare binary (a development run).
func Target(exe string) string {
	marker := ".app" + string(filepath.Separator) + "Contents" + string(filepath.Separator) + "MacOS" + string(filepath.Separator)
	i := strings.LastIndex(exe, marker)
	if i < 0 {
		return ""
	}
	return exe[:i+len(".app")]
}

// prepare drops the quarantine flag so Gatekeeper does not hold the new copy: the download came from our own process,
// which normally sets none, but a copy that carries one would otherwise wait for "Open Anyway".
func prepare(staged string) error {
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", staged).Run()
	return nil
}

// Starter runs the new bundle's binary directly, as the LaunchAgent does, detached from the quitting process.
func Starter(args []string) func(target string) error {
	return func(target string) error {
		cmd := exec.Command(filepath.Join(target, "Contents", "MacOS", "moxaccess"), args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
}

// WaitExit returns once process pid is gone or the timeout passes.
func WaitExit(pid int, timeout time.Duration) {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		p, err := os.FindProcess(pid)
		if err != nil || p.Signal(syscall.Signal(0)) != nil {
			return
		}
	}
}

// ManualStart is what the employee does when the new copy did not start by itself.
const ManualStart = "Новая версия MOX Access установлена, но macOS не дала ей запуститься сама.\n\n" +
	"1. Открой «Программы» и запусти MOX Access.\n" +
	"2. Если macOS пишет, что не может проверить разработчика: Системные настройки → Конфиденциальность и безопасность → " +
	"внизу «Всё равно открыть» для MOX Access → подтверди паролем.\n" +
	"3. Запусти MOX Access ещё раз: кольцо вернётся, рабочий режим и туннель включатся сами.\n\n" +
	"Настройки безопасности сейчас откроются."

// OpenManualStart opens the pane with "Open Anyway".
func OpenManualStart() {
	_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?General").Start()
}
