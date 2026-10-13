// Package store keeps everything Cmdex knows on this computer in one SQLite
// file: the user's command library, the explanations used to break commands
// down, collected commands waiting for review, connection profiles and
// settings. Nothing in it is shipped with the app; starter packs are imported
// into it on first run and belong to the user from then on.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Platforms a command runs on.
const (
	Windows = "windows"
	Linux   = "linux"
	Both    = "both"
)

// Param is one {placeholder} of a command template.
type Param struct {
	Key         string `json:"key"`
	Type        string `json:"type"` // text, number, host, path, secret
	Default     string `json:"default"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Command is one entry of the library.
type Command struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	Shell    string   `json:"shell"`
	Category string   `json:"category"`
	Template string   `json:"template"`
	Purpose  string   `json:"purpose"`
	Notes    string   `json:"notes"`
	Danger   int      `json:"danger"` // 0 run at once, 1 ask first, 2 copy only
	Tags     []string `json:"tags"`
	Envs     []string `json:"envs"`
	Params   []Param  `json:"params"`
	Uses     int      `json:"uses"`
	Source   string   `json:"source"` // user, collected, pack:<name>
	Created  int64    `json:"created"`
	Updated  int64    `json:"updated"`
}

// Explain is what Cmdex knows about one program for breaking commands down.
type Explain struct {
	Cmd     string            `json:"cmd"` // lower case program name
	Summary string            `json:"summary"`
	Flags   map[string]Flag   `json:"flags"`
	Letters map[string]string `json:"letters"` // single-letter flags that combine, like -ano
	Args    []string          `json:"args"`    // positional arguments, in order
	Source  string            `json:"source"`  // pack:<name>, help, user
}

// Flag explains one option. With Value, the next token is its value and
// "{v}" in Text is replaced by it.
type Flag struct {
	Text  string `json:"text"`
	Value bool   `json:"value,omitempty"`
}

// Store is the open database.
type Store struct {
	db *sql.DB
	mu sync.Mutex // serializes writes; SQLite allows one writer
}

// DefaultPath is %APPDATA%\Cmdex\cmdex.db on Windows and
// ~/.config/cmdex/cmdex.db on Linux. CMDEX_DB overrides it (portable use,
// testing).
func DefaultPath() (string, error) {
	if p := os.Getenv("CMDEX_DB"); p != "" {
		return filepath.Abs(p)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name := "cmdex"
	if filepath.Separator == '\\' {
		name = "Cmdex"
	}
	return filepath.Join(base, name, "cmdex.db"), nil
}

// Open opens (creating and migrating if needed) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=trusted_schema(0)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

var migrations = []string{
	// 1: first schema
	`CREATE TABLE commands(
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		platform TEXT NOT NULL DEFAULT 'both',
		shell TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		template TEXT NOT NULL,
		purpose TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		danger INTEGER NOT NULL DEFAULT 0,
		tags TEXT NOT NULL DEFAULT '',
		envs TEXT NOT NULL DEFAULT '',
		params TEXT NOT NULL DEFAULT '[]',
		uses INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'user',
		created INTEGER NOT NULL,
		updated INTEGER NOT NULL
	);
	CREATE UNIQUE INDEX commands_template ON commands(platform, template);
	CREATE TABLE explain(
		cmd TEXT PRIMARY KEY,
		summary TEXT NOT NULL DEFAULT '',
		flags TEXT NOT NULL DEFAULT '{}',
		letters TEXT NOT NULL DEFAULT '{}',
		args TEXT NOT NULL DEFAULT '[]',
		source TEXT NOT NULL DEFAULT 'user'
	);
	CREATE TABLE collected(
		id INTEGER PRIMARY KEY,
		signature TEXT NOT NULL UNIQUE,
		category TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		target INTEGER,
		secret INTEGER NOT NULL DEFAULT 0,
		first_seen INTEGER NOT NULL,
		last_seen INTEGER NOT NULL
	);
	CREATE TABLE variants(
		collected_id INTEGER NOT NULL REFERENCES collected(id) ON DELETE CASCADE,
		raw TEXT NOT NULL,
		count INTEGER NOT NULL DEFAULT 1,
		PRIMARY KEY(collected_id, raw)
	);
	CREATE TABLE rules(
		id INTEGER PRIMARY KEY,
		kind TEXT NOT NULL,
		match TEXT NOT NULL,
		result TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE synonyms(
		word TEXT PRIMARY KEY,
		alts TEXT NOT NULL
	);
	CREATE TABLE connections(
		id INTEGER PRIMARY KEY,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		user TEXT NOT NULL DEFAULT '',
		auth TEXT NOT NULL DEFAULT '',
		key_path TEXT NOT NULL DEFAULT '',
		serial TEXT NOT NULL DEFAULT '{}',
		created INTEGER NOT NULL
	);
	CREATE TABLE settings(
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() int64 { return time.Now().Unix() }

func joinList(xs []string) string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return strings.Join(out, ",")
}

func splitList(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// ---- settings ----------------------------------------------------------------

// Setting returns a stored setting, or def when it is unset.
func (s *Store) Setting(key, def string) string {
	var v string
	if err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v); err != nil {
		return def
	}
	return v
}

// SetSetting stores a setting.
func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// ---- commands ----------------------------------------------------------------

const commandCols = "id, name, platform, shell, category, template, purpose, notes, danger, tags, envs, params, uses, source, created, updated"

func scanCommand(sc interface{ Scan(...any) error }) (Command, error) {
	var c Command
	var tags, envs, params string
	err := sc.Scan(&c.ID, &c.Name, &c.Platform, &c.Shell, &c.Category, &c.Template, &c.Purpose, &c.Notes, &c.Danger, &tags, &envs, &params, &c.Uses, &c.Source, &c.Created, &c.Updated)
	if err != nil {
		return c, err
	}
	c.Tags, c.Envs = splitList(tags), splitList(envs)
	if json.Unmarshal([]byte(params), &c.Params) != nil || c.Params == nil {
		c.Params = []Param{}
	}
	return c, nil
}

// Commands returns the whole library, most used first.
func (s *Store) Commands() ([]Command, error) {
	rows, err := s.db.Query("SELECT " + commandCols + " FROM commands ORDER BY uses DESC, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Command returns one command.
func (s *Store) Command(id int64) (Command, error) {
	return scanCommand(s.db.QueryRow("SELECT "+commandCols+" FROM commands WHERE id = ?", id))
}

// ErrDuplicate is returned when a command with the same platform and
// template already exists.
var ErrDuplicate = errors.New("a command with this template already exists for this platform")

// SaveCommand inserts (ID 0) or updates a command and returns its ID.
func (s *Store) SaveCommand(c Command) (int64, error) {
	c.Name, c.Template = strings.TrimSpace(c.Name), strings.TrimSpace(c.Template)
	if c.Name == "" || c.Template == "" {
		return 0, errors.New("name and template are required")
	}
	if err := CheckCommandText(c.Template); err != nil {
		return 0, err
	}
	for _, p := range c.Params {
		if err := CheckCommandText(p.Default); err != nil {
			return 0, fmt.Errorf("parameter %s: %w", p.Key, err)
		}
	}
	switch c.Platform {
	case Windows, Linux, Both:
	default:
		c.Platform = Both
	}
	if c.Source == "" {
		c.Source = "user"
	}
	params, _ := json.Marshal(c.Params)
	if c.Params == nil {
		params = []byte("[]")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := now()
	var err error
	if c.ID == 0 {
		var res sql.Result
		res, err = s.db.Exec(`INSERT INTO commands(name, platform, shell, category, template, purpose, notes, danger, tags, envs, params, uses, source, created, updated)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Name, c.Platform, c.Shell, c.Category, c.Template, c.Purpose, c.Notes, c.Danger, joinList(c.Tags), joinList(c.Envs), string(params), c.Uses, c.Source, t, t)
		if err == nil {
			c.ID, err = res.LastInsertId()
		}
	} else {
		_, err = s.db.Exec(`UPDATE commands SET name = ?, platform = ?, shell = ?, category = ?, template = ?, purpose = ?, notes = ?, danger = ?, tags = ?, envs = ?, params = ?, updated = ? WHERE id = ?`,
			c.Name, c.Platform, c.Shell, c.Category, c.Template, c.Purpose, c.Notes, c.Danger, joinList(c.Tags), joinList(c.Envs), string(params), t, c.ID)
	}
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return 0, ErrDuplicate
	}
	return c.ID, err
}

// DeleteCommand removes a command.
func (s *Store) DeleteCommand(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM commands WHERE id = ?", id)
	return err
}

// AddUses counts runs of a command (it ranks higher in search).
func (s *Store) AddUses(id int64, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE commands SET uses = uses + ? WHERE id = ?", n, id)
	return err
}

// ---- explanations ------------------------------------------------------------

// Explain returns what is known about a program ("" source when nothing).
func (s *Store) Explain(cmd string) (Explain, bool) {
	var e Explain
	var flags, letters, args string
	err := s.db.QueryRow("SELECT cmd, summary, flags, letters, args, source FROM explain WHERE cmd = ?", strings.ToLower(cmd)).
		Scan(&e.Cmd, &e.Summary, &flags, &letters, &args, &e.Source)
	if err != nil {
		return Explain{}, false
	}
	json.Unmarshal([]byte(flags), &e.Flags)
	json.Unmarshal([]byte(letters), &e.Letters)
	json.Unmarshal([]byte(args), &e.Args)
	return e, true
}

// SaveExplain stores (replacing) what is known about a program.
func (s *Store) SaveExplain(e Explain) error {
	e.Cmd = strings.ToLower(strings.TrimSpace(e.Cmd))
	if e.Cmd == "" {
		return errors.New("program name is required")
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
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO explain(cmd, summary, flags, letters, args, source) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(cmd) DO UPDATE SET summary = excluded.summary, flags = excluded.flags, letters = excluded.letters, args = excluded.args, source = excluded.source`,
		e.Cmd, e.Summary, string(flags), string(letters), string(args), e.Source)
	return err
}

func nonNilMap(m map[string]Flag) map[string]Flag {
	if m == nil {
		return map[string]Flag{}
	}
	return m
}

func nonNilStrMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// ---- synonyms and rules ------------------------------------------------------

// Synonyms returns the search synonyms (normalized word -> alternatives).
func (s *Store) Synonyms() (map[string][]string, error) {
	rows, err := s.db.Query("SELECT word, alts FROM synonyms")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var w, alts string
		if err := rows.Scan(&w, &alts); err != nil {
			return nil, err
		}
		out[w] = splitList(alts)
	}
	return out, rows.Err()
}

// SetSynonym stores the alternatives of a word (none removes it).
func (s *Store) SetSynonym(word string, alts []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(alts) == 0 {
		_, err := s.db.Exec("DELETE FROM synonyms WHERE word = ?", word)
		return err
	}
	_, err := s.db.Exec("INSERT INTO synonyms(word, alts) VALUES(?, ?) ON CONFLICT(word) DO UPDATE SET alts = excluded.alts", word, joinList(alts))
	return err
}

// Rule classifies collected commands. Kind "category" puts commands whose
// program is one of Match (comma separated) into Result; "ignore" never
// collects those programs; "secret" hides the value after any of Match.
type Rule struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Match  string `json:"match"`
	Result string `json:"result"`
}

// Rules returns all classification rules.
func (s *Store) Rules() ([]Rule, error) {
	rows, err := s.db.Query("SELECT id, kind, match, result FROM rules ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Kind, &r.Match, &r.Result); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveRule inserts (ID 0) or updates a rule.
func (s *Store) SaveRule(r Rule) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == 0 {
		res, err := s.db.Exec("INSERT INTO rules(kind, match, result) VALUES(?, ?, ?)", r.Kind, r.Match, r.Result)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec("UPDATE rules SET kind = ?, match = ?, result = ? WHERE id = ?", r.Kind, r.Match, r.Result, r.ID)
	return r.ID, err
}

// DeleteRule removes a rule.
func (s *Store) DeleteRule(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM rules WHERE id = ?", id)
	return err
}
