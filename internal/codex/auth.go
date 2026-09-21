package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// IsMoxAuth tells a login MOX issued from a personal one: the access_token of a MOX login is a JWT with a mox.kid claim.
func IsMoxAuth(raw []byte) bool {
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return false
	}
	parts := strings.Split(auth.Tokens.AccessToken, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Mox *struct {
			Kid string `json:"kid"`
		} `json:"mox"`
	}
	return json.Unmarshal(payload, &claims) == nil && claims.Mox != nil && claims.Mox.Kid != ""
}

// InstallAuth writes the MOX login as auth.json. A personal login found there moves to auth.json.personal — once:
// a .personal that already exists is the employee's own and is never overwritten. Every replaced file also gets a
// timestamped backup, like the shell installer made.
func InstallAuth(home string, mox []byte) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	path := filepath.Join(home, "auth.json")
	personal := filepath.Join(home, "auth.json.personal")
	if current, err := os.ReadFile(path); err == nil {
		stamp := time.Now().Format("20060102-150405")
		if err := os.WriteFile(path+".bak-mox-"+stamp, current, 0o600); err != nil {
			return fmt.Errorf("бэкап auth.json: %w", err)
		}
		if _, err := os.Stat(personal); os.IsNotExist(err) {
			// The login found there is the employee's own — or, after the shell installer of 2026-09-18, the MOX login it
			// wrote, with the personal one in the installer's own backup: take the newest backup that is not a MOX login.
			own := current
			if IsMoxAuth(current) {
				own = personalFromBackups(home)
			}
			if own != nil {
				if err := os.WriteFile(personal, own, 0o600); err != nil {
					return fmt.Errorf("сохранение личного входа: %w", err)
				}
			}
		}
	}
	if err := os.WriteFile(path, mox, 0o600); err != nil {
		return fmt.Errorf("запись auth.json: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// RestoreAuth brings the personal login back (auth.json.personal → auth.json) or, when there was none, removes the
// MOX login so Codex shows its own sign-in screen.
func RestoreAuth(home string) error {
	path := filepath.Join(home, "auth.json")
	personal := filepath.Join(home, "auth.json.personal")
	if data, err := os.ReadFile(personal); err == nil {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return fmt.Errorf("возврат личного входа: %w", err)
		}
		return os.Remove(personal)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// personalFromBackups returns the newest auth.json.bak-mox-* that holds a personal login, or nil.
func personalFromBackups(home string) []byte {
	matches, _ := filepath.Glob(filepath.Join(home, "auth.json.bak-mox-*"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches))) // the stamp sorts chronologically
	for _, m := range matches {
		if data, err := os.ReadFile(m); err == nil && len(data) > 0 && !IsMoxAuth(data) {
			return data
		}
	}
	return nil
}
