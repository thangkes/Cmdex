package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/thangkes/cmdex/internal/explain"
	"github.com/thangkes/cmdex/internal/store"
)

// Source is a shell history file Cmdex can read.
type Source struct {
	ID   string `json:"id"`   // powershell, bash, zsh
	Path string `json:"path"` // where it is on this computer
}

// Sources lists the history files of this user that exist.
func Sources() []Source {
	var out []Source
	add := func(id, p string) {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			out = append(out, Source{ID: id, Path: p})
		}
	}
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			add("powershell", filepath.Join(appdata, "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		add("bash", filepath.Join(home, ".bash_history"))
		add("zsh", filepath.Join(home, ".zsh_history"))
		add("powershell", filepath.Join(home, ".local", "share", "powershell", "PSReadLine", "ConsoleHost_history.txt"))
	}
	return out
}

// Collector reads new history lines into the store.
type Collector struct {
	Store *store.Store
}

// Rules prepared for one scan.
type rules struct {
	ignore   map[string]bool
	category map[string]string
	secret   []string
}

func (c *Collector) rules() rules {
	r := rules{ignore: map[string]bool{}, category: map[string]string{}}
	list, _ := c.Store.Rules()
	for _, x := range list {
		for _, m := range strings.Split(x.Match, ",") {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			switch x.Kind {
			case "ignore":
				r.ignore[strings.ToLower(m)] = true
			case "category":
				r.category[strings.ToLower(m)] = x.Result
			case "secret":
				r.secret = append(r.secret, m)
			}
		}
	}
	return r
}

// Add processes one command line from source (a history file, or a Cmdex
// session). It reports whether the line was kept.
func (c *Collector) Add(raw, source string) bool {
	return c.add(raw, source, c.rules())
}

func (c *Collector) add(raw, source string, r rules) bool {
	l, ok := Normalize(raw, r.secret)
	if !ok || r.ignore[l.Program] {
		return false
	}
	// every command is kept, "cd {path}" and a lone "cls" too: for someone
	// new to the terminal those are the first ones worth finding again
	target, err := c.Store.AddCollected(l.Signature, r.category[l.Program], source, l.Secret, l.Raw)
	if err != nil {
		return false
	}
	if target != 0 { // merged into a library command: the run counts there
		c.Store.AddUses(target, 1)
	}
	return true
}

// Scan reads what was appended to each enabled history file since the last
// scan. The first scan of a file reads all of it. When a file got shorter
// (the shell trimmed it), reading restarts at its new end, so nothing is
// counted twice.
func (c *Collector) Scan(enabled func(id string) bool) (int, error) {
	r := c.rules()
	added := 0
	for _, src := range Sources() {
		if enabled(src.ID) {
			n, _ := c.scanPath(src.ID, src.Path, r)
			added += n
		}
	}
	return added, nil
}

func (c *Collector) scanPath(id, path string, r rules) (int, error) {
	key := "collect.offset." + path
	off, _ := strconv.ParseInt(c.Store.Setting(key, "0"), 10, 64)
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if fi.Size() < off {
		return 0, c.Store.SetSetting(key, strconv.FormatInt(fi.Size(), 10))
	}
	if fi.Size() == off {
		return 0, nil
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	lines, read := readLines(f, id)
	added := 0
	for _, l := range lines {
		if c.add(l, id, r) {
			added++
		}
	}
	return added, c.Store.SetSetting(key, strconv.FormatInt(off+read, 10))
}

// readLines returns the complete lines read (a trailing partial line is left
// for the next scan) and how many bytes they took.
func readLines(r io.Reader, kind string) ([]string, int64) {
	br := bufio.NewReaderSize(r, 64<<10)
	var out []string
	var read int64
	var cont strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil { // no newline yet: the shell is still writing it
			break
		}
		read += int64(len(line))
		line = strings.TrimRight(line, "\r\n")
		switch kind {
		case "zsh":
			// ": 1700000000:0;command"
			if strings.HasPrefix(line, ": ") {
				if i := strings.IndexByte(line, ';'); i > 0 {
					line = line[i+1:]
				}
			}
		case "bash":
			if strings.HasPrefix(line, "#") && isDigits(line[1:]) { // HISTTIMEFORMAT stamp
				continue
			}
		}
		// PowerShell continues lines with a trailing backtick, zsh and bash with a backslash
		if strings.HasSuffix(line, "`") || strings.HasSuffix(line, `\`) {
			cont.WriteString(strings.TrimRight(line[:len(line)-1], " ") + " ")
			continue
		}
		if cont.Len() > 0 {
			line = cont.String() + strings.TrimSpace(line)
			cont.Reset()
		}
		out = append(out, line)
	}
	return out, read
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Match is how a collected template relates to the library.
type Match struct {
	Kind    string `json:"kind"` // existing, near, new
	Target  int64  `json:"target"`
	Name    string `json:"name"`
	Percent int    `json:"percent"`
}

// DefaultSimilarity is the share of words (percent) from which a collected
// template is offered as resembling a library command, unless the user sets
// another level in the Collect settings.
const DefaultSimilarity = 75

// MatchLibrary finds the library command a template duplicates (same shape)
// or resembles (same program, at least minPercent of words in common).
func MatchLibrary(sig string, cmds []store.Command, minPercent int) Match {
	shape := Shape(sig)
	prog := firstProgram(sig)
	best := Match{Kind: "new"}
	bestSim := 0.0
	for _, c := range cmds {
		if Shape(c.Template) == shape {
			return Match{Kind: "existing", Target: c.ID, Name: c.Name, Percent: 100}
		}
		if firstProgram(c.Template) != prog {
			continue
		}
		s := Similarity(sig, c.Template)
		if p := int(s*100 + 0.5); p >= minPercent && s > bestSim {
			bestSim = s
			best = Match{Kind: "near", Target: c.ID, Name: c.Name, Percent: p}
		}
	}
	return best
}

func firstProgram(tpl string) string {
	for _, tk := range explain.Lex(tpl) {
		if !tk.Op {
			return explain.Program(tk.Text)
		}
	}
	return ""
}

// scrubVersion is raised whenever secret masking learns new patterns, or
// templates are made differently, so lines collected by an older Cmdex are
// cleaned and regrouped again once. 3: places given to cd, dir, ls… are {path}.
const scrubVersion = "3"

// Backfill reads again, from their start, the parts of the history files
// that Cmdex 0.1 already scanned, for the lines it skipped then: lone
// programs ("ls", "cls") and the programs its starter rules ignored (cd,
// dir, pwd…). Lines it collected back then are not counted twice. It runs
// once.
func (c *Collector) Backfill(enabled func(id string) bool) (int, error) {
	return c.backfill(Sources(), enabled)
}

func (c *Collector) backfill(sources []Source, enabled func(id string) bool) (int, error) {
	if c.Store.Setting("collect.backfill", "") == "1" {
		return 0, nil
	}
	r := c.rules()
	added := 0
	for _, src := range sources {
		if !enabled(src.ID) {
			continue
		}
		off, _ := strconv.ParseInt(c.Store.Setting("collect.offset."+src.Path, "0"), 10, 64)
		if off <= 0 {
			continue // never scanned: the next scan reads all of it
		}
		f, err := os.Open(src.Path)
		if err != nil {
			continue
		}
		lines, _ := readLines(io.LimitReader(f, off), src.ID)
		f.Close()
		for _, raw := range lines {
			if l, ok := Normalize(raw, r.secret); ok && skippedBefore(l) && c.add(raw, src.ID, r) {
				added++
			}
		}
	}
	return added, c.Store.SetSetting("collect.backfill", "1")
}

// oldIgnored is what the starter rules of Cmdex 0.1 never collected.
var oldIgnored = map[string]bool{"cd": true, "ls": true, "dir": true, "cls": true, "clear": true, "exit": true, "pwd": true,
	"history": true, "whoami": true, "logout": true, "ll": true, "la": true}

// skippedBefore reports whether Cmdex 0.1 left a line out.
func skippedBefore(l Line) bool {
	return oldIgnored[l.Program] || (!strings.Contains(l.Signature, " ") && !strings.Contains(l.Signature, "{"))
}

// Rescrub masks, with today's rules, secrets in lines collected by an older
// version, and regroups them. It runs once per scrubVersion.
func (c *Collector) Rescrub() (int, error) {
	if c.Store.Setting("collect.scrub", "1") == scrubVersion {
		return 0, nil
	}
	fixLine, fixTemplate := c.fixers()
	n, err := c.Store.RewriteVariants(fixLine)
	if err != nil {
		return n, err
	}
	m, err := c.Store.RewriteTemplates(fixTemplate)
	if err != nil {
		return n + m, err
	}
	return n + m, c.Store.SetSetting("collect.scrub", scrubVersion)
}

// fixers clean a collected line (mask, regroup) and a collected template
// (mask) with today's rules.
func (c *Collector) fixers() (func(string) (string, string, bool, bool), func(string) string) {
	r := c.rules()
	return func(raw string) (string, string, bool, bool) {
			l, ok := Normalize(raw, r.secret)
			return l.Raw, l.Signature, l.Secret, ok
		}, func(t string) string {
			out, _ := Redact(t, r.secret)
			return out
		}
}

// ScrubBackup does Rescrub's work in the backup copy of the database
// (store.BackupPath), once per scrubVersion: a copy made before the
// database was cleaned must not keep what was cleaned.
func (c *Collector) ScrubBackup() error {
	if c.Store.Setting("collect.bakScrub", "") == scrubVersion {
		return nil
	}
	fixLine, fixTemplate := c.fixers()
	if _, err := c.Store.ScrubBackup(fixLine, fixTemplate); err != nil {
		return err
	}
	return c.Store.SetSetting("collect.bakScrub", scrubVersion)
}
