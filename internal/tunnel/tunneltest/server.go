// Package tunneltest is the smallest SSH relay a test can talk to: one user key, direct-tcpip channels forwarded to a
// target address the test chooses. It stands in for moxrelay@vps6 with permitopen=127.0.0.1:8000.
package tunneltest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

type Server struct {
	t        *testing.T
	ln       net.Listener
	hostKey  ssh.Signer
	userKey  ssh.PublicKey
	target   atomic.Value // string
	Accepted atomic.Int32
	mu       sync.Mutex
	conns    []net.Conn
}

// NewUserKey makes an ed25519 key pair as the relay expects it: the private key in OpenSSH PEM, the public one parsed.
func NewUserKey(t *testing.T) (privPEM string, pub ssh.PublicKey) {
	t.Helper()
	pubKey, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	pub, _ = ssh.NewPublicKey(pubKey)
	return string(pem.EncodeToMemory(block)), pub
}

// New starts the relay; target is the address direct-tcpip channels are forwarded to ("127.0.0.1:port").
func New(t *testing.T, userKey ssh.PublicKey, target string) *Server {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)
	s := &Server{t: t, hostKey: hostSigner, userKey: userKey}
	s.target.Store(target)
	s.Start()
	return s
}

func (s *Server) SetTarget(addr string) { s.target.Store(addr) }

func (s *Server) Start() {
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) == string(s.userKey.Marshal()) && c.User() == "moxrelay" {
			return nil, nil
		}
		return nil, io.ErrUnexpectedEOF
	}}
	cfg.AddHostKey(s.hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.t.Fatal(err)
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
			go s.serve(nc, cfg)
		}
	}()
}

func (s *Server) serve(nc net.Conn, cfg *ssh.ServerConfig) {
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
		s.Accepted.Add(1)
		target, err := net.Dial("tcp", s.target.Load().(string))
		if err != nil {
			c.Close()
			continue
		}
		go func() { io.Copy(c, target); c.Close() }()
		go func() { io.Copy(target, c); target.Close() }()
	}
}

// Stop drops the listener and every live connection: a relay that went away, not one that merely stopped accepting.
func (s *Server) Stop() {
	s.ln.Close()
	s.mu.Lock()
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
	s.mu.Unlock()
}

func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }
func (s *Server) HostKeyLine() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.hostKey.PublicKey())))
}
