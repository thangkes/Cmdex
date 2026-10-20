// Package mask hides secrets and identifying values in terminal output, so
// a session's log can be shown on a shared screen.
//
// Rules are declarative — a piece of text, the word after a keyword, an RE2
// pattern — never code: a script could read the log and leak it. Values
// that identify something (an IP, a MAC, a serial number, a host…) get a
// pseudonym that stays the same within one log (IP-1, MAC-2…), so the masked
// log still reads; secrets become ••••••. Masking by patterns is never
// complete: what no rule describes stays visible.
package mask

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thangkes/cmdex/internal/collect"
)

// Hidden replaces a secret.
const Hidden = "••••••"

// Built-in categories.
const (
	Secret = "secret" // passwords, keys, tokens, community strings, hashes
	Key    = "key"    // private key blocks
	IP     = "ip"     // IPv4 and IPv6 addresses
	MAC    = "mac"    // MAC addresses
	Email  = "email"  // e-mail addresses
	Serial = "serial" // serial numbers, UUIDs
	User   = "user"   // user names (in paths, prompts, logins)
	Host   = "host"   // host names (this computer's, configured, in prompts)
	Domain = "domain" // any domain-like name (many false positives)
)

// Categories lists the built-in categories in the order they are shown.
var Categories = []string{Secret, Key, IP, MAC, Email, Serial, User, Host, Domain}

// DefaultOn tells whether a category is masked when the user has not chosen.
func DefaultOn(cat string) bool { return cat != Domain }

var prefixes = map[string]string{IP: "IP", MAC: "MAC", Email: "EMAIL", Serial: "SN", User: "USER", Host: "HOST", Domain: "HOST"}

// Rule is one rule the user added.
type Rule struct {
	Kind  string `json:"kind"`  // "text", "after" (the word after a keyword) or "pattern" (RE2)
	Value string `json:"value"` // the text, the keyword or the pattern
	Label string `json:"label"` // name in the counts and the pseudonym (LABEL-1); "" = TEXT
	Hide  bool   `json:"hide"`  // •••••• instead of a pseudonym
}

// Limits on user rules.
const (
	MaxRules   = 200
	maxValue   = 300
	minText    = 3
	maxLabel   = 20
	maxKeyLine = 200 // a private key block longer than this is cut short
)

var reLabel = regexp.MustCompile(`^[\p{L}\p{N} _-]*$`)

// Check reports whether a rule can be used.
func (r Rule) Check() error {
	v := r.Value
	if len(v) > maxValue {
		return errors.New("the rule is too long")
	}
	if len([]rune(r.Label)) > maxLabel || !reLabel.MatchString(r.Label) {
		return errors.New("the label may hold letters, digits, spaces, - and _ (20 at most)")
	}
	switch r.Kind {
	case "text", "after":
		if len([]rune(strings.TrimSpace(v))) < minText {
			return errors.New("the text needs at least 3 characters")
		}
	case "pattern":
		re, err := regexp.Compile(v)
		if err != nil {
			return errors.New("the pattern is not valid: " + err.Error())
		}
		if re.MatchString("") {
			return errors.New("the pattern matches empty text: it would mask nothing useful")
		}
	default:
		return errors.New("unknown rule kind")
	}
	return nil
}

// Config is what a Masker masks.
type Config struct {
	Cats  map[string]bool     // built-in categories on (missing: DefaultOn)
	Rules []Rule              // the user's rules (checked; bad ones are skipped)
	Known map[string][]string // values of this computer by category (Host: its name, User: the account)
}

func (c Config) on(cat string) bool {
	if v, ok := c.Cats[cat]; ok {
		return v
	}
	return DefaultOn(cat)
}

// step finds values of one kind in a line.
type step struct {
	cat    string // counted under
	prefix string // pseudonym prefix; "" hides the value
	re     *regexp.Regexp
	group  int                              // the submatch that is the value (0: all of it)
	ok     func(line string, a, b int) bool // optional check of line[a:b]
	norm   func(string) string              // what counts as the same value
	learn  bool                             // the value is masked wherever it appears later
}

// Masker is a compiled set of rules. It is safe to share; each log has its
// own State.
type Masker struct {
	cfg   Config
	steps []step
	key   bool
	sn    bool
}

// Compile prepares the rules. Bad user rules are skipped (Rule.Check tells
// the user when they add one).
func Compile(c Config) *Masker {
	m := &Masker{cfg: c, key: c.on(Key), sn: c.on(Serial)}
	for _, r := range c.Rules {
		if st, ok := ruleStep(r); ok {
			m.steps = append(m.steps, st)
		}
	}
	if c.on(Secret) {
		for _, re := range collect.SecretPatterns() {
			m.steps = append(m.steps, step{cat: Secret, re: re, group: 2, ok: notEmptySecret})
		}
		for _, re := range outputSecrets {
			m.steps = append(m.steps, step{cat: Secret, re: re, group: 2, ok: notEmptySecret})
		}
		m.steps = append(m.steps, step{cat: Secret, re: reCryptHash})
	}
	if c.on(Email) {
		m.steps = append(m.steps, step{cat: Email, prefix: prefixes[Email], re: reEmail, norm: strings.ToLower})
	}
	if c.on(Host) {
		for _, v := range c.Known[Host] {
			if st, ok := wordStep(Host, prefixes[Host], v); ok {
				m.steps = append(m.steps, st)
			}
		}
		m.steps = append(m.steps,
			step{cat: Host, prefix: prefixes[Host], re: reHostLine, group: 2, norm: strings.ToLower, learn: true, ok: hasLetter},
			step{cat: Host, prefix: prefixes[Host], re: rePromptHost, group: 1, norm: strings.ToLower, learn: true, ok: hasLetter},
			step{cat: Host, prefix: prefixes[Host], re: reDevicePrompt, group: 1, norm: strings.ToLower, ok: devicePrompt})
	}
	if c.on(User) {
		for _, v := range c.Known[User] {
			if st, ok := wordStep(User, prefixes[User], v); ok {
				st.re = regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\b`) // exact case: "admin" stays in "show admin"
				m.steps = append(m.steps, st)
			}
		}
		m.steps = append(m.steps,
			step{cat: User, prefix: prefixes[User], re: reWinHome, group: 2, norm: strings.ToLower, learn: true, ok: realUser},
			step{cat: User, prefix: prefixes[User], re: reUnixHome, group: 2, learn: true},
			step{cat: User, prefix: prefixes[User], re: rePromptUser, group: 2, learn: true},
			step{cat: User, prefix: prefixes[User], re: reLogin, group: 2, learn: true, ok: realUser})
	}
	if c.on(Serial) {
		m.steps = append(m.steps, step{cat: Serial, prefix: prefixes[Serial], re: reSerial, group: 2, norm: strings.ToUpper, learn: true, ok: hasDigit})
	}
	if c.on(MAC) {
		m.steps = append(m.steps, step{cat: MAC, prefix: prefixes[MAC], re: reMAC, norm: macKey, ok: realMAC})
	}
	if c.on(IP) {
		m.steps = append(m.steps,
			step{cat: IP, prefix: prefixes[IP], re: reIPv6, norm: strings.ToLower, ok: realIPv6},
			step{cat: IP, prefix: prefixes[IP], re: reIPv4, ok: realIPv4})
	}
	if c.on(Domain) {
		m.steps = append(m.steps, step{cat: Domain, prefix: prefixes[Domain], re: reDomain, norm: strings.ToLower, ok: realDomain})
	}
	return m
}

func ruleStep(r Rule) (step, bool) {
	if r.Check() != nil {
		return step{}, false
	}
	cat := strings.TrimSpace(r.Label)
	prefix := strings.ToUpper(strings.Join(strings.Fields(strings.ReplaceAll(cat, "_", " ")), "-"))
	if cat == "" {
		cat, prefix = "text", "TEXT"
	}
	if r.Hide {
		prefix = ""
	}
	st := step{cat: cat, prefix: prefix, norm: strings.ToLower}
	switch r.Kind {
	case "text":
		st.re = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(strings.TrimSpace(r.Value)))
	case "after":
		kw := regexp.QuoteMeta(strings.TrimSpace(r.Value))
		if isWord(kw[len(kw)-1]) {
			kw += `\b`
		}
		if isWord(kw[0]) {
			kw = `\b` + kw
		}
		st.re = regexp.MustCompile(`(?i)(` + kw + `[\s:=]*)("[^"]*"|'[^']*'|\S+)`)
		st.group, st.learn = 2, true
	case "pattern":
		st.re = regexp.MustCompile(r.Value)
		if st.re.NumSubexp() > 0 {
			st.group = 1
		}
	}
	return st, true
}

// wordStep masks a known value as a whole word, in any case.
func wordStep(cat, prefix, v string) (step, bool) {
	v = strings.TrimSpace(v)
	if len(v) < minText {
		return step{}, false
	}
	re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(v) + `\b`)
	if err != nil {
		return step{}, false
	}
	return step{cat: cat, prefix: prefix, re: re, norm: strings.ToLower}, true
}

// Pair is one pseudonym and the value behind it.
type Pair struct {
	Cat   string `json:"cat"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// State masks one log, line by line, keeping its pseudonyms.
type State struct {
	m       *Masker
	names   map[string]string          // prefix + "\x00" + normalized value -> pseudonym
	pairs   []Pair                     // pseudonyms in the order they were given
	seen    map[string]map[string]bool // category -> normalized values masked
	counter map[string]int             // prefix -> last number
	learned []learned
	isName  map[string]bool
	inKey   int  // lines left in a private key block (0: outside)
	snNext  bool // the line before was a "SerialNumber" table header
}

type learned struct {
	st    *step
	value string
}

// New starts the masking of a log.
func (m *Masker) New() *State {
	return &State{m: m, names: map[string]string{}, seen: map[string]map[string]bool{}, counter: map[string]int{}, isName: map[string]bool{}}
}

var (
	reKeyBegin = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY( BLOCK)?-----|^PuTTY-User-Key-File-|^Private-Lines:`)
	reKeyEnd   = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY( BLOCK)?-----|^Private-MAC:`)
	reSNHeader = regexp.MustCompile(`(?i)^\s*(serial ?number|identifyingnumber|uuid)\s*$`)
	reSNValue  = regexp.MustCompile(`^\s*([A-Za-z0-9][\w\-/.:]{3,63})\s*$`)
)

// Line masks one line of output.
func (s *State) Line(line string) string {
	if s.m.key {
		if s.inKey > 0 {
			s.inKey--
			if reKeyEnd.MatchString(line) {
				s.inKey = 0
				return line
			}
			s.count(Key, "block")
			return Hidden
		}
		if loc := reKeyBegin.FindStringIndex(line); loc != nil {
			s.inKey = maxKeyLine
			s.count(Key, "block")
			if reKeyEnd.MatchString(line[loc[1]:]) { // the whole key on one line
				s.inKey = 0
				return line[:loc[1]] + Hidden
			}
			return line
		}
	}
	if s.m.sn {
		header := reSNHeader.MatchString(line)
		if s.snNext && !header {
			if strings.TrimSpace(line) == "" {
				return line // tables have blank lines
			}
			s.snNext = false
			if m := reSNValue.FindStringSubmatchIndex(line); m != nil && hasDigit(line, m[2], m[3]) {
				line = line[:m[2]] + s.name(&step{cat: Serial, prefix: prefixes[Serial], norm: strings.ToUpper, learn: true}, line[m[2]:m[3]]) + line[m[3]:]
			}
		}
		s.snNext = header
	}
	for i := range s.m.steps {
		line = s.apply(&s.m.steps[i], line)
	}
	for _, l := range s.learned {
		line = s.replaceWord(l, line)
	}
	return line
}

func (s *State) apply(st *step, line string) string {
	all := st.re.FindAllStringSubmatchIndex(line, -1)
	if all == nil {
		return line
	}
	var b strings.Builder
	last := 0
	for _, m := range all {
		a, e := m[2*st.group], m[2*st.group+1]
		if a < 0 || a < last || a == e {
			continue
		}
		v := line[a:e]
		if strings.Contains(v, Hidden) || s.isName[v] || (st.ok != nil && !st.ok(line, a, e)) {
			continue
		}
		b.WriteString(line[last:a])
		b.WriteString(s.name(st, v))
		last = e
	}
	if last == 0 {
		return line
	}
	b.WriteString(line[last:])
	return b.String()
}

// replaceWord masks a learned value wherever it stands as a whole word.
func (s *State) replaceWord(l learned, line string) string {
	low, v := strings.ToLower(line), strings.ToLower(l.value)
	if len(low) != len(line) || !strings.Contains(low, v) {
		return line // (a value whose case changes its length is left to its rule)
	}
	var b strings.Builder
	last := 0
	for i := 0; ; {
		j := strings.Index(low[i:], v)
		if j < 0 {
			break
		}
		a, e := i+j, i+j+len(v)
		i = e
		if (a > 0 && isWord(line[a-1])) || (e < len(line) && isWord(line[e])) {
			continue
		}
		b.WriteString(line[last:a])
		b.WriteString(s.name(l.st, line[a:e]))
		last = e
	}
	if last == 0 {
		return line
	}
	b.WriteString(line[last:])
	return b.String()
}

// name returns the pseudonym of v (or Hidden), giving one the first time.
func (s *State) name(st *step, v string) string {
	key := v
	if st.norm != nil {
		key = st.norm(v)
	}
	first := s.count(st.cat, key)
	if st.prefix == "" {
		return Hidden
	}
	id := st.prefix + "\x00" + key
	if n, ok := s.names[id]; ok {
		return n
	}
	s.counter[st.prefix]++
	n := st.prefix + "-" + strconv.Itoa(s.counter[st.prefix])
	s.names[id] = n
	s.isName[n] = true
	s.pairs = append(s.pairs, Pair{Cat: st.cat, Name: n, Value: v})
	if first && st.learn && len(v) >= minText {
		s.learned = append(s.learned, learned{st: st, value: v})
	}
	return n
}

// count notes a masked value; it reports whether it is new.
func (s *State) count(cat, key string) bool {
	if s.seen[cat] == nil {
		s.seen[cat] = map[string]bool{}
	}
	if s.seen[cat][key] {
		return false
	}
	s.seen[cat][key] = true
	return true
}

// Stats counts the different values masked, by category.
func (s *State) Stats() map[string]int {
	out := map[string]int{}
	for cat, vs := range s.seen {
		out[cat] = len(vs)
	}
	return out
}

// Total is the number of different values masked.
func (s *State) Total() int {
	n := 0
	for _, vs := range s.seen {
		n += len(vs)
	}
	return n
}

// Mapping lists the pseudonyms and their real values (never the secrets,
// which have none). Only the protected main window may show it.
func (s *State) Mapping() []Pair {
	out := append([]Pair(nil), s.pairs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cat < out[j].Cat })
	return out
}

// Erase forgets the values the state holds.
func (s *State) Erase() {
	s.names, s.pairs, s.seen, s.learned, s.isName = nil, nil, nil, nil, nil
}
