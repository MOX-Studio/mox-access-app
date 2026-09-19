package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testServer is the smallest SSH server the tunnel can talk to: one user key, direct-tcpip channels forwarded to a
// local "hello" service. It stands in for moxrelay@vps6 with permitopen=127.0.0.1:8000.
type testServer struct {
	ln       net.Listener
	hostKey  ssh.Signer
	userKey  ssh.PublicKey
	hello    net.Listener
	accepted atomic.Int32
	mu       sync.Mutex
	conns    []net.Conn
}

func newTestServer(t *testing.T, userKey ssh.PublicKey) *testServer {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)
	hello, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := hello.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.WriteString(c, "hello\n") }()
		}
	}()
	s := &testServer{hostKey: hostSigner, userKey: userKey, hello: hello}
	s.start(t)
	return s
}

func (s *testServer) start(t *testing.T) {
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) == string(s.userKey.Marshal()) && c.User() == "moxrelay" {
			return nil, nil
		}
		return nil, io.ErrUnexpectedEOF
	}}
	cfg.AddHostKey(s.hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, nc)
			s.mu.Unlock()
			go func() {
				conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "direct-tcpip" {
						ch.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					c, r, err := ch.Accept()
					if err != nil {
						continue
					}
					go ssh.DiscardRequests(r)
					s.accepted.Add(1)
					target, err := net.Dial("tcp", s.hello.Addr().String())
					if err != nil {
						c.Close()
						continue
					}
					go func() { io.Copy(c, target); c.Close() }()
					go func() { io.Copy(target, c); target.Close() }()
				}
			}()
		}
	}()
}

// stop drops the listener and every live connection: a relay that went away, not one that merely stopped accepting.
func (s *testServer) stop() {
	s.ln.Close()
	s.mu.Lock()
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
	s.mu.Unlock()
}

func (s *testServer) hostKeyLine() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.hostKey.PublicKey())))
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func readHello(t *testing.T, port int) string {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), 3*time.Second)
	if err != nil {
		return "dial: " + err.Error()
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, _ := io.ReadAll(c)
	return strings.TrimSpace(string(b))
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestForwardReconnectAndStop(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	userPub, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	privPEM := string(pem.EncodeToMemory(block))
	srv := newTestServer(t, userPub)
	defer srv.stop()
	srvPort := srv.ln.Addr().(*net.TCPAddr).Port
	local := freePort(t)
	var logs []string
	tn := New(Config{User: "moxrelay", Host: "127.0.0.1", Port: srvPort, HostKey: srv.hostKeyLine(), PrivateKey: privPEM, LocalPort: local, Remote: "127.0.0.1:1", Log: func(s string) { logs = append(logs, s) }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readHello(t, local); got != "hello" {
		t.Fatalf("through tunnel: %q (logs %v)", got, logs)
	}
	if st := tn.Status(); !st.Connected || st.Reconnects != 0 {
		t.Fatalf("status: %+v", st)
	}
	// The relay drops: the tunnel reconnects on its own and serves again.
	srv.stop()
	time.Sleep(200 * time.Millisecond)
	srv.start(t)
	newPort := srv.ln.Addr().(*net.TCPAddr).Port
	tn.SetPort(newPort) // the test server cannot keep its port; production never calls this
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if readHello(t, local) == "hello" && tn.Status().Reconnects >= 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st := tn.Status(); !st.Connected || st.Reconnects < 1 {
		t.Fatalf("no reconnect: %+v logs=%v", st, logs)
	}
	tn.Stop()
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(local)), 500*time.Millisecond); err == nil {
		t.Fatal("port still open after Stop")
	}
}

func TestWrongHostKeyIsRefused(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	userPub, _ := ssh.NewPublicKey(pub)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	srv := newTestServer(t, userPub)
	defer srv.stop()
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewPublicKey(otherPub)
	tn := New(Config{User: "moxrelay", Host: "127.0.0.1", Port: srv.ln.Addr().(*net.TCPAddr).Port, HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(other))), PrivateKey: string(pem.EncodeToMemory(block)), LocalPort: freePort(t), Remote: "127.0.0.1:1"})
	err := tn.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected host key error, got %v", err)
	}
	tn.Stop()
}

func TestBusyPortIsNamed(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	hostPub, _ := ssh.NewPublicKey(pub)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	tn := New(Config{User: "u", Host: "127.0.0.1", Port: 1, HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostPub))), PrivateKey: string(pem.EncodeToMemory(block)), LocalPort: ln.Addr().(*net.TCPAddr).Port, Remote: "127.0.0.1:1"})
	err := tn.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "занят") {
		t.Fatalf("expected busy-port error, got %v", err)
	}
}
