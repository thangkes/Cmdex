package conn

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConfig describes an SSH connection.
type SSHConfig struct {
	Host       string
	Port       int
	User       string
	Auth       string // key, password, agent
	KeyPath    string
	Password   string // password, or the key's passphrase
	KnownHosts string // default ~/.ssh/known_hosts
}

// UnknownHostError is returned for a server never seen before. The user can
// compare Fingerprint with the server's and call TrustHost to continue.
type UnknownHostError struct {
	Host        string
	Fingerprint string // SHA256:...
	Key         ssh.PublicKey
}

func (e *UnknownHostError) Error() string {
	return "unknown host " + e.Host + " (" + e.Fingerprint + ")"
}

// ErrHostKeyChanged means the server's key differs from the one saved in
// known_hosts: either it was reinstalled, or someone is in the middle.
var ErrHostKeyChanged = errors.New("the server's host key changed since the last connection — refusing to connect")

// ErrNeedPassword is returned when the password or key passphrase is missing.
var ErrNeedPassword = errors.New("password required")

func knownHostsPath(p string) string {
	if p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "known_hosts")
}

// TrustHost adds a server's key to known_hosts.
func TrustHost(knownHostsFile, host string, port int, key ssh.PublicKey) error {
	path := knownHostsPath(knownHostsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	addr := knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port)))
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{addr}, key))
	return err
}

type sshSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   interface{ Write([]byte) (int, error) }
	stop    once
}

// OpenSSH connects, checks the server against known_hosts, signs in and
// starts an interactive shell.
func OpenSSH(c SSHConfig, cols, rows int, h Handlers) (Session, error) {
	if c.Port == 0 {
		c.Port = 22
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	khPath := knownHostsPath(c.KnownHosts)
	var check ssh.HostKeyCallback
	if _, err := os.Stat(khPath); err == nil {
		if check, err = knownhosts.New(khPath); err != nil {
			return nil, fmt.Errorf("known_hosts: %w", err)
		}
	}
	hostKey := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if check != nil {
			err := check(hostname, remote, key)
			var ke *knownhosts.KeyError
			if err == nil {
				return nil
			}
			if errors.As(err, &ke) && len(ke.Want) > 0 {
				return ErrHostKeyChanged
			}
			if !errors.As(err, &ke) {
				return err
			}
		}
		return &UnknownHostError{Host: c.Host, Fingerprint: ssh.FingerprintSHA256(key), Key: key}
	}

	var auths []ssh.AuthMethod
	switch c.Auth {
	case "password":
		if c.Password == "" {
			return nil, ErrNeedPassword
		}
		auths = append(auths, ssh.Password(c.Password), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			ans := make([]string, len(qs))
			for i := range qs {
				ans[i] = c.Password
			}
			return ans, nil
		}))
	case "agent":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if runtime.GOOS == "windows" || sock == "" {
			return nil, errors.New("no SSH agent found (SSH_AUTH_SOCK)")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, err
		}
		auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
	default: // key
		path := c.KeyPath
		if strings.HasPrefix(path, "~") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[1:])
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("key file: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if c.Password == "" {
				return nil, ErrNeedPassword
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(c.Password))
		}
		if err != nil {
			return nil, fmt.Errorf("key file: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User: c.User, Auth: auths, HostKeyCallback: hostKey, Timeout: 15 * time.Second,
	})
	if err != nil {
		var uh *UnknownHostError
		if errors.As(err, &uh) {
			return nil, uh
		}
		if errors.Is(err, ErrHostKeyChanged) {
			return nil, ErrHostKeyChanged
		}
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 120, 32
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		client.Close()
		return nil, err
	}
	sess.Stderr = sess.Stdout // the pty merges them anyway
	if err := sess.Shell(); err != nil {
		client.Close()
		return nil, err
	}
	s := &sshSession{client: client, session: sess, stdin: stdin}
	go pump(stdout, h, func() { s.Close() })
	return s, nil
}

func (s *sshSession) Write(b []byte) (int, error) { return s.stdin.Write(b) }
func (s *sshSession) Resize(cols, rows int) error { return s.session.WindowChange(rows, cols) }
func (s *sshSession) Close() error {
	return s.stop.do(func() error {
		s.session.Close()
		return s.client.Close()
	})
}
