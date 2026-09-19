// Package codex touches the Codex Desktop installation of the employee: its home directory (auth.json, config.toml),
// the environment of the shell, the trust of the gateway certificate and the application itself. The cross-platform
// parts live here; the OS-specific ones in *_darwin.go / *_windows.go.
package codex

import (
	"os"
	"path/filepath"
)

// Home is ~/.codex — the Desktop shell reads CODEX_HOME only for its own partition, so the application never sets it.
func Home() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".codex") }
