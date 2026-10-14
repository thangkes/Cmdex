package store

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

// Key modes of a database (setting "crypto.mode").
const (
	KeyNone     = ""         // not encrypted yet: new, or written by Cmdex 0.1.5 or older
	KeyKeychain = "keychain" // the key is in the system keychain, under "crypto.keyid"
	KeyPassword = "password" // the key comes from the master password and "crypto.salt"
)

const checkValue = "cmdex" // sealed into "crypto.check" to recognize the right key

// KeyState says how the database is opened: its key mode and, for the
// keychain, the name of the key there.
func (s *Store) KeyState() (mode, keyID string) {
	return s.Setting("crypto.mode", KeyNone), s.Setting("crypto.keyid", "")
}

// PasswordSalt is the salt of the master password (KeyPassword).
func (s *Store) PasswordSalt() []byte {
	b, _ := hex.DecodeString(s.Setting("crypto.salt", ""))
	return b
}

// SetUpKey makes key the database key of a database not encrypted yet, and
// encrypts it (Unlock). mode is KeyKeychain (keyID names the key in the
// keychain) or KeyPassword (salt is what derived key from the password).
func (s *Store) SetUpKey(mode, keyID string, salt, key []byte) error {
	if m, _ := s.KeyState(); m != KeyNone {
		return errors.New("the database has a key already")
	}
	if mode != KeyKeychain && mode != KeyPassword {
		return errors.New("unknown key mode")
	}
	b, err := newBox(key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	for k, v := range map[string]string{"crypto.mode": mode, "crypto.keyid": keyID, "crypto.salt": hex.EncodeToString(salt),
		"crypto.check": b.seal("crypto.check", checkValue)} {
		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", k, v); err != nil {
			tx.Rollback()
			s.mu.Unlock()
			return err
		}
	}
	err = tx.Commit()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.Unlock(key)
}

// Unlock opens the database with key: ErrWrongKey unless it is the one set
// up. Values still plain (the first time, or written by an older Cmdex)
// are encrypted, in the backup copy too, and the files are rewritten so
// the plain values cannot be read back from the disk.
func (s *Store) Unlock(key []byte) error {
	b, err := newBox(key)
	if err != nil {
		return err
	}
	if v, err := b.open("crypto.check", s.Setting("crypto.check", "")); err != nil || v != checkValue {
		return ErrWrongKey
	}
	s.mu.Lock()
	s.box = b
	n, err := s.sealPlain(s.db)
	if err == nil && n > 0 {
		err = erase(s.db)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.sealBackup()
}

// Locked reports whether the database waits for its key.
func (s *Store) Locked() bool {
	m, _ := s.KeyState()
	return m != KeyNone && s.box == nil
}

// sealBackup encrypts the backup copy (BackupPath) with the same key. It is
// opened as a database (upgraded to this version's tables if older).
func (s *Store) sealBackup() error {
	if _, err := os.Stat(s.BackupPath()); err != nil {
		return nil
	}
	bak, err := Open(s.BackupPath())
	if err != nil {
		return err
	}
	defer bak.Close()
	bak.box = s.box
	n, err := bak.sealPlain(bak.db)
	if err == nil && n > 0 {
		err = erase(bak.db)
	}
	return err
}

// sealPlain encrypts, in db, every sensitive value still plain and turns
// its lookup column into a keyed hash. It returns how many rows changed.
func (s *Store) sealPlain(db *sql.DB) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	plain := func(col string) string {
		return "(" + col + " != '' AND " + col + " NOT LIKE '" + encPrefix + "%')"
	}

	type cmd struct {
		id                                                 int64
		name, template, purpose, notes, tags, envs, params string
	}
	var cmds []cmd
	rows, err := tx.Query("SELECT id, name, template, purpose, notes, tags, envs, params FROM commands WHERE " +
		strings.Join([]string{plain("name"), plain("template"), plain("purpose"), plain("notes"), plain("tags"), plain("envs"), plain("params")}, " OR "))
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var c cmd
		rows.Scan(&c.id, &c.name, &c.template, &c.purpose, &c.notes, &c.tags, &c.envs, &c.params)
		cmds = append(cmds, c)
	}
	rows.Close()
	for _, c := range cmds {
		vals := map[string]*string{"name": &c.name, "template": &c.template, "purpose": &c.purpose, "notes": &c.notes, "tags": &c.tags, "envs": &c.envs, "params": &c.params}
		for col, v := range vals {
			if *v, err = s.open("commands."+col, *v); err != nil {
				return n, err
			}
		}
		if _, err := tx.Exec("UPDATE commands SET name = ?, template = ?, purpose = ?, notes = ?, tags = ?, envs = ?, params = ?, template_mac = ? WHERE id = ?",
			s.seal("commands.name", c.name), s.seal("commands.template", c.template), s.seal("commands.purpose", c.purpose), s.seal("commands.notes", c.notes),
			s.seal("commands.tags", c.tags), s.seal("commands.envs", c.envs), s.seal("commands.params", c.params), s.look("commands.template", c.template), c.id); err != nil {
			return n, err
		}
		n++
	}

	type conn struct {
		id                                int64
		name, host, user, keyPath, serial string
	}
	var conns []conn
	rows, err = tx.Query("SELECT id, name, host, user, key_path, serial FROM connections WHERE " +
		strings.Join([]string{plain("name"), plain("host"), plain("user"), plain("key_path"), plain("serial")}, " OR "))
	if err != nil {
		return n, err
	}
	for rows.Next() {
		var c conn
		rows.Scan(&c.id, &c.name, &c.host, &c.user, &c.keyPath, &c.serial)
		conns = append(conns, c)
	}
	rows.Close()
	for _, c := range conns {
		vals := map[string]*string{"name": &c.name, "host": &c.host, "user": &c.user, "key_path": &c.keyPath, "serial": &c.serial}
		for col, v := range vals {
			if *v, err = s.open("connections."+col, *v); err != nil {
				return n, err
			}
		}
		if _, err := tx.Exec("UPDATE connections SET name = ?, host = ?, user = ?, key_path = ?, serial = ? WHERE id = ?",
			s.seal("connections.name", c.name), s.seal("connections.host", c.host), s.seal("connections.user", c.user),
			s.seal("connections.key_path", c.keyPath), s.seal("connections.serial", c.serial), c.id); err != nil {
			return n, err
		}
		n++
	}

	type group struct {
		id  int64
		sig string
	}
	var groups []group
	rows, err = tx.Query("SELECT id, signature FROM collected WHERE " + plain("signature"))
	if err != nil {
		return n, err
	}
	for rows.Next() {
		var g group
		rows.Scan(&g.id, &g.sig)
		groups = append(groups, g)
	}
	rows.Close()
	for _, g := range groups {
		if _, err := tx.Exec("UPDATE collected SET signature = ?, signature_mac = ? WHERE id = ?",
			s.seal("collected.signature", g.sig), s.look("collected.signature", g.sig), g.id); err != nil {
			return n, err
		}
		n++
	}

	type line struct {
		gid      int64
		raw, mac string
	}
	var lines []line
	rows, err = tx.Query("SELECT collected_id, raw, raw_mac FROM variants WHERE " + plain("raw"))
	if err != nil {
		return n, err
	}
	for rows.Next() {
		var l line
		rows.Scan(&l.gid, &l.raw, &l.mac)
		lines = append(lines, l)
	}
	rows.Close()
	for _, l := range lines {
		if _, err := tx.Exec("UPDATE variants SET raw = ?, raw_mac = ? WHERE collected_id = ? AND raw_mac = ?",
			s.seal("variants.raw", l.raw), s.look("variants.raw", l.raw), l.gid, l.mac); err != nil {
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}
