package codex

import (
	"os"
	"path/filepath"
	"testing"
)

// A MOX access_token is a JWT whose payload carries mox.kid; the personal one carries no such claim.
const moxAuth = `{"auth_mode":"chatgpt","tokens":{"access_token":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJlIiwibW94Ijp7ImtpZCI6IjEifX0.sig","refresh_token":"r"}}`
const personalAuth = `{"auth_mode":"chatgpt","tokens":{"access_token":"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJwZXJzb25hbCJ9.sig","refresh_token":"p"}}`

func TestIsMoxAuth(t *testing.T) {
	if !IsMoxAuth([]byte(moxAuth)) || IsMoxAuth([]byte(personalAuth)) || IsMoxAuth([]byte("junk")) {
		t.Fatal("IsMoxAuth")
	}
}

func TestInstallAndRestoreAuth(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	os.WriteFile(authPath, []byte(personalAuth), 0o600)
	if err := InstallAuth(home, []byte(moxAuth)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(authPath)
	if string(got) != moxAuth {
		t.Fatal("auth.json is not the MOX login")
	}
	personal, _ := os.ReadFile(filepath.Join(home, "auth.json.personal"))
	if string(personal) != personalAuth {
		t.Fatal("personal login not preserved")
	}
	if info, _ := os.Stat(authPath); info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", info.Mode().Perm())
	}
	// A second install (new bundle) must not overwrite the preserved personal login with the MOX one.
	if err := InstallAuth(home, []byte(moxAuth)); err != nil {
		t.Fatal(err)
	}
	personal, _ = os.ReadFile(filepath.Join(home, "auth.json.personal"))
	if string(personal) != personalAuth {
		t.Fatal("personal login overwritten on reinstall")
	}
	if err := RestoreAuth(home); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(authPath)
	if string(got) != personalAuth {
		t.Fatal("restore did not bring the personal login back")
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json.personal")); !os.IsNotExist(err) {
		t.Fatal(".personal should be consumed by restore")
	}
	// No personal login at all: install then restore leaves no auth.json.
	home2 := t.TempDir()
	InstallAuth(home2, []byte(moxAuth))
	RestoreAuth(home2)
	if _, err := os.Stat(filepath.Join(home2, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("auth.json should be gone when there was no personal login")
	}
}
