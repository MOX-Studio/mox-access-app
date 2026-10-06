//go:build darwin

package codex

// Darwin is the macOS implementation of the OS-level steps the application takes (app.CodexOps).
type Darwin struct{}

func (Darwin) Home() string                        { return Home() }
func (Darwin) TrustCert(pemPath string) error      { return TrustCert(pemPath) }
func (Darwin) SetEnv(vars map[string]string) error { return SetEnv(vars) }
func (Darwin) UnsetEnv(keys []string) error        { return UnsetEnv(keys) }
func (Darwin) QuitCodex() error                    { return QuitCodex() }
func (Darwin) LaunchCodex() error                  { return LaunchCodex() }
func (Darwin) CodexRunning() bool                  { return CodexRunning() }

// SessionReady is always true on macOS: the variables come from launchd (the env LaunchAgent at login) and LaunchCodex
// strips the caller's own environment.
func (Darwin) SessionReady(map[string]string) bool { return true }
func (Darwin) RestartHint() string                 { return "" }
