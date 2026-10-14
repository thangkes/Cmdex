package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
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
	err = tx.QueryRow("SELECT id, status, IFNULL(target, 0) FROM collected WHERE signature_mac = ?", s.look("collected.signature", signature)).Scan(&id, &status, &target)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if id, err = s.insertGroup(tx, signature, category, source, secret, t, t); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	default:
		if _, err := tx.Exec("UPDATE collected SET last_seen = ?, secret = secret OR ? WHERE id = ?", t, secret, id); err != nil {
			return 0, err
		}
	}
	if err := s.addVariant(tx, id, raw, 1, "count + excluded.count"); err != nil {
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
		if c.Signature, err = s.open("collected.signature", c.Signature); err != nil {
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
		vr, err := s.db.Query("SELECT raw, count FROM variants WHERE collected_id = ? ORDER BY count DESC, rowid LIMIT 20", out[i].ID)
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
			if v.Raw, err = s.open("variants.raw", v.Raw); err != nil {
				vr.Close()
				return nil, err
			}
			out[i].Variants = append(out[i].Variants, v)
		}
		vr.Close()
	}
	return out, nil
}

// execer is a database or a transaction.
type execer interface {
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}

// groupID finds a collected group by its template.
func (s *Store) groupID(q execer, signature string) (int64, error) {
	var id int64
	err := q.QueryRow("SELECT id FROM collected WHERE signature_mac = ?", s.look("collected.signature", signature)).Scan(&id)
	return id, err
}

// insertGroup adds a collected group, its template sealed.
func (s *Store) insertGroup(q execer, signature, category, source string, secret bool, first, last int64) (int64, error) {
	res, err := q.Exec("INSERT INTO collected(signature, signature_mac, category, source, secret, first_seen, last_seen) VALUES(?, ?, ?, ?, ?, ?, ?)",
		s.seal("collected.signature", signature), s.look("collected.signature", signature), category, source, secret, first, last)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// addVariant counts runs of a line in a group, the line sealed; merge says
// how a line already there is counted ("count + excluded.count", or
// "MAX(count, excluded.count)" for imports).
func (s *Store) addVariant(q execer, gid int64, raw string, count int, merge string) error {
	_, err := q.Exec("INSERT INTO variants(collected_id, raw, raw_mac, count) VALUES(?, ?, ?, ?) ON CONFLICT(collected_id, raw_mac) DO UPDATE SET count = "+merge,
		gid, s.seal("variants.raw", raw), s.look("variants.raw", raw), count)
	return err
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

const connectionCols = "id, kind, name, host, port, user, auth, key_path, serial, created, platform"

// scanConnection reads a profile, decrypting its sealed columns.
func (s *Store) scanConnection(sc interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	var serial string
	if err := sc.Scan(&c.ID, &c.Kind, &c.Name, &c.Host, &c.Port, &c.User, &c.Auth, &c.KeyPath, &serial, &c.Created, &c.Platform); err != nil {
		return c, err
	}
	var err error
	for _, f := range []struct {
		name string
		v    *string
	}{{"connections.name", &c.Name}, {"connections.host", &c.Host}, {"connections.user", &c.User}, {"connections.key_path", &c.KeyPath}, {"connections.serial", &serial}} {
		if *f.v, err = s.open(f.name, *f.v); err != nil {
			return c, err
		}
	}
	json.Unmarshal([]byte(serial), &c.Serial)
	return c, nil
}

// Connections returns the saved profiles, by name.
func (s *Store) Connections() ([]Connection, error) {
	rows, err := s.db.Query("SELECT " + connectionCols + " FROM connections")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		c, err := s.scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, rows.Err()
}

// Connection returns one profile.
func (s *Store) Connection(id int64) (Connection, error) {
	return s.scanConnection(s.db.QueryRow("SELECT "+connectionCols+" FROM connections WHERE id = ?", id))
}

// sealConnection returns the stored form of name, host, user, key path and
// serial settings.
func (s *Store) sealConnection(c Connection) (name, host, user, keyPath, serial string) {
	sj, _ := json.Marshal(c.Serial)
	return s.seal("connections.name", c.Name), s.seal("connections.host", c.Host), s.seal("connections.user", c.User),
		s.seal("connections.key_path", c.KeyPath), s.seal("connections.serial", string(sj))
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
	name, host, user, keyPath, serial := s.sealConnection(c)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == 0 {
		res, err := s.db.Exec("INSERT INTO connections(kind, name, host, port, user, auth, key_path, serial, created, platform) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			c.Kind, name, host, c.Port, user, c.Auth, keyPath, serial, now(), c.Platform)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec("UPDATE connections SET kind = ?, name = ?, host = ?, port = ?, user = ?, auth = ?, key_path = ?, serial = ?, platform = ? WHERE id = ?",
		c.Kind, name, host, c.Port, user, c.Auth, keyPath, serial, c.Platform, c.ID)
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
		if r.sig, err = s.open("collected.signature", r.sig); err == nil {
			r.raw, err = s.open("variants.raw", r.raw)
		}
		if err != nil {
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
		if _, err := tx.Exec("DELETE FROM variants WHERE collected_id = ? AND raw_mac = ?", r.gid, s.look("variants.raw", r.raw)); err != nil {
			return changed, err
		}
		if !ok { // nothing usable left: drop it
			continue
		}
		gid, err := s.groupID(tx, sig)
		if errors.Is(err, sql.ErrNoRows) {
			if gid, err = s.insertGroup(tx, sig, r.cat, r.src, secret, r.first, r.last); err != nil {
				return changed, err
			}
		} else if err != nil {
			return changed, err
		} else if secret {
			tx.Exec("UPDATE collected SET secret = 1 WHERE id = ?", gid)
		}
		if err := s.addVariant(tx, gid, line, r.count, "count + excluded.count"); err != nil {
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
			_, err := s.db.Exec("UPDATE commands SET template = ?, template_mac = ? WHERE id = ?", s.seal("commands.template", t), s.look("commands.template", t), c.ID)
			s.mu.Unlock()
			if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
