package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupFormat names a full Cmdex backup file (docs/backup-format.md).
const (
	BackupFormat  = "cmdex-backup"
	BackupVersion = 2
)

// Backup is everything in the user's database, to move it to another
// computer. The defaults are not in it (every Cmdex brings them); what the
// user did to them is: copies they edited (Command.BaseKey), and the
// defaults they deleted or ran (BaseState). Passwords are not in it either:
// they stay in the system keychain.
//
// Version 1 (Cmdex 0.1) held the starter commands as the user's own; they
// are matched to the defaults when imported.
type Backup struct {
	Format      string              `json:"format"`
	Version     int                 `json:"version"`
	Exported    string              `json:"exported"`
	App         string              `json:"app,omitempty"`
	Commands    []Command           `json:"commands"`
	Explain     []Explain           `json:"explain"`
	Synonyms    map[string][]string `json:"synonyms"`
	Rules       []Rule              `json:"rules"`
	Connections []Connection        `json:"connections"`
	Collected   []BackupCollected   `json:"collected"`
	Settings    map[string]string   `json:"settings"`
	BaseState   []BaseState         `json:"baseState"`
}

// BaseState is what the user did to one default: Kind "command" (Key is
// CommandKey) or "rule" (RuleKey); Uses counts runs, Hidden means deleted.
type BaseState struct {
	Kind   string `json:"kind"`
	Key    string `json:"key"`
	Uses   int    `json:"uses,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// BackupCollected is a collected group; Target names the library command it
// was added to or merged into by platform and template (IDs differ between
// computers).
type BackupCollected struct {
	Signature string      `json:"signature"`
	Category  string      `json:"category"`
	Source    string      `json:"source"`
	Status    string      `json:"status"`
	Secret    bool        `json:"secret"`
	FirstSeen int64       `json:"firstSeen"`
	LastSeen  int64       `json:"lastSeen"`
	Target    *CommandRef `json:"target,omitempty"`
	Variants  []Variant   `json:"variants"`
}

// CommandRef points at a library command.
type CommandRef struct {
	Platform string `json:"platform"`
	Template string `json:"template"`
}

// BackupResult counts what an import did.
type BackupResult struct {
	Added       int `json:"added"`   // commands added
	Updated     int `json:"updated"` // commands replaced by a newer copy
	Kept        int `json:"kept"`    // commands already here, kept as they are
	Explains    int `json:"explains"`
	Rules       int `json:"rules"`
	Connections int `json:"connections"`
	Collected   int `json:"collected"`
	Settings    int `json:"settings"`
	Rejected    int `json:"rejected"` // commands refused: empty, or hiding characters (CheckCommandText)
}

// portableSetting reports whether a setting belongs in a backup: not the
// read positions in this computer's history files, nor help-lookup misses.
func portableSetting(key string) bool {
	return !strings.HasPrefix(key, "collect.offset.") && !strings.HasPrefix(key, "help.miss.")
}

// ExportBackup returns the user's database.
func (s *Store) ExportBackup(app string) (Backup, error) {
	b := Backup{Format: BackupFormat, Version: BackupVersion, Exported: time.Now().UTC().Format(time.RFC3339), App: app,
		Explain: []Explain{}, Rules: []Rule{}, Collected: []BackupCollected{}, Settings: map[string]string{}, BaseState: []BaseState{}}
	var err error
	if b.Commands, err = s.userCommands(); err != nil {
		return b, err
	}
	all, err := s.Commands() // collected groups may point at defaults
	if err != nil {
		return b, err
	}
	byID := map[int64]Command{}
	for _, c := range all {
		byID[c.ID] = c
	}
	for _, c := range b.Commands {
		byID[c.ID] = c
	}
	rows, err := s.db.Query("SELECT cmd FROM explain ORDER BY cmd")
	if err != nil {
		return b, err
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		if e, ok := s.Explain(n); ok {
			b.Explain = append(b.Explain, e)
		}
	}
	if b.Synonyms, err = readSynonyms(s.db); err != nil {
		return b, err
	}
	if b.Rules, err = readRules(s.db); err != nil {
		return b, err
	}
	st, err := s.db.Query("SELECT kind, key, uses, hidden FROM base_state WHERE uses > 0 OR hidden = 1 ORDER BY kind, key")
	if err != nil {
		return b, err
	}
	for st.Next() {
		var x BaseState
		st.Scan(&x.Kind, &x.Key, &x.Uses, &x.Hidden)
		b.BaseState = append(b.BaseState, x)
	}
	st.Close()
	if b.Connections, err = s.Connections(); err != nil {
		return b, err
	}
	groups, err := s.db.Query("SELECT id, signature, category, source, status, IFNULL(target, 0), secret, first_seen, last_seen FROM collected ORDER BY id")
	if err != nil {
		return b, err
	}
	type grp struct {
		id     int64
		target int64
		bc     BackupCollected
	}
	var gs []grp
	for groups.Next() {
		var g grp
		if err := groups.Scan(&g.id, &g.bc.Signature, &g.bc.Category, &g.bc.Source, &g.bc.Status, &g.target, &g.bc.Secret, &g.bc.FirstSeen, &g.bc.LastSeen); err != nil {
			groups.Close()
			return b, err
		}
		gs = append(gs, g)
	}
	groups.Close()
	for _, g := range gs {
		if c, ok := byID[g.target]; ok {
			g.bc.Target = &CommandRef{Platform: c.Platform, Template: c.Template}
		}
		vr, err := s.db.Query("SELECT raw, count FROM variants WHERE collected_id = ? ORDER BY count DESC", g.id)
		if err != nil {
			return b, err
		}
		g.bc.Variants = []Variant{}
		for vr.Next() {
			var v Variant
			vr.Scan(&v.Raw, &v.Count)
			g.bc.Variants = append(g.bc.Variants, v)
		}
		vr.Close()
		b.Collected = append(b.Collected, g.bc)
	}
	set, err := s.db.Query("SELECT key, value FROM settings ORDER BY key")
	if err != nil {
		return b, err
	}
	defer set.Close()
	for set.Next() {
		var k, v string
		set.Scan(&k, &v)
		if portableSetting(k) {
			b.Settings[k] = v
		}
	}
	return b, set.Err()
}

// ParseBackup reads a backup file and checks it is one this version reads.
func ParseBackup(data []byte) (Backup, error) {
	var b Backup
	if err := json.Unmarshal(data, &b); err != nil {
		return b, err
	}
	if b.Format != BackupFormat {
		return b, errors.New("not a Cmdex backup file")
	}
	if b.Version < 1 || b.Version > BackupVersion {
		return b, fmt.Errorf("backup version %d is newer than this Cmdex can read; update Cmdex first", b.Version)
	}
	return b, nil
}

// IsBackup reports whether data looks like a full backup (not a pack).
func IsBackup(data []byte) bool {
	var probe struct {
		Format string `json:"format"`
	}
	return json.Unmarshal(data, &probe) == nil && probe.Format == BackupFormat
}

// ImportBackup loads a backup. With replace, the library is emptied first
// (the read positions in this computer's history files are kept). Without
// it the backup is merged in:
//   - copies of the defaults that were never edited (version 1 backups and
//     databases) are not added: only their run counts are kept;
//   - a command already here (same platform and template) is replaced only
//     by a copy edited more recently; its use count becomes the larger one;
//   - an explanation the user wrote here is kept;
//   - rules, connections and collected groups already here are kept;
//     run counts of collected lines become the larger one;
//   - interface settings come from the backup.
//
// Importing the same backup twice changes nothing the second time.
func (s *Store) ImportBackup(b Backup, replace bool) (BackupResult, error) {
	r, err := s.importBackup(b, replace)
	if err != nil {
		return r, err
	}
	return r, s.adopt()
}

func (s *Store) importBackup(b Backup, replace bool) (BackupResult, error) {
	var r BackupResult
	if b.Format != BackupFormat {
		return r, errors.New("not a Cmdex backup")
	}
	idx, _ := s.defaultIndex()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return r, err
	}
	defer tx.Rollback()

	if replace {
		for _, q := range []string{"DELETE FROM variants", "DELETE FROM collected", "DELETE FROM commands", "DELETE FROM explain",
			"DELETE FROM rules", "DELETE FROM synonyms", "DELETE FROM connections", "DELETE FROM base_state", "DELETE FROM settings WHERE key NOT LIKE 'collect.offset.%'"} {
			if _, err := tx.Exec(q); err != nil {
				return r, err
			}
		}
	}

	ids := map[CommandRef]int64{}
	for _, c := range b.Commands {
		if strings.TrimSpace(c.Template) == "" || strings.TrimSpace(c.Name) == "" || CheckCommandText(c.Template) != nil {
			r.Rejected++
			continue
		}
		hidden := false
		for _, p := range c.Params {
			hidden = hidden || CheckCommandText(p.Default) != nil
		}
		if hidden {
			r.Rejected++
			continue
		}
		switch c.Platform {
		case Windows, Linux, Both:
		default:
			c.Platform = Both
		}
		if c.Source == "" {
			c.Source = "user"
		}
		if c.Created == 0 {
			c.Created = now()
		}
		if c.Updated == 0 {
			c.Updated = c.Created
		}
		params, _ := json.Marshal(nonNilParams(c.Params))
		ref := CommandRef{c.Platform, c.Template}
		key := CommandKey(c.Platform, c.Template)
		if idx.commands[c.BaseKey] == 0 {
			c.BaseKey = "" // not a default (any more)
		}
		if c.BaseKey == "" && strings.HasPrefix(c.Source, "pack:") && c.Updated == c.Created && idx.commands[key] != 0 {
			// a copy of a default made by Cmdex 0.1, never edited: the default is here
			if _, err := tx.Exec(`INSERT INTO base_state(kind, key, uses) VALUES('command', ?, ?)
				ON CONFLICT(kind, key) DO UPDATE SET uses = MAX(uses, excluded.uses)`, key, c.Uses); err != nil {
				return r, err
			}
			ids[ref] = idx.commands[key]
			r.Kept++
			continue
		}
		var id, updated int64
		var uses int
		err := tx.QueryRow("SELECT id, updated, uses FROM commands WHERE platform = ? AND template = ?", c.Platform, c.Template).Scan(&id, &updated, &uses)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if c.BaseKey != "" {
				var n int
				tx.QueryRow("SELECT COUNT(*) FROM commands WHERE base_key = ?", c.BaseKey).Scan(&n)
				if n > 0 {
					c.BaseKey = "" // this computer has its own copy of that default
				}
			}
			res, err := tx.Exec(`INSERT INTO commands(name, platform, shell, category, template, purpose, notes, danger, tags, envs, params, uses, source, created, updated, base_key)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				c.Name, c.Platform, c.Shell, c.Category, c.Template, c.Purpose, c.Notes, c.Danger, joinList(c.Tags), joinList(c.Envs), string(params), c.Uses, c.Source, c.Created, c.Updated, c.BaseKey)
			if err != nil {
				return r, err
			}
			id, _ = res.LastInsertId()
			r.Added++
		case err != nil:
			return r, err
		case c.Updated > updated:
			if _, err := tx.Exec(`UPDATE commands SET name = ?, shell = ?, category = ?, purpose = ?, notes = ?, danger = ?, tags = ?, envs = ?, params = ?,
				uses = MAX(uses, ?), source = ?, created = ?, updated = ? WHERE id = ?`,
				c.Name, c.Shell, c.Category, c.Purpose, c.Notes, c.Danger, joinList(c.Tags), joinList(c.Envs), string(params), c.Uses, c.Source, c.Created, c.Updated, id); err != nil {
				return r, err
			}
			r.Updated++
		default:
			if _, err := tx.Exec("UPDATE commands SET uses = MAX(uses, ?) WHERE id = ?", c.Uses, id); err != nil {
				return r, err
			}
			r.Kept++
		}
		ids[ref] = id
	}

	for _, e := range b.Explain {
		e.Cmd = strings.ToLower(strings.TrimSpace(e.Cmd))
		if e.Cmd == "" {
			continue
		}
		var src string
		err := tx.QueryRow("SELECT source FROM explain WHERE cmd = ?", e.Cmd).Scan(&src)
		if err == nil && src == "user" && e.Source != "user" {
			continue // the user's own words here win over a pack or help text
		}
		if err == nil && src == "user" && !replace {
			continue
		}
		flags, _ := json.Marshal(nonNilMap(e.Flags))
		letters, _ := json.Marshal(nonNilStrMap(e.Letters))
		if e.Args == nil {
			e.Args = []string{}
		}
		args, _ := json.Marshal(e.Args)
		if e.Source == "" {
			e.Source = "user"
		}
		if _, err := tx.Exec(`INSERT INTO explain(cmd, summary, flags, letters, args, source) VALUES(?, ?, ?, ?, ?, ?)
			ON CONFLICT(cmd) DO UPDATE SET summary = excluded.summary, flags = excluded.flags, letters = excluded.letters, args = excluded.args, source = excluded.source`,
			e.Cmd, e.Summary, string(flags), string(letters), string(args), e.Source); err != nil {
			return r, err
		}
		r.Explains++
	}

	for w, alts := range b.Synonyms {
		var have string
		tx.QueryRow("SELECT alts FROM synonyms WHERE word = ?", w).Scan(&have)
		merged := splitList(have)
		for _, a := range alts {
			if !contains(merged, a) {
				merged = append(merged, a)
			}
		}
		if _, err := tx.Exec("INSERT INTO synonyms(word, alts) VALUES(?, ?) ON CONFLICT(word) DO UPDATE SET alts = excluded.alts", w, joinList(merged)); err != nil {
			return r, err
		}
	}

	for _, ru := range b.Rules {
		var n int
		tx.QueryRow("SELECT COUNT(*) FROM rules WHERE kind = ? AND match = ?", ru.Kind, ru.Match).Scan(&n)
		if n > 0 {
			continue
		}
		if _, err := tx.Exec("INSERT INTO rules(kind, match, result) VALUES(?, ?, ?)", ru.Kind, ru.Match, ru.Result); err != nil {
			return r, err
		}
		r.Rules++
	}

	for _, c := range b.Connections {
		switch c.Kind {
		case "ssh", "telnet", "serial":
		default:
			continue
		}
		var n int
		tx.QueryRow("SELECT COUNT(*) FROM connections WHERE kind = ? AND name = ? AND host = ? AND port = ?", c.Kind, c.Name, c.Host, c.Port).Scan(&n)
		if n > 0 {
			continue
		}
		serial, _ := json.Marshal(c.Serial)
		if c.Created == 0 {
			c.Created = now()
		}
		if _, err := tx.Exec("INSERT INTO connections(kind, name, host, port, user, auth, key_path, serial, created) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)",
			c.Kind, c.Name, c.Host, c.Port, c.User, c.Auth, c.KeyPath, string(serial), c.Created); err != nil {
			return r, err
		}
		r.Connections++
	}

	for _, g := range b.Collected {
		if g.Signature == "" {
			continue
		}
		var target any
		if g.Target != nil {
			if id, ok := ids[*g.Target]; ok {
				target = id
			} else {
				var id int64
				if tx.QueryRow("SELECT id FROM commands WHERE platform = ? AND template = ?", g.Target.Platform, g.Target.Template).Scan(&id) == nil {
					target = id
				} else if id := idx.commands[CommandKey(g.Target.Platform, g.Target.Template)]; id != 0 {
					target = id
				}
			}
		}
		switch g.Status {
		case StatusPending, StatusAdded, StatusMerged, StatusIgnored:
		default:
			g.Status = StatusPending
		}
		var gid int64
		err := tx.QueryRow("SELECT id FROM collected WHERE signature = ?", g.Signature).Scan(&gid)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.Exec("INSERT INTO collected(signature, category, source, status, target, secret, first_seen, last_seen) VALUES(?, ?, ?, ?, ?, ?, ?, ?)",
				g.Signature, g.Category, g.Source, g.Status, target, g.Secret, g.FirstSeen, g.LastSeen)
			if err != nil {
				return r, err
			}
			gid, _ = res.LastInsertId()
			r.Collected++
		} else if err != nil {
			return r, err
		}
		for _, v := range g.Variants {
			if _, err := tx.Exec("INSERT INTO variants(collected_id, raw, count) VALUES(?, ?, ?) ON CONFLICT(collected_id, raw) DO UPDATE SET count = MAX(count, excluded.count)",
				gid, v.Raw, v.Count); err != nil {
				return r, err
			}
		}
	}

	for _, x := range b.BaseState {
		if x.Kind != "command" && x.Kind != "rule" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO base_state(kind, key, uses, hidden) VALUES(?, ?, ?, ?)
			ON CONFLICT(kind, key) DO UPDATE SET uses = MAX(uses, excluded.uses), hidden = MAX(hidden, excluded.hidden)`,
			x.Kind, x.Key, x.Uses, x.Hidden); err != nil {
			return r, err
		}
	}

	for k, v := range b.Settings {
		if !portableSetting(k) {
			continue
		}
		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", k, v); err != nil {
			return r, err
		}
		r.Settings++
	}
	return r, tx.Commit()
}

func nonNilParams(p []Param) []Param {
	if p == nil {
		return []Param{}
	}
	return p
}

// ExportDatabase writes a consistent copy of the whole database file to
// path (SQLite VACUUM INTO), safe while Cmdex is running.
func (s *Store) ExportDatabase(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec("VACUUM INTO ?", path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // like the backup JSON: readable by its owner only
}

// BackupFromDatabase reads another Cmdex database file (from another
// computer, or an exported copy) as a backup. The file itself is never
// changed: it is copied (with its -wal journal, if any) into tmpDir, and
// the copy is opened, which also upgrades it if it is older.
func BackupFromDatabase(path, tmpDir string) (Backup, error) {
	copyPath := filepath.Join(tmpDir, "import.db")
	for _, ext := range []string{"", "-wal"} {
		if err := copyFile(path+ext, copyPath+ext); err != nil {
			if ext != "" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Backup{}, err
		}
	}
	c, err := Open(copyPath)
	if err != nil {
		return Backup{}, fmt.Errorf("not a Cmdex database: %w", err)
	}
	defer c.Close()
	var n int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM commands").Scan(&n); err != nil {
		return Backup{}, fmt.Errorf("not a Cmdex database: %w", err)
	}
	return c.ExportBackup("")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
