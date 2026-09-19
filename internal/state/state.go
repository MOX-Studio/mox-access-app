// Package state is the small JSON file the application keeps about itself: which login is active, who the employee
// is, when the bundle was imported, which harness version was applied. Nothing secret lives here.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	ModePersonal  = "personal"
	ModeCorporate = "corporate"
)

type State struct {
	Mode           string    `json:"mode"`
	Employee       string    `json:"employee,omitempty"`
	ImportedAt     time.Time `json:"importedAt,omitempty"`
	HarnessVersion string    `json:"harnessVersion,omitempty"`
	LastCheck      time.Time `json:"lastCheck,omitempty"`
	LastCheckText  string    `json:"lastCheckText,omitempty"`
}

func Load(dir string) State {
	s := State{Mode: ModePersonal}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.Mode == "" {
		s.Mode = ModePersonal
	}
	return s
}

func Save(dir string, s State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", " ")
	return os.WriteFile(filepath.Join(dir, "state.json"), data, 0o600)
}
