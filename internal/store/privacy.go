package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// BackupPath is the copy of the user's database made once before the
// upgrade to two databases (adopt). Cmdex never deletes it: it is the way
// back. It gets the same care as the database itself: private to the user,
// secrets masked, and the collected log cleared with the database's.
func (s *Store) BackupPath() string { return s.path + ".bak-v1" }

// openBackup opens the backup copy, if there is one. Its schema is not
// migrated: only the collected tables, which every version has, are used.
func (s *Store) openBackup() (*sql.DB, error) {
	path := s.BackupPath()
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=trusted_schema(0)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// ScrubBackup masks, in the backup copy, the secrets of collected lines and
// collected command templates with today's rules (fixLine and fixTemplate,
// as Collector.Rescrub does for the database), then rewrites the file so
// the old values are gone from it, and makes it readable by its owner
// only. Commands, connections and settings in the copy are not changed,
// except a collected command whose masked template is already there: that
// unmasked duplicate is dropped.
// It reports whether there was a copy.
func (s *Store) ScrubBackup(fixLine func(raw string) (line, signature string, secret, ok bool), fixTemplate func(string) string) (bool, error) {
	db, err := s.openBackup()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	defer db.Close()
	b := &Store{db: db, box: s.box}
	if _, err := b.RewriteVariants(fixLine); err != nil {
		return true, err
	}
	rows, err := db.Query("SELECT id, template FROM commands WHERE source = 'collected'")
	if err != nil {
		return true, err
	}
	fixed := map[int64]string{}
	for rows.Next() {
		var id int64
		var t string
		rows.Scan(&id, &t)
		if t, err = b.open("commands.template", t); err != nil {
			rows.Close()
			return true, err
		}
		if nt := fixTemplate(t); nt != t {
			fixed[id] = nt
		}
	}
	rows.Close()
	for id, t := range fixed {
		if _, err := db.Exec("UPDATE commands SET template = ?, template_mac = ? WHERE id = ?", b.seal("commands.template", t), b.look("commands.template", t), id); err != nil {
			// another command already has the masked template: drop this copy's secret anyway
			if _, err := db.Exec("DELETE FROM commands WHERE id = ?", id); err != nil {
				return true, err
			}
		}
	}
	if err := erase(db); err != nil {
		return true, err
	}
	return true, os.Chmod(s.BackupPath(), 0o600)
}

// erase rewrites a database file without its free pages, so deleted rows
// cannot be read back from the disk, and empties its journal. The content
// stays the same; when it fails (a full disk), SQLite keeps the old file.
func erase(db *sql.DB) error {
	if _, err := db.Exec("VACUUM"); err != nil {
		return err
	}
	_, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// clearCollectedIn deletes, in db, the collected lines of groups last seen
// before before, and those groups when still waiting for review. It
// returns how many groups were cleared.
func clearCollectedIn(db *sql.DB, before int64) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM collected WHERE last_seen < ?
		AND (status = 'pending' OR id IN (SELECT collected_id FROM variants))`, before).Scan(&n); err != nil || n == 0 {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM variants WHERE collected_id IN (SELECT id FROM collected WHERE last_seen < ?)", before); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM collected WHERE status = 'pending' AND last_seen < ?", before); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}
