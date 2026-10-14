package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
)

// defaultsVersion is raised when the layout of the defaults database or the
// way it is built changes, so every user gets it rebuilt.
const defaultsVersion = "2" // 2: device platforms, explanations keyed per device

const defaultsSchema = `CREATE TABLE meta(
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE commands(
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	platform TEXT NOT NULL,
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
	source TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL DEFAULT 0,
	updated INTEGER NOT NULL DEFAULT 0,
	base_key TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX commands_template ON commands(platform, template);
CREATE TABLE explain(
	cmd TEXT PRIMARY KEY,
	summary TEXT NOT NULL DEFAULT '',
	flags TEXT NOT NULL DEFAULT '{}',
	letters TEXT NOT NULL DEFAULT '{}',
	args TEXT NOT NULL DEFAULT '[]',
	source TEXT NOT NULL DEFAULT ''
);
CREATE TABLE synonyms(
	word TEXT PRIMARY KEY,
	alts TEXT NOT NULL
);
CREATE TABLE rules(
	id INTEGER PRIMARY KEY,
	kind TEXT NOT NULL,
	match TEXT NOT NULL,
	result TEXT NOT NULL DEFAULT ''
);`

// retiredRules were default rules of older versions. The copies those
// versions put in the user's database are removed (unless the user changed
// them). Commands of every kind are collected now, cd and dir included:
// they are what newcomers to the terminal need most.
var retiredRules = []Rule{
	{Kind: "ignore", Match: "cd, ls, dir, cls, clear, exit, pwd, history, whoami, logout, ll, la"},
}

// DefaultsHash identifies the defaults built from packs in lang.
func DefaultsHash(packs []Pack, lang string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", defaultsVersion, lang)
	json.NewEncoder(h).Encode(packs)
	return hex.EncodeToString(h.Sum(nil))
}

// DefaultsPath is the defaults database in use ("" before UseDefaults).
func (s *Store) DefaultsPath() string {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	return s.basePath
}

// UseDefaults opens the defaults database built from packs in lang, next to
// the user's database. The file is built first when it does not exist yet,
// that is when this program brings other defaults (an update) or the
// language changed; files of older defaults are removed. Then the user's
// database is fitted to the defaults (adopt).
func (s *Store) UseDefaults(packs []Pack, lang string) error {
	hash := DefaultsHash(packs, lang)
	dir := filepath.Dir(s.path)
	path := filepath.Join(dir, "cmdex-defaults-"+hash[:16]+".db")
	db, err := openDefaults(path, hash)
	if err != nil {
		if err := buildDefaults(path, packs, lang, hash); err != nil {
			return fmt.Errorf("defaults: %w", err)
		}
		if db, err = openDefaults(path, hash); err != nil {
			return fmt.Errorf("defaults: %w", err)
		}
	}
	s.baseMu.Lock()
	old := s.base
	s.base, s.basePath, s.packs = db, path, packs
	s.baseMu.Unlock()
	if old != nil {
		old.Close()
	}
	removeOldDefaults(dir, path)
	return s.adopt()
}

// openDefaults opens a defaults database read-only, if it holds hash.
func openDefaults(path, hash string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=query_only(1)&_pragma=trusted_schema(0)")
	if err != nil {
		return nil, err
	}
	var have string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'hash'").Scan(&have); err != nil || have != hash {
		db.Close()
		return nil, errors.New("defaults database is out of date")
	}
	return db, nil
}

// buildDefaults writes the defaults database: into a temporary file first,
// renamed into place when complete, so a crash never leaves half a file.
func buildDefaults(path string, packs []Pack, lang, hash string) error {
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	os.Remove(tmp)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(tmp)+"?_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	err = fillDefaults(db, packs, lang, hash)
	if cerr := db.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func fillDefaults(db *sql.DB, packs []Pack, lang, hash string) error {
	if _, err := db.Exec(defaultsSchema); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := map[int64]bool{}
	syn := map[string][]string{}
	var synWords []string
	rules := map[string]bool{}
	for _, p := range packs {
		source := "pack:" + p.Name
		for _, pc := range p.Commands {
			c := packCommand(pc, lang, source)
			if checkCommand(c) != nil {
				continue // a command that would run differently from how it looks
			}
			id := stableID(CommandKey(c.Platform, c.Template))
			for ids[id] {
				id++
			}
			params, _ := json.Marshal(c.Params)
			res, err := tx.Exec(`INSERT OR IGNORE INTO commands(id, name, platform, shell, category, template, purpose, notes, danger, tags, params, source)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, c.Name, c.Platform, c.Shell, c.Category, c.Template, c.Purpose, c.Notes, c.Danger, joinList(c.Tags), string(params), source)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				ids[id] = true
			}
		}
		for _, pe := range p.Explain {
			e := packExplain(pe, lang, source)
			flags, _ := json.Marshal(e.Flags)
			letters, _ := json.Marshal(e.Letters)
			args, _ := json.Marshal(e.Args)
			if _, err := tx.Exec("INSERT OR REPLACE INTO explain(cmd, summary, flags, letters, args, source) VALUES(?, ?, ?, ?, ?, ?)",
				e.Cmd, e.Summary, string(flags), string(letters), string(args), source); err != nil {
				return err
			}
		}
		for w, alts := range p.Synonyms {
			if _, ok := syn[w]; !ok {
				synWords = append(synWords, w)
			}
			for _, a := range alts {
				if !contains(syn[w], a) {
					syn[w] = append(syn[w], a)
				}
			}
		}
		for _, pr := range p.Rules {
			if k := RuleKey(pr.Kind, pr.Match); !rules[k] {
				rules[k] = true
				if _, err := tx.Exec("INSERT INTO rules(kind, match, result) VALUES(?, ?, ?)", pr.Kind, pr.Match, pr.Result.In(lang)); err != nil {
					return err
				}
			}
		}
	}
	for _, w := range synWords {
		if _, err := tx.Exec("INSERT INTO synonyms(word, alts) VALUES(?, ?)", w, joinList(syn[w])); err != nil {
			return err
		}
	}
	for k, v := range map[string]string{"hash": hash, "lang": lang, "version": defaultsVersion} {
		if _, err := tx.Exec("INSERT INTO meta(key, value) VALUES(?, ?)", k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// stableID gives a default command the same ID in every build of the
// defaults (it is stored as a target of collected groups), and keeps it a
// safe integer for JavaScript.
func stableID(key string) int64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	if id := int64(h.Sum64() & (1<<52 - 1)); id != 0 {
		return id
	}
	return 1
}

// removeOldDefaults deletes defaults databases other than keep. One still
// open by another Cmdex stays until a later start.
func removeOldDefaults(dir, keep string) {
	old, _ := filepath.Glob(filepath.Join(dir, "cmdex-defaults-*"))
	for _, p := range old {
		if !strings.EqualFold(p, keep) {
			os.Remove(p)
		}
	}
}

// ---- reading the defaults ------------------------------------------------------

// baseState is what the user's database records about one default.
type baseState struct {
	uses   int
	hidden bool
}

func (s *Store) states(kind string) (map[string]baseState, error) {
	rows, err := s.db.Query("SELECT key, uses, hidden FROM base_state WHERE kind = ?", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]baseState{}
	for rows.Next() {
		var k string
		var st baseState
		if err := rows.Scan(&k, &st.uses, &st.hidden); err != nil {
			return nil, err
		}
		out[k] = st
	}
	return out, rows.Err()
}

func (s *Store) state(kind, key string) baseState {
	var st baseState
	s.db.QueryRow("SELECT uses, hidden FROM base_state WHERE kind = ? AND key = ?", kind, key).Scan(&st.uses, &st.hidden)
	return st
}

func (s *Store) setHidden(kind, key string, hidden bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO base_state(kind, key, hidden) VALUES(?, ?, ?)
		ON CONFLICT(kind, key) DO UPDATE SET hidden = excluded.hidden`, kind, key, hidden)
	return err
}

func markDefault(c *Command, st baseState) {
	c.ID, c.Uses, c.Base = -c.ID, st.uses, true
}

// defaultCommands returns the defaults that user (the user's commands)
// neither replaces nor holds already, and that were not hidden.
func (s *Store) defaultCommands(user []Command) ([]Command, error) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	if s.base == nil {
		return nil, nil
	}
	state, err := s.states("command")
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, c := range user {
		taken[CommandKey(c.Platform, c.Template)] = true
		if c.BaseKey != "" {
			taken[c.BaseKey] = true
		}
	}
	rows, err := s.base.Query("SELECT " + commandCols + " FROM commands")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Command
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		key := CommandKey(c.Platform, c.Template)
		st := state[key]
		if taken[key] || st.hidden {
			continue
		}
		markDefault(&c, st)
		out = append(out, c)
	}
	return out, rows.Err()
}

// defaultCommand returns one default (hidden or not) found by where.
func (s *Store) defaultCommand(where string, args ...any) (Command, error) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	if s.base == nil {
		return Command{}, sql.ErrNoRows
	}
	c, err := scanCommand(s.base.QueryRow("SELECT "+commandCols+" FROM commands WHERE "+where, args...))
	if err != nil {
		return c, err
	}
	markDefault(&c, s.state("command", CommandKey(c.Platform, c.Template)))
	return c, nil
}

// hasDefault reports whether key (CommandKey) is a default command.
func (s *Store) hasDefault(key string) bool {
	platform, template, _ := strings.Cut(key, " ")
	_, err := s.defaultCommand("platform = ? AND template = ?", platform, template)
	return err == nil
}

// defaultShown reports whether the library shows the default command key:
// it exists, was not hidden, and no command of the user's replaces it.
func (s *Store) defaultShown(key string) bool {
	if !s.hasDefault(key) || s.state("command", key).hidden {
		return false
	}
	platform, template, _ := strings.Cut(key, " ")
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM commands WHERE base_key = ? OR (platform = ? AND template = ?)", key, platform, template).Scan(&n)
	return n == 0
}

func (s *Store) defaultExplain(cmd string) (Explain, bool) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	if s.base == nil {
		return Explain{}, false
	}
	return scanExplain(s.base.QueryRow("SELECT cmd, summary, flags, letters, args, source FROM explain WHERE cmd = ?", strings.ToLower(cmd)))
}

func (s *Store) defaultSynonyms() (map[string][]string, error) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	if s.base == nil {
		return map[string][]string{}, nil
	}
	return readSynonyms(s.base)
}

// defaultRules returns the default rules the user did not remove, with
// negative IDs.
func (s *Store) defaultRules() ([]Rule, error) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	if s.base == nil {
		return []Rule{}, nil
	}
	all, err := readRules(s.base)
	if err != nil {
		return nil, err
	}
	state, err := s.states("rule")
	if err != nil {
		return nil, err
	}
	out := []Rule{}
	for _, r := range all {
		if !state[RuleKey(r.Kind, r.Match)].hidden {
			r.ID = -r.ID
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Store) defaultRule(id int64) (Rule, bool) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	var r Rule
	if s.base == nil || s.base.QueryRow("SELECT id, kind, match, result FROM rules WHERE id = ?", id).Scan(&r.ID, &r.Kind, &r.Match, &r.Result) != nil {
		return r, false
	}
	return r, true
}

// defaultIndex is what the defaults hold, read at once so that callers can
// then lock the user's database without holding the defaults.
type defaultIndex struct {
	commands map[string]int64 // CommandKey -> library ID (negative)
	danger   map[string]int   // CommandKey -> safety level
	explain  map[string]bool
	rules    map[string]bool // RuleKey
	synonyms map[string][]string
	packs    []Pack
}

func (s *Store) defaultIndex() (defaultIndex, bool) {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	idx := defaultIndex{commands: map[string]int64{}, danger: map[string]int{}, explain: map[string]bool{}, rules: map[string]bool{}, synonyms: map[string][]string{}, packs: s.packs}
	if s.base == nil {
		return idx, false
	}
	if rows, err := s.base.Query("SELECT id, platform, template, danger FROM commands"); err == nil {
		for rows.Next() {
			var id int64
			var p, t string
			var d int
			rows.Scan(&id, &p, &t, &d)
			idx.commands[CommandKey(p, t)] = -id
			idx.danger[CommandKey(p, t)] = d
		}
		rows.Close()
	}
	if rows, err := s.base.Query("SELECT cmd FROM explain"); err == nil {
		for rows.Next() {
			var c string
			rows.Scan(&c)
			idx.explain[c] = true
		}
		rows.Close()
	}
	if rs, err := readRules(s.base); err == nil {
		for _, r := range rs {
			idx.rules[RuleKey(r.Kind, r.Match)] = true
		}
	}
	idx.synonyms, _ = readSynonyms(s.base)
	return idx, true
}

// RestoreDefaults brings back the default commands the user deleted
// (commands the user changed stay as they are). It returns how many.
func (s *Store) RestoreDefaults() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec("UPDATE base_state SET hidden = 0 WHERE kind = 'command' AND hidden = 1")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, err = s.db.Exec("DELETE FROM base_state WHERE hidden = 0 AND uses = 0")
	return int(n), err
}

// ---- fitting the user's database to the defaults -------------------------------

// adopt fits the user's database to the defaults. Cmdex 0.1 copied the
// starter packs into it; a copy the user never edited is dropped (the
// default shows instead, with the copy's run count), an edited one becomes
// the user's copy of that default. Old copies of default explanations are
// dropped too, and, while upgrading only, old copies of default rules and
// synonyms, so later updates reach the user; what the user changed stays.
// It runs at every start, which keeps the database right after going back
// to an older Cmdex and forward again, and changes nothing otherwise.
// The first time, the database is first copied to cmdex.db.bak-v1.
func (s *Store) adopt() error {
	idx, ok := s.defaultIndex()
	if !ok {
		return nil
	}
	// a library of Cmdex 0.1 (starter packs copied in), not yet fitted
	upgrading := s.Setting("packs.imported", "") != ""
	if upgrading {
		bak := s.BackupPath()
		if _, err := os.Stat(bak); errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			_, err := s.db.Exec("VACUUM INTO ?", bak)
			s.mu.Unlock()
			if err != nil {
				return fmt.Errorf("backup before update: %w", err)
			}
			if err := os.Chmod(bak, 0o600); err != nil { // like the database: its owner only
				return fmt.Errorf("backup before update: %w", err)
			}
		}
	}

	// what the packs say, in every language
	byName := map[string]string{}      // platform + name -> CommandKey
	ruleWords := map[string][]string{} // RuleKey -> group names
	for _, p := range idx.packs {
		for _, pc := range p.Commands {
			key := CommandKey(normPlatform(pc.Platform), pc.Template)
			for _, n := range pc.Name {
				byName[normPlatform(pc.Platform)+" "+n] = key
			}
		}
		for _, pr := range p.Rules {
			ruleWords[RuleKey(pr.Kind, pr.Match)] = append(ruleWords[RuleKey(pr.Kind, pr.Match)], textValues(pr.Result)...)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type row struct {
		id                       int64
		platform, template, name string
		uses                     int
		created, updated         int64
	}
	var rows []row
	q, err := tx.Query("SELECT id, platform, template, name, uses, created, updated FROM commands WHERE source LIKE 'pack:%' AND base_key = ''")
	if err != nil {
		return err
	}
	for q.Next() {
		var r row
		if err := q.Scan(&r.id, &r.platform, &r.template, &r.name, &r.uses, &r.created, &r.updated); err != nil {
			q.Close()
			return err
		}
		rows = append(rows, r)
	}
	q.Close()
	for _, r := range rows {
		target := CommandKey(r.platform, r.template)
		if idx.commands[target] == 0 {
			// the template changed since: the same command by its name, but
			// only for a library being upgraded, never for imported commands
			// (a file must not hide a starter command behind one of its own)
			if !upgrading {
				continue
			}
			target = byName[r.platform+" "+r.name]
			if idx.commands[target] == 0 {
				continue // not a default any more: it stays the user's
			}
		}
		var taken int
		tx.QueryRow("SELECT COUNT(*) FROM commands WHERE id != ? AND (base_key = ? OR platform || ' ' || template = ?)", r.id, target, target).Scan(&taken)
		if r.updated == r.created { // never edited: the default replaces it
			if _, err := tx.Exec(`INSERT INTO base_state(kind, key, uses) VALUES('command', ?, ?)
				ON CONFLICT(kind, key) DO UPDATE SET uses = MAX(uses, excluded.uses)`, target, r.uses); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM commands WHERE id = ?", r.id); err != nil {
				return err
			}
		} else if taken == 0 {
			if _, err := tx.Exec("UPDATE commands SET base_key = ? WHERE id = ?", target, r.id); err != nil {
				return err
			}
		}
	}

	var exps []string
	q, err = tx.Query("SELECT cmd FROM explain WHERE source LIKE 'pack:%'")
	if err != nil {
		return err
	}
	for q.Next() {
		var c string
		q.Scan(&c)
		exps = append(exps, c)
	}
	q.Close()
	for _, c := range exps {
		if idx.explain[c] {
			if _, err := tx.Exec("DELETE FROM explain WHERE cmd = ?", c); err != nil {
				return err
			}
		}
	}

	// Rules and synonyms are fitted only when upgrading: after that, those in
	// the user's database are the user's own, even when they look like a
	// default (a rule for the same programs, a shorter list of synonyms).
	if upgrading {
		var rules []Rule
		q, err = tx.Query("SELECT id, kind, match, result FROM rules")
		if err != nil {
			return err
		}
		for q.Next() {
			var r Rule
			q.Scan(&r.ID, &r.Kind, &r.Match, &r.Result)
			rules = append(rules, r)
		}
		q.Close()
		for _, r := range rules {
			retired := false
			for _, x := range retiredRules {
				retired = retired || (x.Kind == r.Kind && x.Match == r.Match)
			}
			isDefault := idx.rules[RuleKey(r.Kind, r.Match)]
			words := ruleWords[RuleKey(r.Kind, r.Match)]
			switch {
			case retired, isDefault && (contains(words, r.Result) || (r.Result == "" && len(words) == 0)):
				if _, err := tx.Exec("DELETE FROM rules WHERE id = ?", r.ID); err != nil {
					return err
				}
			case isDefault: // the user renamed the group: their rule replaces the default
				if _, err := tx.Exec(`INSERT INTO base_state(kind, key, hidden) VALUES('rule', ?, 1)
				ON CONFLICT(kind, key) DO UPDATE SET hidden = 1`, RuleKey(r.Kind, r.Match)); err != nil {
					return err
				}
			}
		}

		mine := map[string][]string{}
		q, err = tx.Query("SELECT word, alts FROM synonyms")
		if err != nil {
			return err
		}
		for q.Next() {
			var w, alts string
			q.Scan(&w, &alts)
			mine[w] = splitList(alts)
		}
		q.Close()
		for w, alts := range mine {
			if base, ok := idx.synonyms[w]; ok && len(alts) > 0 && subset(alts, base) {
				if _, err := tx.Exec("DELETE FROM synonyms WHERE word = ?", w); err != nil {
					return err
				}
			}
		}

	}

	// Cmdex 0.1 imports the starter packs again when this is missing, which
	// is what a user going back to it needs.
	if _, err := tx.Exec("DELETE FROM settings WHERE key = 'packs.imported'"); err != nil {
		return err
	}
	return tx.Commit()
}

func normPlatform(p string) string {
	if ValidPlatform(p) {
		return p
	}
	return Both
}

// checkCommand reports whether a command may be stored: named, and with no
// characters that make it run differently from how it looks.
func checkCommand(c Command) error {
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Template) == "" {
		return errors.New("name and template are required")
	}
	if err := CheckCommandText(c.Template); err != nil {
		return err
	}
	for _, p := range c.Params {
		if err := CheckCommandText(p.Default); err != nil {
			return err
		}
	}
	return nil
}
