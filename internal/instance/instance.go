// Package instance keeps one MOX Access per user: a second copy would find the tunnel port taken and sit there
// showing "no connection" (Dinara, 2026-10-06, the old copy still running next to the new one).
package instance

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrRunning: another copy holds the lock.
var ErrRunning = errors.New("MOX Access уже запущен")

// held keeps the lock file open for the life of the process; the system drops the lock when the process ends.
var held *os.File

// Acquire takes the per-user lock in dir, or returns ErrRunning.
func Acquire(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := lock(f); err != nil {
		f.Close()
		return err
	}
	held = f
	return nil
}
