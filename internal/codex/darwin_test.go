//go:build darwin

package codex

import (
	"strings"
	"testing"
)

func TestRenderEnvPlist(t *testing.T) {
	out := renderEnvPlist(map[string]string{"CODEX_API_BASE_URL": "https://localhost:8000/backend-api", "NO_PROXY": "localhost,127.0.0.1,::1"})
	for _, want := range []string{"<key>Label</key><string>ru.mox.access.env</string>", "launchctl setenv CODEX_API_BASE_URL 'https://localhost:8000/backend-api'", "launchctl setenv NO_PROXY 'localhost,127.0.0.1,::1'", "<key>RunAtLoad</key><true/>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$HOME") {
		t.Error("launchd expands nothing: the plist must carry resolved paths only")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a'b; rm -rf /"); got != `'a'\''b; rm -rf /'` {
		t.Fatalf("quote: %s", got)
	}
}

func TestSetenvCommandIsSorted(t *testing.T) {
	a := renderEnvPlist(map[string]string{"B": "2", "A": "1"})
	b := renderEnvPlist(map[string]string{"A": "1", "B": "2"})
	if a != b {
		t.Error("plist must not depend on map order")
	}
}
