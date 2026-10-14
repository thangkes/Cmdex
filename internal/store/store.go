// Package store keeps what Cmdex knows in two SQLite files:
//
//   - the user's database (cmdex.db): commands the user wrote, collected or
//     changed, their explanations, collected commands waiting for review,
//     connection profiles and settings. Updates never touch it.
//   - the defaults database (cmdex-defaults-<hash>.db, next to it): the
//     starter commands, explanations, synonyms and rules shipped inside the
//     program. It is rebuilt whenever the program brings new ones (or the
//     interface language changes), and only read otherwise.
//
// The two are read as one library. The user's copy of a default (edited, or
// with its own explanation) hides the default; deleting a default only
// hides it (base_state), so updates never bring it back by surprise.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Platforms a command runs on. Both is Windows and Linux. Cisco (IOS and
// IOS-XE) and PfSense are devices reached over SSH, Telnet or a serial
// console.
const (
	Windows = "windows"
	Linux   = "linux"
	Both    = "both"
	Cisco   = "cisco"
	PfSense = "pfsense"
)

// ValidPlatform reports whether p is a platform a command may have.
func ValidPlatform(p string) bool {
	switch p {
	case Windows, Linux, Both, Cisco, PfSense:
		return true
	}
	return false
}

// IsDevice reports whether p is a network device rather than a computer.
func IsDevice(p string) bool { return p == Cisco || p == PfSense }

// KeywordCLI reports whether p's command line is made of keywords that may
// be abbreviated ("sh ip int br"), as on Cisco, rather than of programs and
// options. Its explanations are kept under "<platform>:<command>".
func KeywordCLI(p string) bool { return p == Cisco }

// ExplainKey is the key of a program's explanation on platform p: devices
// have their own ("cisco:show"), so their commands are never explained as
// a computer's ("ip" on Cisco is not Linux's ip).
func ExplainKey(p, program string) string {
	if IsDevice(p) {
		return p + ":" + program
	}
	return program
}

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
	BaseKey  string   `json:"baseKey,omitempty"` // the default this command replaces (CommandKey)
	Base     bool     `json:"base,omitempty"`    // a default, shipped with the program (negative ID)
}

// CommandKey names a command by platform and template, which is how a
// command is matched between the two databases, backups and updates.
func CommandKey(platform, template string) string { return platform + " " + template }

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
	db   *sql.DB
	mu   sync.Mutex // serializes writes; SQLite allows one writer
	path string

	baseMu   sync.RWMutex
	base     *sql.DB // defaults database (nil until UseDefaults)
	basePath string
	packs    []Pack // what the defaults were built from
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
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return s, nil
}

// Close closes the databases.
func (s *Store) Close() error {
	s.baseMu.Lock()
	if s.base != nil {
		s.base.Close()
		s.base = nil
	}
	s.baseMu.Unlock()
	return s.db.Close()
}

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
	// 2: defaults moved to their own database; the user's database keeps
	// which default a command replaces, and the run counts and hidden state
	// of defaults (kind "command" or "rule", key CommandKey or RuleKey)
	`ALTER TABLE commands ADD COLUMN base_key TEXT NOT NULL DEFAULT '';
	CREATE TABLE base_state(
		kind TEXT NOT NULL,
		key TEXT NOT NULL,
		uses INTEGER NOT NULL DEFAULT 0,
		hidden INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(kind, key)
	);`,
	// 3: groups merged by the automatic merge (Collect settings) are marked
	`ALTER TABLE collected ADD COLUMN auto INTEGER NOT NULL DEFAULT 0;`,
	// 4: the kind of device a connection reaches ("" = guess: Linux over SSH)
	`ALTER TABLE connections ADD COLUMN platform TEXT NOT NULL DEFAULT '';`,
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

const commandCols = "id, name, platform, shell, category, template, purpose, notes, danger, tags, envs, params, uses, source, created, updated, base_key"

func scanCommand(sc interface{ Scan(...any) error }) (Command, error) {
	var c Command
	var tags, envs, params string
	err := sc.Scan(&c.ID, &c.Name, &c.Platform, &c.Shell, &c.Category, &c.Template, &c.Purpose, &c.Notes, &c.Danger, &tags, &envs, &params, &c.Uses, &c.Source, &c.Created, &c.Updated, &c.BaseKey)
	if err != nil {
		return c, err
	}
	c.Tags, c.Envs = splitList(tags), splitList(envs)
	if json.Unmarshal([]byte(params), &c.Params) != nil || c.Params == nil {
		c.Params = []Param{}
	}
	return c, nil
}

// Commands returns the whole library: the user's commands and the defaults
// they neither replace nor hid, most used first.
func (s *Store) Commands() ([]Command, error) {
	out, err := s.userCommands()
	if err != nil {
		return nil, err
	}
	defs, err := s.defaultCommands(out)
	if err != nil {
		return nil, err
	}
	out = append(out, defs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// userCommands returns the commands of the user's database only.
func (s *Store) userCommands() ([]Command, error) {
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

// Command returns one command (a negative ID is a default).
func (s *Store) Command(id int64) (Command, error) {
	if id < 0 {
		return s.defaultCommand("id = ?", -id)
	}
	return scanCommand(s.db.QueryRow("SELECT "+commandCols+" FROM commands WHERE id = ?", id))
}

// ErrDuplicate is returned when a command with the same platform and
// template already exists.
var ErrDuplicate = errors.New("a command with this template already exists for this platform")

// SaveCommand inserts (ID 0) or updates a command and returns its ID.
// Saving a default (negative ID) makes it the user's: the copy goes into the
// user's database, replaces the default, and gets a new ID.
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
	if !ValidPlatform(c.Platform) {
		c.Platform = Both
	}
	c.BaseKey = "" // never taken from the caller
	switch {
	case c.ID < 0:
		d, err := s.Command(c.ID)
		if err != nil {
			return 0, err
		}
		c.BaseKey, c.Uses, c.ID = CommandKey(d.Platform, d.Template), d.Uses, 0
		s.db.QueryRow("SELECT id FROM commands WHERE base_key = ?", c.BaseKey).Scan(&c.ID)
		if c.Source == "" {
			c.Source = d.Source
		}
	case c.ID > 0:
		s.db.QueryRow("SELECT base_key FROM commands WHERE id = ?", c.ID).Scan(&c.BaseKey)
	}
	if key := CommandKey(c.Platform, c.Template); key != c.BaseKey && s.defaultShown(key) {
		return 0, ErrDuplicate
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
		res, err = s.db.Exec(`INSERT INTO commands(name, platform, shell, category, template, purpose, notes, danger, tags, envs, params, uses, source, created, updated, base_key)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Name, c.Platform, c.Shell, c.Category, c.Template, c.Purpose, c.Notes, c.Danger, joinList(c.Tags), joinList(c.Envs), string(params), c.Uses, c.Source, t, t, c.BaseKey)
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
	if err == nil && c.BaseKey != "" { // its run count moved to the copy
		_, err = s.db.Exec("UPDATE base_state SET uses = 0 WHERE kind = 'command' AND key = ?", c.BaseKey)
	}
	return c.ID, err
}

// DeleteCommand removes a command. A default, or the user's copy of one, is
// hidden so that updates do not bring it back (RestoreDefaults does).
func (s *Store) DeleteCommand(id int64) error {
	if id < 0 {
		d, err := s.Command(id)
		if err != nil {
			return err
		}
		return s.setHidden("command", CommandKey(d.Platform, d.Template), true)
	}
	var bk string
	s.db.QueryRow("SELECT base_key FROM commands WHERE id = ?", id).Scan(&bk)
	s.mu.Lock()
	_, err := s.db.Exec("DELETE FROM commands WHERE id = ?", id)
	s.mu.Unlock()
	if err == nil && bk != "" {
		err = s.setHidden("command", bk, true)
	}
	return err
}

// AddUses counts runs of a command (it ranks higher in search). Runs of a
// default are kept in the user's database, so rebuilding the defaults keeps
// them.
func (s *Store) AddUses(id int64, n int) error {
	if id < 0 {
		d, err := s.Command(id)
		if err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		_, err = s.db.Exec(`INSERT INTO base_state(kind, key, uses) VALUES('command', ?, ?)
			ON CONFLICT(kind, key) DO UPDATE SET uses = uses + excluded.uses`, CommandKey(d.Platform, d.Template), n)
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE commands SET uses = uses + ? WHERE id = ?", n, id)
	return err
}

// ---- explanations ------------------------------------------------------------

// Explain returns what is known about a program: the user's own words, or
// else the default ones.
func (s *Store) Explain(cmd string) (Explain, bool) {
	if e, ok := scanExplain(s.db.QueryRow("SELECT cmd, summary, flags, letters, args, source FROM explain WHERE cmd = ?", strings.ToLower(cmd))); ok {
		return e, true
	}
	return s.defaultExplain(cmd)
}

func scanExplain(row *sql.Row) (Explain, bool) {
	var e Explain
	var flags, letters, args string
	if err := row.Scan(&e.Cmd, &e.Summary, &flags, &letters, &args, &e.Source); err != nil {
		return Explain{}, false
	}
	json.Unmarshal([]byte(flags), &e.Flags)
	json.Unmarshal([]byte(letters), &e.Letters)
	json.Unmarshal([]byte(args), &e.Args)
	return e, true
}

// SaveExplain stores (replacing) what is known about a program in the
// user's database.
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

// Synonyms returns the search synonyms (normalized word -> alternatives):
// the defaults, with the user's list of a word replacing the default one.
func (s *Store) Synonyms() (map[string][]string, error) {
	out, err := s.defaultSynonyms()
	if err != nil {
		return nil, err
	}
	mine, err := readSynonyms(s.db)
	if err != nil {
		return nil, err
	}
	for w, alts := range mine {
		if len(alts) == 0 {
			delete(out, w) // the user removed a default word
		} else {
			out[w] = alts
		}
	}
	return out, nil
}

func readSynonyms(db *sql.DB) (map[string][]string, error) {
	rows, err := db.Query("SELECT word, alts FROM synonyms")
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

// SetSynonym stores the alternatives of a word (none removes it). A list
// equal to the default one is not stored, so later updates of the default
// reach the user.
func (s *Store) SetSynonym(word string, alts []string) error {
	def, _ := s.defaultSynonyms()
	base, isDefault := def[word]
	s.mu.Lock()
	defer s.mu.Unlock()
	if (len(alts) == 0 && !isDefault) || (isDefault && sameSet(alts, base)) {
		_, err := s.db.Exec("DELETE FROM synonyms WHERE word = ?", word)
		return err
	}
	_, err := s.db.Exec("INSERT INTO synonyms(word, alts) VALUES(?, ?) ON CONFLICT(word) DO UPDATE SET alts = excluded.alts", word, joinList(alts))
	return err
}

func sameSet(a, b []string) bool { return subset(a, b) && subset(b, a) }

func subset(a, b []string) bool {
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

// Rule classifies collected commands. Kind "category" puts commands whose
// program is one of Match (comma separated) into Result; "ignore" never
// collects those programs; "secret" hides the value after any of Match.
// Default rules have negative IDs.
type Rule struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Match  string `json:"match"`
	Result string `json:"result"`
}

// RuleKey names a rule between the two databases and backups.
func RuleKey(kind, match string) string { return kind + " " + match }

// Rules returns all classification rules: the defaults the user did not
// remove, then the user's own.
func (s *Store) Rules() ([]Rule, error) {
	out, err := s.defaultRules()
	if err != nil {
		return nil, err
	}
	mine, err := readRules(s.db)
	if err != nil {
		return nil, err
	}
	return append(out, mine...), nil
}

func readRules(db *sql.DB) ([]Rule, error) {
	rows, err := db.Query("SELECT id, kind, match, result FROM rules ORDER BY id")
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

// SaveRule inserts (ID 0) or updates a rule. Changing a default rule hides
// it and saves the change as the user's own rule.
func (s *Store) SaveRule(r Rule) (int64, error) {
	if r.ID < 0 {
		if err := s.DeleteRule(r.ID); err != nil {
			return 0, err
		}
		r.ID = 0
	}
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

// DeleteRule removes a rule (a default one is hidden).
func (s *Store) DeleteRule(id int64) error {
	if id < 0 {
		d, ok := s.defaultRule(-id)
		if !ok {
			return errors.New("rule not found")
		}
		return s.setHidden("rule", RuleKey(d.Kind, d.Match), true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM rules WHERE id = ?", id)
	return err
}
