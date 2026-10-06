//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Target is the running executable itself: Windows lets a running .exe be renamed, not overwritten.
func Target(exe string) string { return exe }

func prepare(string) error { return nil }

// Starter runs the new executable detached from the quitting process.
func Starter(args []string) func(target string) error {
	return func(target string) error {
		cmd := exec.Command(target, args...)
		const detached, newGroup = 0x00000008, 0x00000200 // DETACHED_PROCESS, CREATE_NEW_PROCESS_GROUP
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detached | newGroup}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
}

// WaitExit returns once process pid is gone or the timeout passes.
func WaitExit(pid int, timeout time.Duration) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// ManualStart is what the employee does when the new copy did not start by itself.
const ManualStart = "Новая версия MOX Access установлена, но не запустилась сама.\n\n" +
	"Запусти MOX Access из той же папки, что и раньше. Если Windows покажет синее окно SmartScreen: " +
	"«Подробнее» → «Выполнить в любом случае». Кольцо вернётся, рабочий режим и туннель включатся сами."

func OpenManualStart() {}
