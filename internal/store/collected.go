package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Statuses of a collected group.
const (
	StatusPending = "pending"
	StatusAdded   = "added"
	StatusMerged  = "merged"
	StatusIgnored = "ignored"
)

// Variant is one raw command line seen while collecting.
type Variant struct {
	Raw   string `json:"raw"`
	Count int    `json:"count"`
}

// Collected is a group of command lines that share one template.
type Collected struct {
	ID        int64     `json:"id"`
	Signature string    `json:"signature"` // the template, e.g. "ping {host} -n {count}"
	Category  string    `json:"category"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	Target    int64     `json:"target"` // library command it was merged into
	Secret    bool      `json:"secret"`
	FirstSeen int64     `json:"firstSeen"`
	LastSeen  int64     `json:"lastSeen"`
	Total     int       `json:"total"`
	Variants  []Variant `json:"variants"`
}

// AddCollected counts one more run of raw under its template signature.
// A group the user ignored keeps counting but stays ignored.
func (s *Store) AddCollected(signature, category, source string, secret bool, raw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := now()
	var id int64
	err = tx.QueryRow("SELECT id FROM collected WHERE signature = ?", signature).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec("INSERT INTO collected(signature, category, source, secret, first_seen, last_seen) VALUES(?, ?, ?, ?, ?, ?)",
			signature, category, source, secret, t, t)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err := tx.Exec("UPDATE collected SET last_seen = ?, secret = secret OR ? WHERE id = ?", t, secret, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO variants(collected_id, raw, count) VALUES(?, ?, 1) ON CONFLICT(collected_id, raw) DO UPDATE SET count = count + 1", id, raw); err != nil {
		return err
	}
	return tx.Commit()
}

// CollectedList returns groups with the given status ("" for all), most run first.
func (s *Store) CollectedList(status string) ([]Collected, error) {
	rows, err := s.db.Query(`SELECT c.id, c.signature, c.category, c.source, c.status, IFNULL(c.target, 0), c.secret, c.first_seen, c.last_seen,
			IFNULL((SELECT SUM(count) FROM variants v WHERE v.collected_id = c.id), 0) AS total
		FROM collected c WHERE ? = '' OR c.status = ? ORDER BY total DESC, c.last_seen DESC`, status, status)
	if err != nil {
		return nil, err
	}
	out := []Collected{}
	for rows.Next() {
		var c Collected
		if err := rows.Scan(&c.ID, &c.Signature, &c.Category, &c.Source, &c.Status, &c.Target, &c.Secret, &c.FirstSeen, &c.LastSeen, &c.Total); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		vr, err := s.db.Query("SELECT raw, count FROM variants WHERE collected_id = ? ORDER BY count DESC LIMIT 20", out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Variants = []Variant{}
		for vr.Next() {
			var v Variant
			if err := vr.Scan(&v.Raw, &v.Count); err != nil {
				vr.Close()
				return nil, err
			}
			out[i].Variants = append(out[i].Variants, v)
		}
		vr.Close()
	}
	return out, nil
}

// SetCollectedStatus records what the user decided for a group.
func (s *Store) SetCollectedStatus(id int64, status string, target int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tgt any
	if target > 0 {
		tgt = target
	}
	_, err := s.db.Exec("UPDATE collected SET status = ?, target = ? WHERE id = ?", status, tgt, id)
	return err
}

// ---- connections -------------------------------------------------------------

// Serial is the line setup of a COM / tty port.
type Serial struct {
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	DataBits int    `json:"dataBits"`
	Parity   string `json:"parity"`   // none, even, odd
	StopBits string `json:"stopBits"` // 1, 2
	LineEnd  string `json:"lineEnd"`  // cr, lf, crlf
}

// Connection is a saved profile. Passwords are never stored here; they live
// in the operating system's keychain under the connection's ID.
type Connection struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"` // ssh, telnet, serial
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Auth    string `json:"auth"` // key, password, agent
	KeyPath string `json:"keyPath"`
	Serial  Serial `json:"serial"`
	Created int64  `json:"created"`
}

// Connections returns the saved profiles.
func (s *Store) Connections() ([]Connection, error) {
	rows, err := s.db.Query("SELECT id, kind, name, host, port, user, auth, key_path, serial, created FROM connections ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		var serial string
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.Host, &c.Port, &c.User, &c.Auth, &c.KeyPath, &serial, &c.Created); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(serial), &c.Serial)
		out = append(out, c)
	}
	return out, rows.Err()
}

// Connection returns one profile.
func (s *Store) Connection(id int64) (Connection, error) {
	var c Connection
	var serial string
	err := s.db.QueryRow("SELECT id, kind, name, host, port, user, auth, key_path, serial, created FROM connections WHERE id = ?", id).
		Scan(&c.ID, &c.Kind, &c.Name, &c.Host, &c.Port, &c.User, &c.Auth, &c.KeyPath, &serial, &c.Created)
	if err == nil {
		json.Unmarshal([]byte(serial), &c.Serial)
	}
	return c, err
}

// SaveConnection inserts (ID 0) or updates a profile.
func (s *Store) SaveConnection(c Connection) (int64, error) {
	switch c.Kind {
	case "ssh", "telnet", "serial":
	default:
		return 0, errors.New("unknown connection kind")
	}
	if c.Name == "" {
		c.Name = c.Host
		if c.Kind == "serial" {
			c.Name = c.Serial.Port
		}
	}
	serial, _ := json.Marshal(c.Serial)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == 0 {
		res, err := s.db.Exec("INSERT INTO connections(kind, name, host, port, user, auth, key_path, serial, created) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)",
			c.Kind, c.Name, c.Host, c.Port, c.User, c.Auth, c.KeyPath, string(serial), now())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec("UPDATE connections SET kind = ?, name = ?, host = ?, port = ?, user = ?, auth = ?, key_path = ?, serial = ? WHERE id = ?",
		c.Kind, c.Name, c.Host, c.Port, c.User, c.Auth, c.KeyPath, string(serial), c.ID)
	return c.ID, err
}

// DeleteConnection removes a profile.
func (s *Store) DeleteConnection(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM connections WHERE id = ?", id)
	return err
}
