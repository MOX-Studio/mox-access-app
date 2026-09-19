// Package tunnel keeps the SSH port forward to the MOX gateway open from inside the application: the Desktop shell
// attaches its login to localhost:<port> only, so the gateway on the server is reached through 127.0.0.1:<port> and
// [::1]:<port> here. It replaces the ssh LaunchAgent / scheduled task of the shell installers: the relay user on the
// server allows nothing but this forward (restrict,port-forwarding,permitopen).
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Config struct {
	User, Host string
	Port       int
	HostKey    string // one authorized_keys-style line: "ssh-ed25519 AAAA…"
	PrivateKey string // OpenSSH PEM
	LocalPort  int
	Remote     string // "127.0.0.1:8000" on the server
	Log        func(string)
}

type Status struct {
	Connected  bool
	Since      time.Time
	LastError  string
	Reconnects int
}

type Tunnel struct {
	cfg       Config
	mu        sync.Mutex
	client    *ssh.Client
	listeners []net.Listener
	status    Status
	cancel    context.CancelFunc
	done      chan struct{}
}

func New(cfg Config) *Tunnel {
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	return &Tunnel{cfg: cfg}
}

// SetPort changes the relay port for the next connection; tests use it, production configuration is fixed.
func (t *Tunnel) SetPort(port int) { t.mu.Lock(); t.cfg.Port = port; t.mu.Unlock() }

func (t *Tunnel) clientConfig() (*ssh.ClientConfig, error) {
	signer, err := ssh.ParsePrivateKey([]byte(t.cfg.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("ключ релея не читается: %w", err)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.cfg.HostKey))
	if err != nil {
		return nil, fmt.Errorf("host-ключ сервера не читается: %w", err)
	}
	return &ssh.ClientConfig{User: t.cfg.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.FixedHostKey(hostKey), Timeout: 15 * time.Second}, nil
}

// addr reads the relay address under the lock; dial itself never takes the lock, so Start may call it while holding it.
func (t *Tunnel) addr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return net.JoinHostPort(t.cfg.Host, strconv.Itoa(t.cfg.Port))
}

func (t *Tunnel) dial(addr string) (*ssh.Client, error) {
	cfg, err := t.clientConfig()
	if err != nil {
		return nil, err
	}
	c, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("подключение к релею %s: %w", addr, err)
	}
	return c, nil
}

// Start binds the local port(s) and connects to the relay once, synchronously, so the caller learns right away that
// the port is busy or the host key is wrong. After that a goroutine keeps the connection alive and reconnects.
func (t *Tunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		return nil
	}
	if _, err := t.clientConfig(); err != nil {
		return err
	}
	port := strconv.Itoa(t.cfg.LocalPort)
	ln4, err := net.Listen("tcp4", "127.0.0.1:"+port)
	if err != nil {
		return fmt.Errorf("порт %s занят другой программой: %w", port, err)
	}
	t.listeners = []net.Listener{ln4}
	if ln6, err := net.Listen("tcp6", "[::1]:"+port); err == nil {
		t.listeners = append(t.listeners, ln6)
	} else {
		t.cfg.Log("IPv6 loopback недоступен, слушаю только 127.0.0.1")
	}
	client, err := t.dial(net.JoinHostPort(t.cfg.Host, strconv.Itoa(t.cfg.Port)))
	if err != nil {
		for _, l := range t.listeners {
			l.Close()
		}
		t.listeners = nil
		return err
	}
	t.client = client
	t.status = Status{Connected: true, Since: time.Now()}
	ctx, t.cancel = context.WithCancel(ctx)
	t.done = make(chan struct{})
	for _, l := range t.listeners {
		go t.serve(ctx, l)
	}
	go t.keep(ctx)
	t.cfg.Log("туннель поднят: localhost:" + port + " → " + t.cfg.Remote)
	return nil
}

func (t *Tunnel) serve(ctx context.Context, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go t.forward(ctx, conn)
	}
}

func (t *Tunnel) forward(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	t.mu.Lock()
	client := t.client
	t.mu.Unlock()
	if client == nil {
		return
	}
	remote, err := client.Dial("tcp", t.cfg.Remote)
	if err != nil {
		t.cfg.Log("проброс не удался: " + err.Error())
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, remote); done <- struct{}{} }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// keep sends keepalives every 5 s; two misses (ServerAliveInterval=5/CountMax=2 of the old agent) or a closed
// connection trigger a reconnect with backoff 1→30 s, like KeepAlive under launchd but without the throttle.
func (t *Tunnel) keep(ctx context.Context) {
	defer close(t.done)
	backoff := time.Second
	misses := 0
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		t.mu.Lock()
		client := t.client
		t.mu.Unlock()
		if client == nil {
			c, err := t.dial(t.addr())
			if err != nil {
				t.setError(err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}
			t.mu.Lock()
			t.client = c
			t.status.Connected = true
			t.status.Since = time.Now()
			t.status.LastError = ""
			t.status.Reconnects++
			t.mu.Unlock()
			t.cfg.Log("туннель переподключён")
			backoff = time.Second
			misses = 0
			continue
		}
		closed := make(chan error, 1)
		go func() { closed <- client.Wait() }()
		select {
		case <-ctx.Done():
			return
		case err := <-closed:
			t.drop(client, err)
			misses = 0
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				misses++
			} else {
				misses = 0
			}
			if misses >= 2 {
				t.drop(client, errors.New("релей не отвечает"))
				misses = 0
			}
		}
	}
}

func (t *Tunnel) drop(client *ssh.Client, err error) {
	client.Close()
	t.mu.Lock()
	if t.client == client {
		t.client = nil
	}
	t.status.Connected = false
	if err != nil {
		t.status.LastError = err.Error()
	}
	t.mu.Unlock()
	t.cfg.Log("туннель разорван: " + errString(err))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (t *Tunnel) setError(err error) {
	t.mu.Lock()
	t.status.Connected = false
	t.status.LastError = err.Error()
	t.mu.Unlock()
}

func (t *Tunnel) Status() Status { t.mu.Lock(); defer t.mu.Unlock(); return t.status }

// Stop closes the listeners and the relay connection; the local port is free when it returns.
func (t *Tunnel) Stop() {
	t.mu.Lock()
	cancel, done := t.cancel, t.done
	for _, l := range t.listeners {
		l.Close()
	}
	t.listeners = nil
	if t.client != nil {
		t.client.Close()
		t.client = nil
	}
	t.cancel = nil
	t.status.Connected = false
	t.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	t.cfg.Log("туннель остановлен")
}
