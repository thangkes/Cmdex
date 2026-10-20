package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"

	"github.com/thangkes/cmdex/internal/conn"
	"github.com/thangkes/cmdex/internal/store"
)

const keyringService = "Cmdex"

type session struct {
	id       string
	name     string
	kind     string
	platform string // windows, linux or "" (devices)
	conn     conn.Session
	pasted   string // the line ▶ pasted, until the next key: Enter runs it
}

type pendingHost struct {
	host string
	port int
	key  ssh.PublicKey
}

// SessionInfo describes an open session to the page.
type SessionInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Platform string `json:"platform"`
}

// OpenResult is the outcome of opening a profile. Need is set instead of ID
// when the user must act first: "password", or "trust" with Fingerprint.
type OpenResult struct {
	Session     *SessionInfo `json:"session"`
	Need        string       `json:"need"`
	Fingerprint string       `json:"fingerprint"`
}

func (a *App) addSession(name, kind, platform string, open func(conn.Handlers) (conn.Session, error)) (*SessionInfo, error) {
	a.mu.Lock()
	a.nextID++
	id := fmt.Sprintf("s%d", a.nextID)
	a.mu.Unlock()
	h := conn.Handlers{
		Data: func(b []byte) { a.emit("session:data", id, base64.StdEncoding.EncodeToString(b)) },
		Closed: func(err error) {
			a.mu.Lock()
			delete(a.sessions, id)
			a.mu.Unlock()
			a.endLog(id)
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			a.emit("session:closed", id, msg)
		},
	}
	c, err := open(h)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.sessions[id] = &session{id: id, name: name, kind: kind, platform: platform, conn: c}
	a.mu.Unlock()
	a.newLog(id, kind)
	return &SessionInfo{ID: id, Name: name, Kind: kind, Platform: platform}, nil
}

// OpenLocal opens a shell on this computer.
func (a *App) OpenLocal(cols, rows int) (*SessionInfo, error) {
	name, _ := conn.LocalShell()
	label := shellLabel()
	if strings.Contains(strings.ToLower(name), "pwsh") {
		label = "PowerShell 7"
	}
	return a.addSession(label, "local", runtime.GOOS, func(h conn.Handlers) (conn.Session, error) {
		return conn.OpenLocal(cols, rows, h)
	})
}

// Connections returns the saved profiles.
func (a *App) Connections() ([]store.Connection, error) { return a.store.Connections() }

// SaveConnection stores a profile. A non-empty password (or key passphrase)
// goes to the system keychain, never to the database.
func (a *App) SaveConnection(c store.Connection, password string) (int64, error) {
	id, err := a.store.SaveConnection(c)
	if err != nil {
		return 0, err
	}
	if password != "" {
		if err := keyring.Set(keyringService, fmt.Sprintf("conn-%d", id), password); err != nil {
			return id, fmt.Errorf("saved, but the password could not be stored in the keychain: %w", err)
		}
	}
	return id, nil
}

// DeleteConnection removes a profile and its stored password.
func (a *App) DeleteConnection(id int64) error {
	keyring.Delete(keyringService, fmt.Sprintf("conn-%d", id))
	return a.store.DeleteConnection(id)
}

// SerialPorts lists COM / tty ports present now.
func (a *App) SerialPorts() []string { return conn.SerialPorts() }

// OpenProfile connects a saved profile. password, when given, is used for
// this connection (and saved to the keychain if remember is set).
func (a *App) OpenProfile(id int64, cols, rows int, password string, remember bool) (OpenResult, error) {
	c, err := a.store.Connection(id)
	if err != nil {
		return OpenResult{}, err
	}
	account := fmt.Sprintf("conn-%d", id)
	if password == "" {
		password, _ = keyring.Get(keyringService, account)
	} else if remember {
		keyring.Set(keyringService, account, password)
	}
	var info *SessionInfo
	switch c.Kind {
	case "ssh":
		cfg := conn.SSHConfig{Host: c.Host, Port: c.Port, User: c.User, Auth: c.Auth, KeyPath: c.KeyPath, Password: password}
		info, err = a.addSession(c.Name, "ssh", c.SessionPlatform(), func(h conn.Handlers) (conn.Session, error) { return conn.OpenSSH(cfg, cols, rows, h) })
		var uh *conn.UnknownHostError
		switch {
		case errors.As(err, &uh):
			a.mu.Lock()
			port := c.Port
			if port == 0 {
				port = 22
			}
			a.pending[id] = &pendingHost{host: c.Host, port: port, key: uh.Key}
			a.mu.Unlock()
			return OpenResult{Need: "trust", Fingerprint: uh.Fingerprint}, nil
		case errors.Is(err, conn.ErrNeedPassword):
			return OpenResult{Need: "password"}, nil
		}
	case "telnet":
		info, err = a.addSession(c.Name, "telnet", c.SessionPlatform(), func(h conn.Handlers) (conn.Session, error) { return conn.OpenTelnet(c.Host, c.Port, h) })
	case "serial":
		sc := conn.SerialConfig{Port: c.Serial.Port, Baud: c.Serial.Baud, DataBits: c.Serial.DataBits, Parity: c.Serial.Parity, StopBits: c.Serial.StopBits, LineEnd: c.Serial.LineEnd}
		info, err = a.addSession(c.Name, "serial", c.SessionPlatform(), func(h conn.Handlers) (conn.Session, error) { return conn.OpenSerial(sc, h) })
	default:
		return OpenResult{}, errors.New("unknown connection kind")
	}
	if err != nil {
		return OpenResult{}, err
	}
	return OpenResult{Session: info}, nil
}

// TrustHost saves the key the server of profile id showed, after the user
// compared its fingerprint, then OpenProfile is called again. fingerprint
// must be the one the user was shown: a key that arrived meanwhile (another
// connection attempt, maybe from someone in the middle) is never trusted.
func (a *App) TrustHost(id int64, fingerprint string) error {
	a.mu.Lock()
	p := a.pending[id]
	delete(a.pending, id)
	a.mu.Unlock()
	if p == nil {
		return errors.New("nothing to trust")
	}
	if fingerprint == "" || ssh.FingerprintSHA256(p.key) != fingerprint {
		return errors.New("the server's key changed while you were checking it — connect again and compare the new fingerprint")
	}
	return conn.TrustHost("", p.host, p.port, p.key)
}

// Sessions lists open sessions.
func (a *App) Sessions() []SessionInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []SessionInfo{}
	for _, s := range a.sessions {
		out = append(out, SessionInfo{ID: s.id, Name: s.name, Kind: s.kind, Platform: s.platform})
	}
	return out
}

func (a *App) get(id string) (*session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	if s == nil {
		return nil, errors.New("the session is closed")
	}
	return s, nil
}

// SessionWrite sends typed input (base64, as the terminal hands it over).
func (a *App) SessionWrite(id, b64 string) error {
	s, err := a.get(id)
	if err != nil {
		return err
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	// a pasted line counts as run only when the next key is Enter: a line
	// edited first, or not run, is not collected
	a.mu.Lock()
	line := s.pasted
	s.pasted = ""
	a.mu.Unlock()
	if _, err := s.conn.Write(b); err != nil {
		return err
	}
	if line != "" && (string(b) == "\r" || string(b) == "\n") && a.coll != nil && a.store.Setting("collect.session", "1") == "1" {
		a.coll.Add(line, "session:"+s.name)
	}
	return nil
}

// SessionResize passes the terminal size on.
func (a *App) SessionResize(id string, cols, rows int) error {
	s, err := a.get(id)
	if err != nil {
		return err
	}
	return s.conn.Resize(cols, rows)
}

// SessionClose ends a session.
func (a *App) SessionClose(id string) error {
	s, err := a.get(id)
	if err != nil {
		return nil
	}
	return s.conn.Close()
}

// Run pastes a command line into a session and presses Enter: no
// confirmation. commandID (0 if none) counts a use of a library command; the
// line is also offered to the collector.
func (a *App) Run(id, line string, commandID int64) error {
	s, err := a.get(id)
	if err != nil {
		return err
	}
	line = strings.TrimRight(line, "\r\n")
	if err := store.CheckCommandText(line); err != nil {
		return err // never paste a line that runs differently from how it looks
	}
	// pasted only: the user reads it in the terminal and presses Enter
	if _, err := s.conn.Write([]byte(line)); err != nil {
		return err
	}
	a.mu.Lock()
	s.pasted = line
	a.mu.Unlock()
	if commandID != 0 { // negative: a default
		a.store.AddUses(commandID, 1)
	}
	return nil
}
