package instance

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

// The lock is per process, so the second copy is a real second process: this test binary re-run as a helper.
func TestSecondCopyIsRefused(t *testing.T) {
	if dir := os.Getenv("INSTANCE_HELPER_DIR"); dir != "" {
		if err := Acquire(dir); errors.Is(err, ErrRunning) {
			os.Exit(3)
		} else if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	if err := Acquire(dir); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestSecondCopyIsRefused")
	cmd.Env = append(os.Environ(), "INSTANCE_HELPER_DIR="+dir)
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("second copy: %v", err)
	}
	held.Close()
	held = nil
	cmd = exec.Command(os.Args[0], "-test.run=TestSecondCopyIsRefused")
	cmd.Env = append(os.Environ(), "INSTANCE_HELPER_DIR="+dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("after the first copy is gone the lock must be free: %v", err)
	}
}
