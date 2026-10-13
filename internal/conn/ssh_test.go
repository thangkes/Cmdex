package conn

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshServer accepts password "pw" and echoes what the shell receives.
func sshServer(t *testing.T) (port int, swapKey func()) {
	t.Helper()
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "bob" && string(p) == "pw" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	var mu sync.Mutex
	newKey := func() {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		s, _ := ssh.NewSignerFromKey(priv)
		mu.Lock()
		cfg.AddHostKey(s) // replaces the key of the same type
		mu.Unlock()
	}
	newKey()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				mu.Lock()
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				mu.Unlock()
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, creqs, _ := nc.Accept()
					go func() {
						for r := range creqs {
							r.Reply(r.Type == "pty-req" || r.Type == "shell", nil)
						}
					}()
					go func() {
						buf := make([]byte, 256)
						for {
							n, err := ch.Read(buf)
							if err != nil {
								return
							}
							ch.Write(bytes.ToUpper(buf[:n]))
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, newKey
}

func TestSSHHostKeyChecking(t *testing.T) {
	port, swapKey := sshServer(t)
	kh := filepath.Join(t.TempDir(), "known_hosts")
	cfg := SSHConfig{Host: "127.0.0.1", Port: port, User: "bob", Auth: "password", Password: "pw", KnownHosts: kh}

	_, err := OpenSSH(cfg, 80, 24, Handlers{})
	var uh *UnknownHostError
	if !errors.As(err, &uh) || !strings.HasPrefix(uh.Fingerprint, "SHA256:") {
		t.Fatalf("first connection: want UnknownHostError, got %v", err)
	}
	if err := TrustHost(kh, "127.0.0.1", port, uh.Key); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var out bytes.Buffer
	s, err := OpenSSH(cfg, 80, 24, Handlers{Data: func(b []byte) { mu.Lock(); out.Write(b); mu.Unlock() }})
	if err != nil {
		t.Fatalf("trusted host: %v", err)
	}
	s.Write([]byte("hello"))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := strings.Contains(out.String(), "HELLO")
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("no echo: %q", out.String())
	}
	s.Close()

	bad := cfg
	bad.Password = "nope"
	if _, err := OpenSSH(bad, 80, 24, Handlers{}); err == nil {
		t.Fatal("wrong password accepted")
	}
	none := cfg
	none.Password = ""
	if _, err := OpenSSH(none, 80, 24, Handlers{}); !errors.Is(err, ErrNeedPassword) {
		t.Fatalf("missing password: %v", err)
	}

	swapKey() // the server now presents another key: must refuse, not offer to trust
	if _, err := OpenSSH(cfg, 80, 24, Handlers{}); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("changed key: want ErrHostKeyChanged, got %v", err)
	}
}
