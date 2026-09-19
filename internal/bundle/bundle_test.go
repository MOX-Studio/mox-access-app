package bundle

import (
	"os"
	"testing"
)

func TestParseFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/lilya.moxaccess")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if b.Employee.Email != "lilya@example.com" || b.Gateway.LocalPort != 8000 || b.Gateway.Origin != "https://localhost:8000" {
		t.Fatalf("bad bundle: %+v", b)
	}
	if b.Relay == nil || b.Relay.Port != 2222 || b.Relay.User != "moxrelay" || b.Relay.Host != "relay.example" {
		t.Fatalf("bad relay: %+v", b.Relay)
	}
	if b.TLS == nil || b.TLS.SHA1 != "43C37FF6EE7E86B4BFB451FE0AB440E74305C585" {
		t.Fatalf("bad tls: %+v", b.TLS)
	}
	if b.AccessToken() != "access.token.value" {
		t.Fatal("token")
	}
	if v, ok := b.Config.Features["apps"]; !ok || v { // apps must be present and false
		t.Fatalf("features: %v", b.Config.Features)
	}
	env := b.EnvVars("/Users/x/.codex/mox-access.pem")
	if env["CODEX_CA_CERTIFICATE"] != "/Users/x/.codex/mox-access.pem" || env["NO_PROXY"] != "localhost,127.0.0.1,::1" || env["no_proxy"] != env["NO_PROXY"] || env["CODEX_API_BASE_URL"] != "https://localhost:8000/backend-api" {
		t.Fatalf("env: %v", env)
	}
}

func TestParseRejects(t *testing.T) {
	good, _ := os.ReadFile("testdata/lilya.moxaccess")
	cases := map[string]string{
		"empty":         `{}`,
		"wrong format":  `{"format":"moxaccess/2"}`,
		"bad relay key": string(good[:len(good)-1]) + `,"relay":{"user":"u","host":"h","port":22,"hostKey":"ssh-ed25519 AAAA","privateKey":"not a key"}}`,
		"bad cert":      string(good[:len(good)-1]) + `,"tls":{"certificate":"-----BEGIN CERTIFICATE-----\nAAAA'$(rm -rf /)\n-----END CERTIFICATE-----\n","sha1":"x"}}`,
		"no email":      string(good[:len(good)-1]) + `,"employee":{"name":"x","email":""}}`,
	}
	for name, s := range cases {
		if _, err := Parse([]byte(s)); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}
