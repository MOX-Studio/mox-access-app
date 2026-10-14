package codex

import (
	"strings"
	"testing"
)

var tilda = Secret{Name: "tilda", Kind: "mcp_http", URL: "https://tilda.ru/api/mcp/", Value: "tilda-secret-value-0001", Version: 1}

func TestApplySecretsAddsAndRemovesOnlyItsBlocks(t *testing.T) {
	user := "model = \"gpt\"\n\n[mcp_servers.github]\ncommand = \"gh\"\n"
	got, skipped := ApplySecrets(user, []Secret{tilda})
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	want := user + "\n[mcp_servers.tilda] # MOX ACCESS secret\nurl = \"https://tilda.ru/api/mcp/\"\nhttp_headers = { Authorization = \"Bearer tilda-secret-value-0001\" }\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if again, _ := ApplySecrets(got, []Secret{tilda}); again != got {
		t.Fatalf("not idempotent:\n%q", again)
	}
	if back, _ := ApplySecrets(got, nil); back != user {
		t.Fatalf("removal left\n%q", back)
	}
}

func TestApplySecretsReplacesAChangedValueAndKeepsWhatFollows(t *testing.T) {
	first, _ := ApplySecrets("[features]\napps = false # MOX ACCESS\n", []Secret{tilda})
	first += "\n[projects.\"/x\"]\ntrust_level = \"trusted\"\n"
	next := tilda
	next.Value, next.Version = "tilda-secret-value-0002", 2
	got, _ := ApplySecrets(first, []Secret{next})
	if strings.Contains(got, "0001") || !strings.Contains(got, "Bearer tilda-secret-value-0002") {
		t.Fatalf("value not replaced:\n%s", got)
	}
	if !strings.Contains(got, "[projects.\"/x\"]\ntrust_level = \"trusted\"") || !strings.Contains(got, "apps = false # MOX ACCESS") {
		t.Fatalf("other sections lost:\n%s", got)
	}
}

func TestApplySecretsLeavesAServerTheEmployeeConfiguredHerself(t *testing.T) {
	own := "[mcp_servers.tilda]\nurl = \"https://tilda.ru/api/mcp/\"\nbearer_token_env_var = \"MY_TILDA\"\n"
	got, skipped := ApplySecrets(own, []Secret{tilda})
	if got != own || len(skipped) != 1 || skipped[0] != "tilda" {
		t.Fatalf("own server touched: %q %v", got, skipped)
	}
}

func TestApplySecretsDropsWhatCannotBeWrittenSafely(t *testing.T) {
	for _, bad := range []Secret{
		{Name: "Tilda MCP", Kind: "mcp_http", URL: tilda.URL, Value: tilda.Value},
		{Name: "tilda", Kind: "env", URL: tilda.URL, Value: tilda.Value},
		{Name: "tilda", Kind: "mcp_http", URL: "http://tilda.ru", Value: tilda.Value},
		{Name: "tilda", Kind: "mcp_http", URL: tilda.URL, Value: "with\"quote-0000000"},
		{Name: "tilda", Kind: "mcp_http", URL: tilda.URL, Value: "line\nbreak-00000000"},
	} {
		got, skipped := ApplySecrets("", []Secret{bad})
		if got != "" || len(skipped) != 1 {
			t.Fatalf("%+v written: %q", bad, got)
		}
	}
}

func TestWithoutSecretsKeepsValuesOutOfBackups(t *testing.T) {
	text, _ := ApplySecrets("model = \"gpt\"\n", []Secret{tilda})
	if strings.Contains(WithoutSecrets(text), tilda.Value) {
		t.Fatal("value survived")
	}
}

var gcore = Secret{Name: "GCORE_API_TOKEN", Kind: "env", Value: "12345$gcore-secret-value-0003", Version: 1}

const envLine = `GCORE_API_TOKEN = "12345$gcore-secret-value-0003" # MOX ACCESS secret`

func TestApplySecretsWritesAVariableTheAgentShellSees(t *testing.T) {
	user := "model = \"gpt\"\n\n[shell_environment_policy]\ninherit = \"core\"\n"
	got, skipped := ApplySecrets(user, []Secret{tilda, gcore})
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	want := user + "\n[mcp_servers.tilda] # MOX ACCESS secret\nurl = \"https://tilda.ru/api/mcp/\"\nhttp_headers = { Authorization = \"Bearer tilda-secret-value-0001\" }\n" +
		"\n[shell_environment_policy.set] # MOX ACCESS secret\n" + envLine + "\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if again, _ := ApplySecrets(got, []Secret{tilda, gcore}); again != got {
		t.Fatalf("not idempotent:\n%q", again)
	}
	if back, _ := ApplySecrets(got, []Secret{tilda}); strings.Contains(back, "GCORE") || strings.Contains(back, "shell_environment_policy.set") {
		t.Fatalf("withdrawn variable stayed:\n%s", back)
	}
	if back, _ := ApplySecrets(got, nil); back != user {
		t.Fatalf("removal left\n%q", back)
	}
	if strings.Contains(WithoutSecrets(got), gcore.Value) {
		t.Fatal("value survived in a backup")
	}
}

// Denis keeps his own variables there: a second [shell_environment_policy.set] table would make the whole config
// unreadable to Codex, so the MOX lines go into hers and come out of it again, her lines untouched.
func TestApplySecretsAddsVariablesToTheEmployeesOwnSetTable(t *testing.T) {
	user := "[shell_environment_policy]\ninherit = \"core\"\n\n[shell_environment_policy.set]\nNOTION_TOKEN = \"mine\"\ngcore_api_token = \"mine\"\n\n[features]\napps = false\n"
	other := Secret{Name: "GCORE_REGION", Kind: "env", Value: "region-value-00000001", Version: 1}
	got, skipped := ApplySecrets(user, []Secret{gcore, other})
	if len(skipped) != 1 || skipped[0] != "GCORE_API_TOKEN" {
		t.Fatalf("her own variable of that name must win: skipped %v\n%s", skipped, got)
	}
	want := "[shell_environment_policy]\ninherit = \"core\"\n\n[shell_environment_policy.set]\n" +
		`GCORE_REGION = "region-value-00000001" # MOX ACCESS secret` + "\nNOTION_TOKEN = \"mine\"\ngcore_api_token = \"mine\"\n\n[features]\napps = false\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "[shell_environment_policy.set]") != 1 {
		t.Fatal("second set table")
	}
	if again, _ := ApplySecrets(got, []Secret{gcore, other}); again != got {
		t.Fatalf("not idempotent:\n%q", again)
	}
	if back, _ := ApplySecrets(got, nil); back != user {
		t.Fatalf("removal left\n%q", back)
	}
}

func TestApplySecretsLeavesASetItCannotExtend(t *testing.T) {
	for _, user := range []string{
		"[shell_environment_policy]\nset = { A = \"1\" }\n",
		"[shell_environment_policy]\nset.A = \"1\"\n",
		"shell_environment_policy = { inherit = \"core\" }\n",
		"shell_environment_policy.set.A = \"1\"\n",
	} {
		got, skipped := ApplySecrets(user, []Secret{tilda, gcore})
		if strings.Contains(got, "GCORE") || len(skipped) != 1 || skipped[0] != "GCORE_API_TOKEN" || !strings.Contains(got, "[mcp_servers.tilda]") {
			t.Fatalf("%q: %v\n%s", user, skipped, got)
		}
	}
	// A dotted key at the top level does not stop a [shell_environment_policy.set] table.
	if got, skipped := ApplySecrets("shell_environment_policy.inherit = \"core\"\n", []Secret{gcore}); len(skipped) != 0 || !strings.Contains(got, envLine) {
		t.Fatalf("%v\n%s", skipped, got)
	}
}

func TestApplySecretsDropsAVariableItMayNotSet(t *testing.T) {
	for _, name := range []string{"gcore_token", "1TOKEN", "GCORE-TOKEN", "PATH", "HOME", "CODEX_HOME", "OPENAI_API_KEY"} {
		bad := gcore
		bad.Name = name
		if got, skipped := ApplySecrets("", []Secret{bad}); got != "" || len(skipped) != 1 {
			t.Fatalf("%s written: %q", name, got)
		}
	}
	bad := gcore
	bad.Value = "with\"quote-0000000"
	if got, _ := ApplySecrets("", []Secret{bad}); got != "" {
		t.Fatalf("quote written: %q", got)
	}
}
