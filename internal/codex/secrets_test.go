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
