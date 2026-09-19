package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/bundle"
	"github.com/MOX-Studio/mox-access-app/internal/state"
	"github.com/MOX-Studio/mox-access-app/internal/tunnel/tunneltest"
)

// fakeOps records the order of OS-level steps instead of touching the machine.
type fakeOps struct {
	home    string
	steps   []string
	failAt  string
	running bool
}

func (f *fakeOps) note(s string) error {
	f.steps = append(f.steps, s)
	if s == f.failAt {
		return errors.New("отказ " + s)
	}
	return nil
}
func (f *fakeOps) Home() string                   { return f.home }
func (f *fakeOps) TrustCert(string) error         { return f.note("cert") }
func (f *fakeOps) SetEnv(map[string]string) error { return f.note("env+") }
func (f *fakeOps) UnsetEnv([]string) error        { return f.note("env-") }
func (f *fakeOps) QuitCodex() error               { f.running = false; return f.note("quit") }
func (f *fakeOps) LaunchCodex() error             { f.running = true; return f.note("launch") }

// gateway is an https server with a self-signed localhost leaf, answering /mox/export like the real one.
func gateway(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, _ := tls.X509KeyPair(certPEM, pemKey(key))
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mox/export" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer access.token.value" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(404) // no export made for this employee yet
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	return srv, string(certPEM)
}

func pemKey(k *ecdsa.PrivateKey) []byte {
	b, _ := x509.MarshalECPrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

// testBundle is the fixture with the relay and the certificate of this test's own gateway.
func testBundle(t *testing.T, dir string, relayPort int, hostKey, privKey, certPEM string, localPort int) string {
	raw, _ := os.ReadFile("../bundle/testdata/lilya.moxaccess")
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["gateway"] = map[string]any{"origin": "https://localhost:" + itoa(localPort), "localPort": localPort}
	m["relay"] = map[string]any{"user": "moxrelay", "host": "127.0.0.1", "port": relayPort, "hostKey": hostKey, "privateKey": privKey}
	m["tls"] = map[string]any{"certificate": certPEM, "sha1": strings.Repeat("A", 40)}
	out, _ := json.Marshal(m)
	p := filepath.Join(dir, "lilya.moxaccess")
	os.WriteFile(p, out, 0o600)
	return p
}

func itoa(i int) string { return fmtInt(i) }

func TestEnableDisableRoundTrip(t *testing.T) {
	dir := t.TempDir()
	gw, certPEM := gateway(t)
	defer gw.Close()
	privPEM, userPub := tunneltest.NewUserKey(t)
	relay := tunneltest.New(t, userPub, gw.Listener.Addr().String())
	defer relay.Stop()
	ops := &fakeOps{home: filepath.Join(dir, ".codex")}
	os.MkdirAll(ops.home, 0o700)
	os.WriteFile(filepath.Join(ops.home, "auth.json"), []byte(`{"tokens":{"access_token":"personal.token.x"}}`), 0o600)
	os.WriteFile(filepath.Join(ops.home, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600)
	a, err := New(filepath.Join(dir, "app"), ops, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	local := freePort(t)
	if err := a.Import(testBundle(t, dir, relay.Port(), relay.HostKeyLine(), privPEM, certPEM, local)); err != nil {
		t.Fatal(err)
	}
	if a.Status().Mode != state.ModePersonal || a.Status().Employee != "lilya@example.com" {
		t.Fatalf("after import: %+v", a.Status())
	}
	if err := a.Enable(context.Background()); err != nil {
		t.Fatalf("enable: %v (steps %v)", err, ops.steps)
	}
	if got := strings.Join(ops.steps, " "); got != "cert env+ quit launch" {
		t.Fatalf("enable order: %s", got)
	}
	auth, _ := os.ReadFile(filepath.Join(ops.home, "auth.json"))
	if string(auth) != string(a.Bundle().Auth) {
		t.Fatal("auth.json is not the MOX login")
	}
	cfg, _ := os.ReadFile(filepath.Join(ops.home, "config.toml"))
	if !strings.HasPrefix(string(cfg), "# MOX ACCESS\n") || !strings.Contains(string(cfg), "model = \"gpt-5\"") || !strings.Contains(string(cfg), "apps = false # MOX ACCESS") {
		t.Fatalf("config.toml:\n%s", cfg)
	}
	if st := a.Status(); st.Mode != state.ModeCorporate || !st.Tunnel.Connected || st.KeyText == "" {
		t.Fatalf("status after enable: %+v", st)
	}
	ops.steps = nil
	if err := a.Disable(context.Background()); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := strings.Join(ops.steps, " "); got != "env- quit launch" {
		t.Fatalf("disable order: %s", got)
	}
	auth, _ = os.ReadFile(filepath.Join(ops.home, "auth.json"))
	if string(auth) != `{"tokens":{"access_token":"personal.token.x"}}` {
		t.Fatal("personal login not restored")
	}
	cfg, _ = os.ReadFile(filepath.Join(ops.home, "config.toml"))
	if string(cfg) != "model = \"gpt-5\"\n" {
		t.Fatalf("config.toml not restored:\n%s", cfg)
	}
	if st := a.Status(); st.Mode != state.ModePersonal || st.Tunnel.Connected {
		t.Fatalf("status after disable: %+v", st)
	}
}

func TestEnableStopsAtFirstFailedStep(t *testing.T) {
	dir := t.TempDir()
	gw, certPEM := gateway(t)
	defer gw.Close()
	privPEM, userPub := tunneltest.NewUserKey(t)
	relay := tunneltest.New(t, userPub, gw.Listener.Addr().String())
	defer relay.Stop()
	ops := &fakeOps{home: filepath.Join(dir, ".codex"), failAt: "cert"}
	os.MkdirAll(ops.home, 0o700)
	os.WriteFile(filepath.Join(ops.home, "auth.json"), []byte(`{"tokens":{"access_token":"personal.token.x"}}`), 0o600)
	a, _ := New(filepath.Join(dir, "app"), ops, func(string) {})
	a.Import(testBundle(t, dir, relay.Port(), relay.HostKeyLine(), privPEM, certPEM, freePort(t)))
	err := a.Enable(context.Background())
	if err == nil || !strings.Contains(err.Error(), "сертификат") {
		t.Fatalf("expected certificate step error, got %v", err)
	}
	auth, _ := os.ReadFile(filepath.Join(ops.home, "auth.json"))
	if string(auth) != `{"tokens":{"access_token":"personal.token.x"}}` {
		t.Fatal("auth.json must be untouched when the certificate step fails")
	}
	if a.Status().Mode != state.ModePersonal {
		t.Fatal("mode must stay personal")
	}
}

func TestImportRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	a, _ := New(filepath.Join(dir, "app"), &fakeOps{home: dir}, func(string) {})
	p := filepath.Join(dir, "x.moxaccess")
	os.WriteFile(p, []byte("{}"), 0o600)
	if err := a.Import(p); err == nil {
		t.Fatal("accepted garbage")
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "bundle.moxaccess")); !os.IsNotExist(err) {
		t.Fatal("garbage must not be stored")
	}
}

var _ = bundle.Format
