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
	// a line that is only a program name ("cls", "exit") teaches nothing
	if !strings.Contains(l.Signature, " ") && !strings.Contains(l.Signature, "{") {
		return false
	}
	return c.Store.AddCollected(l.Signature, r.category[l.Program], source, l.Secret, l.Raw) == nil
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

// MatchLibrary finds the library command a template duplicates (same shape)
// or resembles (same program, at least 70% of words in common).
func MatchLibrary(sig string, cmds []store.Command) Match {
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
		if s := Similarity(sig, c.Template); s >= 0.7 && s > bestSim {
			bestSim = s
			best = Match{Kind: "near", Target: c.ID, Name: c.Name, Percent: int(s*100 + 0.5)}
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

// scrubVersion is raised whenever secret masking learns new patterns, so
// lines collected by an older Cmdex are cleaned again once.
const scrubVersion = "2"

// Rescrub masks, with today's rules, secrets in lines collected by an older
// version, and regroups them. It runs once per scrubVersion.
func (c *Collector) Rescrub() (int, error) {
	if c.Store.Setting("collect.scrub", "1") == scrubVersion {
		return 0, nil
	}
	r := c.rules()
	n, err := c.Store.RewriteVariants(func(raw string) (string, string, bool, bool) {
		l, ok := Normalize(raw, r.secret)
		return l.Raw, l.Signature, l.Secret, ok
	})
	if err != nil {
		return n, err
	}
	m, err := c.Store.RewriteTemplates(func(t string) string { out, _ := Redact(t, r.secret); return out })
	if err != nil {
		return n + m, err
	}
	return n + m, c.Store.SetSetting("collect.scrub", scrubVersion)
}
