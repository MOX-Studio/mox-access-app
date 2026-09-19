package codex

import (
	"strings"
	"testing"
)

const fragment = "# MOX ACCESS\nchatgpt_base_url = \"https://localhost:8000/backend-api\"\nopenai_base_url = \"https://localhost:8000/backend-api/codex\"\nexperimental_realtime_ws_base_url = \"https://localhost:8000/openai/v1/live\"\ncli_auth_credentials_store = \"file\"\n"

func TestApplyConfig(t *testing.T) {
	cases := []struct {
		name, in string
		install  bool
		want     []string // substrings that must be present
		absent   []string
	}{
		{"empty install", "", true, []string{fragment, "[features]\napps = false # MOX ACCESS"}, nil},
		{"keeps user keys and sections", "model = \"gpt-5\"\n\n[projects.\"/Users/l/x\"]\ntrust_level = \"trusted\"\n", true,
			[]string{"model = \"gpt-5\"", "[projects.\"/Users/l/x\"]\ntrust_level = \"trusted\"", "apps = false # MOX ACCESS"}, nil},
		{"idempotent", fragment + "model = \"gpt-5\"\n\n[features]\napps = false # MOX ACCESS\n", true, []string{"model = \"gpt-5\""}, nil},
		{"existing features keeps other keys", "[features]\nmemories = true\napps = true\n", true, []string{"memories = true", "apps = false # MOX ACCESS"}, []string{"apps = true"}},
		{"old mox provider goes", "model_provider = \"mox\"\n\n[model_providers.mox]\nname = \"x\"\n\n[features]\nmemories = true\n", true, []string{"memories = true"}, []string{"model_providers.mox", "model_provider = \"mox\""}},
		{"remove leaves other features", fragment + "model = \"gpt-5\"\n\n[features]\nmemories = true\napps = false # MOX ACCESS\n", false, []string{"model = \"gpt-5\"", "[features]\nmemories = true"}, []string{"MOX ACCESS", "chatgpt_base_url"}},
		{"remove drops empty features", fragment + "model = \"gpt-5\"\n\n[features]\napps = false # MOX ACCESS\n", false, []string{"model = \"gpt-5\""}, []string{"[features]", "apps"}},
		{"remove keeps user apps line", "[features]\napps = true\n", false, []string{"apps = true"}, nil},
	}
	for _, c := range cases {
		out := ApplyConfig(c.in, fragment, c.install)
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q in:\n%s", c.name, w, out)
			}
		}
		for _, a := range c.absent {
			if strings.Contains(out, a) {
				t.Errorf("%s: unexpected %q in:\n%s", c.name, a, out)
			}
		}
		if c.install && strings.Count(out, "apps = false # MOX ACCESS") != 1 {
			t.Errorf("%s: apps line count != 1:\n%s", c.name, out)
		}
		if c.install && !strings.HasPrefix(out, "# MOX ACCESS\n") {
			t.Errorf("%s: fragment must lead:\n%s", c.name, out)
		}
		if c.install && ApplyConfig(out, fragment, true) != out {
			t.Errorf("%s: second install changed the file", c.name)
		}
	}
}
