package tunnel

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/tunnel/tunneltest"
)

func helloService(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.WriteString(c, "hello\n") }()
		}
	}()
	return ln
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func readHello(port int) string {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		return "dial: " + err.Error()
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, _ := io.ReadAll(c)
	return strings.TrimSpace(string(b))
}

func TestForwardReconnectAndStop(t *testing.T) {
	privPEM, userPub := tunneltest.NewUserKey(t)
	hello := helloService(t)
	defer hello.Close()
	srv := tunneltest.New(t, userPub, hello.Addr().String())
	defer srv.Stop()
	local := freePort(t)
	var logs []string
	tn := New(Config{User: "moxrelay", Host: "127.0.0.1", Port: srv.Port(), HostKey: srv.HostKeyLine(), PrivateKey: privPEM, LocalPort: local, Remote: "127.0.0.1:1", Log: func(s string) { logs = append(logs, s) }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readHello(local); got != "hello" {
		t.Fatalf("through tunnel: %q (logs %v)", got, logs)
	}
	if st := tn.Status(); !st.Connected || st.Reconnects != 0 {
		t.Fatalf("status: %+v", st)
	}
	srv.Stop()
	time.Sleep(200 * time.Millisecond)
	srv.Start()
	tn.SetPort(srv.Port()) // the test relay cannot keep its port; production never calls this
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if readHello(local) == "hello" && tn.Status().Reconnects >= 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st := tn.Status(); !st.Connected || st.Reconnects < 1 {
		t.Fatalf("no reconnect: %+v logs=%v", st, logs)
	}
	tn.Stop()
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)), 500*time.Millisecond); err == nil {
		t.Fatal("port still open after Stop")
	}
}

func TestWrongHostKeyIsRefused(t *testing.T) {
	privPEM, userPub := tunneltest.NewUserKey(t)
	srv := tunneltest.New(t, userPub, "127.0.0.1:1")
	defer srv.Stop()
	other := tunneltest.New(t, userPub, "127.0.0.1:1")
	defer other.Stop()
	tn := New(Config{User: "moxrelay", Host: "127.0.0.1", Port: srv.Port(), HostKey: other.HostKeyLine(), PrivateKey: privPEM, LocalPort: freePort(t), Remote: "127.0.0.1:1"})
	err := tn.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected host key error, got %v", err)
	}
	tn.Stop()
}

func TestBusyPortIsNamed(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	privPEM, userPub := tunneltest.NewUserKey(t)
	srv := tunneltest.New(t, userPub, "127.0.0.1:1")
	defer srv.Stop()
	tn := New(Config{User: "moxrelay", Host: "127.0.0.1", Port: srv.Port(), HostKey: srv.HostKeyLine(), PrivateKey: privPEM, LocalPort: ln.Addr().(*net.TCPAddr).Port, Remote: "127.0.0.1:1"})
	err := tn.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "занят") {
		t.Fatalf("expected busy-port error, got %v", err)
	}
}
