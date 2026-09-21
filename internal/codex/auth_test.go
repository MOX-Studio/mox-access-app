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

// After the shell installer of 2026-09-18 auth.json already holds a MOX login and the personal one sits in the
// installer's backup: InstallAuth takes the newest backup that is not a MOX login, so Disable brings it back.
func TestInstallAuthRecoversPersonalLoginFromInstallerBackup(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "auth.json"), []byte(moxAuth), 0o600)
	os.WriteFile(filepath.Join(home, "auth.json.bak-mox-20260917-100000"), []byte(`{"tokens":{"access_token":"older.personal.x"}}`), 0o600)
	os.WriteFile(filepath.Join(home, "auth.json.bak-mox-20260918-153000"), []byte(personalAuth), 0o600)
	if err := InstallAuth(home, []byte(moxAuth)); err != nil {
		t.Fatal(err)
	}
	personal, err := os.ReadFile(filepath.Join(home, "auth.json.personal"))
	if err != nil || string(personal) != personalAuth {
		t.Fatalf("personal = %q, %v", personal, err)
	}
	if err := RestoreAuth(home); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	if string(got) != personalAuth {
		t.Fatal("personal login not restored")
	}
}

// A MOX login with no personal backup anywhere leaves no .personal: Restore then shows the sign-in screen.
func TestInstallAuthWithoutAnyPersonalLogin(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "auth.json"), []byte(moxAuth), 0o600)
	if err := InstallAuth(home, []byte(moxAuth)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json.personal")); !os.IsNotExist(err) {
		t.Fatal("a MOX login must not become the personal one")
	}
}
