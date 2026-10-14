package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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
	Auto      bool      `json:"auto"`   // merged by the automatic merge, not by the user
	Secret    bool      `json:"secret"`
	FirstSeen int64     `json:"firstSeen"`
	LastSeen  int64     `json:"lastSeen"`
	Total     int       `json:"total"`
	Variants  []Variant `json:"variants"`
}

// AddCollected counts one more run of raw under its template signature.
// A group the user ignored keeps counting but stays ignored. When the group
// was merged into a library command, that command is returned (its run
// count should grow too); otherwise 0.
func (s *Store) AddCollected(signature, category, source string, secret bool, raw string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	t := now()
	var id, target int64
	var status string
	err = tx.QueryRow("SELECT id, status, IFNULL(target, 0) FROM collected WHERE signature = ?", signature).Scan(&id, &status, &target)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec("INSERT INTO collected(signature, category, source, secret, first_seen, last_seen) VALUES(?, ?, ?, ?, ?, ?)",
			signature, category, source, secret, t, t)
		if err != nil {
			return 0, err
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	default:
		if _, err := tx.Exec("UPDATE collected SET last_seen = ?, secret = secret OR ? WHERE id = ?", t, secret, id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec("INSERT INTO variants(collected_id, raw, count) VALUES(?, ?, 1) ON CONFLICT(collected_id, raw) DO UPDATE SET count = count + 1", id, raw); err != nil {
		return 0, err
	}
	if status != StatusMerged {
		target = 0
	}
	return target, tx.Commit()
}

// CollectedList returns groups with the given status ("" for all), most run first.
func (s *Store) CollectedList(status string) ([]Collected, error) {
	rows, err := s.db.Query(`SELECT c.id, c.signature, c.category, c.source, c.status, IFNULL(c.target, 0), c.auto, c.secret, c.first_seen, c.last_seen,
			IFNULL((SELECT SUM(count) FROM variants v WHERE v.collected_id = c.id), 0) AS total
		FROM collected c WHERE ? = '' OR c.status = ? ORDER BY total DESC, c.last_seen DESC`, status, status)
	if err != nil {
		return nil, err
	}
	out := []Collected{}
	for rows.Next() {
		var c Collected
		if err := rows.Scan(&c.ID, &c.Signature, &c.Category, &c.Source, &c.Status, &c.Target, &c.Auto, &c.Secret, &c.FirstSeen, &c.LastSeen, &c.Total); err != nil {
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
	return s.setCollected(id, status, target, false)
}

// SetCollectedAuto records a merge made by the automatic merge.
func (s *Store) SetCollectedAuto(id int64, target int64) error {
	return s.setCollected(id, StatusMerged, target, true)
}

func (s *Store) setCollected(id int64, status string, target int64, auto bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tgt any
	if target != 0 { // negative: a default
		tgt = target
	}
	_, err := s.db.Exec("UPDATE collected SET status = ?, target = ?, auto = ? WHERE id = ?", status, tgt, auto, id)
	return err
}

// ClearCollected deletes the collected log: the command lines seen, and
// the groups still waiting for review. Groups the user decided on (added,
// merged, ignored) are kept without their lines, so those commands do not
// come back for review. With before > 0, only groups last seen before that
// time (Unix seconds) are cleared. The file is then rewritten so the lines
// cannot be read back from the disk, and the same is done in the backup
// copy (BackupPath). It returns how many groups were cleared.
func (s *Store) ClearCollected(before int64) (int, error) {
	if before <= 0 {
		before = 1 << 62
	}
	s.mu.Lock()
	n, err := clearCollectedIn(s.db, before)
	if err == nil && n > 0 {
		err = erase(s.db)
	}
	s.mu.Unlock()
	if err != nil {
		return n, err
	}
	if bak, err := s.openBackup(); err == nil {
		defer bak.Close()
		if m, err := clearCollectedIn(bak, before); err != nil {
			return n, err
		} else if m > 0 {
			if err := erase(bak); err != nil {
				return n, err
			}
		}
	}
	return n, nil
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
	// Platform is the kind of device reached: windows, linux, cisco,
	// pfsense, or "" to guess (Linux over SSH, nothing over Telnet/serial).
	Platform string `json:"platform"`
}

// validDevice reports whether p may be a connection's platform.
func validDevice(p string) bool { return p == "" || (p != Both && ValidPlatform(p)) }

// SessionPlatform is the platform of a session opened from c.
func (c Connection) SessionPlatform() string {
	if c.Platform != "" {
		return c.Platform
	}
	if c.Kind == "ssh" {
		return Linux
	}
	return ""
}

// Connections returns the saved profiles.
func (s *Store) Connections() ([]Connection, error) {
	rows, err := s.db.Query("SELECT id, kind, name, host, port, user, auth, key_path, serial, created, platform FROM connections ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		var serial string
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.Host, &c.Port, &c.User, &c.Auth, &c.KeyPath, &serial, &c.Created, &c.Platform); err != nil {
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
	err := s.db.QueryRow("SELECT id, kind, name, host, port, user, auth, key_path, serial, created, platform FROM connections WHERE id = ?", id).
		Scan(&c.ID, &c.Kind, &c.Name, &c.Host, &c.Port, &c.User, &c.Auth, &c.KeyPath, &serial, &c.Created, &c.Platform)
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
	if !validDevice(c.Platform) {
		return 0, errors.New("unknown device type")
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
		res, err := s.db.Exec("INSERT INTO connections(kind, name, host, port, user, auth, key_path, serial, created, platform) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			c.Kind, c.Name, c.Host, c.Port, c.User, c.Auth, c.KeyPath, string(serial), now(), c.Platform)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec("UPDATE connections SET kind = ?, name = ?, host = ?, port = ?, user = ?, auth = ?, key_path = ?, serial = ?, platform = ? WHERE id = ?",
		c.Kind, c.Name, c.Host, c.Port, c.User, c.Auth, c.KeyPath, string(serial), c.Platform, c.ID)
	return c.ID, err
}

// DeleteConnection removes a profile.
func (s *Store) DeleteConnection(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM connections WHERE id = ?", id)
	return err
}

// RewriteVariants passes every collected line through fix (which returns the
// cleaned line, its template and whether it hides a secret) and moves lines
// whose text or template changed to the right group, keeping their counts.
// Groups left empty are removed. It returns how many lines changed.
func (s *Store) RewriteVariants(fix func(raw string) (line, signature string, secret, ok bool)) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	type row struct {
		gid                int64
		sig, cat, src, raw string
		count              int
		first, last        int64
	}
	rows, err := tx.Query(`SELECT c.id, c.signature, c.category, c.source, v.raw, v.count, c.first_seen, c.last_seen
		FROM variants v JOIN collected c ON c.id = v.collected_id`)
	if err != nil {
		return 0, err
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.gid, &r.sig, &r.cat, &r.src, &r.raw, &r.count, &r.first, &r.last); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, r)
	}
	rows.Close()
	changed := 0
	for _, r := range all {
		line, sig, secret, ok := fix(r.raw)
		if ok && line == r.raw && sig == r.sig {
			continue
		}
		changed++
		if _, err := tx.Exec("DELETE FROM variants WHERE collected_id = ? AND raw = ?", r.gid, r.raw); err != nil {
			return changed, err
		}
		if !ok { // nothing usable left: drop it
			continue
		}
		var gid int64
		err := tx.QueryRow("SELECT id FROM collected WHERE signature = ?", sig).Scan(&gid)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.Exec("INSERT INTO collected(signature, category, source, secret, first_seen, last_seen) VALUES(?, ?, ?, ?, ?, ?)",
				sig, r.cat, r.src, secret, r.first, r.last)
			if err != nil {
				return changed, err
			}
			gid, _ = res.LastInsertId()
		} else if err != nil {
			return changed, err
		} else if secret {
			tx.Exec("UPDATE collected SET secret = 1 WHERE id = ?", gid)
		}
		if _, err := tx.Exec("INSERT INTO variants(collected_id, raw, count) VALUES(?, ?, ?) ON CONFLICT(collected_id, raw) DO UPDATE SET count = count + excluded.count",
			gid, line, r.count); err != nil {
			return changed, err
		}
	}
	// groups left empty go, except decisions (kept by ClearCollected without their lines)
	if _, err := tx.Exec("DELETE FROM collected WHERE status = 'pending' AND id NOT IN (SELECT DISTINCT collected_id FROM variants)"); err != nil {
		return changed, err
	}
	return changed, tx.Commit()
}

// RewriteTemplates applies fix to the templates of commands that came from
// collecting (the user's own and pack commands are left alone).
func (s *Store) RewriteTemplates(fix func(string) string) (int, error) {
	cmds, err := s.Commands()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range cmds {
		if c.Source != "collected" {
			continue
		}
		if t := fix(c.Template); t != c.Template {
			s.mu.Lock()
			_, err := s.db.Exec("UPDATE commands SET template = ? WHERE id = ?", t, c.ID)
			s.mu.Unlock()
			if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
