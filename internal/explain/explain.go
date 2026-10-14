package explain

import (
	"regexp"
	"sort"
	"strings"

	"github.com/thangkes/cmdex/internal/store"
)

// Kinds of segments.
const (
	KindCmd     = "cmd"
	KindFlag    = "flag"
	KindArg     = "arg"
	KindOp      = "op"
	KindUnknown = "unknown"
)

// Segment is one explained part of a command line.
type Segment struct {
	Start int    `json:"start"` // rune offsets into the line
	End   int    `json:"end"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Known bool   `json:"known"` // the text comes from the library or help, not a guess
}

// Result is the breakdown of a command line.
type Result struct {
	Line     string    `json:"line"`
	Segments []Segment `json:"segments"`
	Programs []string  `json:"programs"` // in pipeline order
	Missing  []string  `json:"missing"`  // programs nothing is known about
}

// Lookup returns what is known about a program (by Program name).
type Lookup func(program string) (store.Explain, bool)

// Messages are the few generic texts, in the interface language.
type Messages struct {
	UnknownCmd, UnknownFlag, Arg, IPv4, Port, UserAt, Wrapper string
	Ops                                                       map[string]string
}

// English and Vietnamese messages.
var (
	MessagesEN = Messages{
		UnknownCmd: "Not in your library yet — add a note when you save it", UnknownFlag: "Option not explained yet",
		Arg: "Argument", IPv4: "IPv4 address", Port: "port %s", UserAt: "user %s on host %s", Wrapper: "runs the command after it",
		Ops: map[string]string{
			"|": "Pipe: the output of the left side feeds the right side", "&&": "Run the next command only if this one succeeds",
			"||": "Run the next command only if this one fails", ";": "Run one after another", ">": "Write the output to a file (overwrite)",
			">>": "Append the output to a file", "2>": "Write errors to a file", "2>&1": "Send errors where the output goes",
			"<": "Read input from a file", "&": "Run in the background",
		},
	}
	MessagesVI = Messages{
		UnknownCmd: "Chưa có trong thư viện — thêm ghi chú khi lưu lệnh", UnknownFlag: "Tùy chọn chưa có giải nghĩa",
		Arg: "Đối số", IPv4: "địa chỉ IPv4", Port: "cổng %s", UserAt: "tài khoản %s trên máy %s", Wrapper: "chạy lệnh đứng sau nó",
		Ops: map[string]string{
			"|": "Pipe: kết quả lệnh trước làm đầu vào cho lệnh sau", "&&": "Chạy lệnh sau chỉ khi lệnh trước thành công",
			"||": "Chạy lệnh sau chỉ khi lệnh trước lỗi", ";": "Chạy lần lượt", ">": "Ghi kết quả ra file (ghi đè)",
			">>": "Ghi thêm vào cuối file", "2>": "Ghi lỗi ra file", "2>&1": "Gộp lỗi vào cùng chỗ với kết quả",
			"<": "Lấy đầu vào từ file", "&": "Chạy ngầm",
		},
	}
)

// wrappers run the command that follows them.
var wrappers = map[string]bool{"sudo": true, "doas": true, "nohup": true, "time": true, "nice": true, "env": true}

// wrapperValue lists wrapper options that take a value, so the value is not
// mistaken for the program to run.
var wrapperValue = map[string]map[string]bool{
	"sudo": {"-u": true, "-g": true, "-C": true, "-h": true, "-p": true, "-r": true, "-t": true, "-U": true, "-D": true},
	"doas": {"-u": true, "-C": true},
	"nice": {"-n": true},
}

var (
	reIPv4   = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)
	rePort   = regexp.MustCompile(`^:(\d{1,5})$`)
	reUserAt = regexp.MustCompile(`^([^@\s]+)@([^@\s]+)$`)
	reFlag   = regexp.MustCompile(`^(--?|/)[A-Za-z?]`)
)

// Options change how a line is read.
type Options struct {
	// Keywords: the line is a device command made of keywords that may be
	// abbreviated to any unique beginning, as on Cisco ("sh ip int br").
	// The program's Flags are then the keywords that may follow it.
	Keywords bool
	// Programs are the commands known on that device, to resolve an
	// abbreviated command ("conf" for "configure").
	Programs []string
}

// keywordWrappers, on keyword devices, run the command after them: "no"
// undoes a setting, "do" runs an exec command from configuration mode.
var keywordWrappers = map[string]bool{"no": true, "do": true}

// Explain breaks line down.
func Explain(line string, lookup Lookup, m Messages) Result {
	return ExplainWith(line, lookup, m, Options{})
}

// ExplainWith breaks line down as opts says.
func ExplainWith(line string, lookup Lookup, m Messages, opts Options) Result {
	toks := Lex(line)
	res := Result{Line: line, Segments: []Segment{}, Programs: []string{}, Missing: []string{}}
	var cur *store.Explain
	argi := 0
	newCmd := true
	for i := 0; i < len(toks); i++ {
		tk := toks[i]
		if tk.Op {
			res.Segments = append(res.Segments, Segment{tk.Start, tk.End, KindOp, tk.Text, m.Ops[tk.Text], true})
			cur, newCmd = nil, true
			continue
		}
		if newCmd {
			prog := Program(tk.Text)
			if opts.Keywords {
				prog = Expand(strings.ToLower(tk.Text), opts.Programs)
			}
			res.Programs = append(res.Programs, prog)
			e, ok := lookup(prog)
			if opts.Keywords && keywordWrappers[prog] {
				seg := Segment{Start: tk.Start, End: tk.End, Kind: KindCmd, Title: tk.Text, Text: e.Summary, Known: ok}
				if !ok {
					seg.Text = m.Wrapper
				}
				res.Segments = append(res.Segments, seg)
				continue // "no shutdown": the next word is a command again
			}
			seg := Segment{Start: tk.Start, End: tk.End, Kind: KindCmd, Title: tk.Text, Text: e.Summary, Known: ok}
			if !ok {
				seg.Kind, seg.Text = KindUnknown, m.UnknownCmd
				res.Missing = append(res.Missing, prog)
				e = store.Explain{}
			}
			if wrappers[prog] {
				if seg.Text == "" || !ok {
					seg.Text = m.Wrapper
				}
				seg.Kind, seg.Known = KindCmd, true
				res.Segments = append(res.Segments, seg)
				// sudo -u root apt ...: options of the wrapper, then a new program
				for i+1 < len(toks) && !toks[i+1].Op && reFlag.MatchString(toks[i+1].Text) {
					i++
					fs := flagSegment(toks, &i, &e, m)
					if fs.End == toks[i].End && fs.Title == toks[i].Text && wrapperValue[prog][toks[i].Text] && i+1 < len(toks) && !toks[i+1].Op {
						i++ // "sudo -u root": root is the option's value, not the program
						fs.End, fs.Title = toks[i].End, fs.Title+" "+toks[i].Text
					}
					res.Segments = append(res.Segments, fs)
				}
				if prog == "env" {
					for i+1 < len(toks) && strings.Contains(toks[i+1].Text, "=") {
						i++
						res.Segments = append(res.Segments, Segment{toks[i].Start, toks[i].End, KindArg, toks[i].Text, m.Arg, false})
					}
				}
				continue
			}
			res.Segments = append(res.Segments, seg)
			ee := e
			cur, newCmd, argi = &ee, false, 0
			continue
		}
		if opts.Keywords {
			if seg, ok := keywordSegment(toks, &i, cur); ok {
				res.Segments = append(res.Segments, seg)
				continue
			}
		} else if isFlag(cur, tk.Text) {
			res.Segments = append(res.Segments, flagSegment(toks, &i, cur, m))
			continue
		}
		res.Segments = append(res.Segments, argSegment(tk, cur, argi, m))
		argi++
	}
	return res
}

// Expand resolves an abbreviated word among names: the word itself when it
// is one of them, else the shortest name it begins (Cisco runs "int" as
// "interface" and "sh" as "show"); unchanged when nothing matches.
func Expand(w string, names []string) string {
	best := ""
	for _, n := range names {
		if n == w {
			return w
		}
		if strings.HasPrefix(n, w) && (best == "" || len(n) < len(best) || (len(n) == len(best) && n < best)) {
			best = n
		}
	}
	if best == "" {
		return w
	}
	return best
}

// keywordSegment explains a keyword of a device command, written in full
// or abbreviated. A keyword whose Flag has Value takes the next word as its
// value ("vlan 10").
func keywordSegment(toks []Token, i *int, e *store.Explain) (Segment, bool) {
	tk := toks[*i]
	if e == nil || len(e.Flags) == 0 {
		return Segment{}, false
	}
	names := make([]string, 0, len(e.Flags))
	for k := range e.Flags {
		names = append(names, k)
	}
	sort.Strings(names)
	key := Expand(strings.ToLower(tk.Text), names)
	f, ok := e.Flags[key]
	if !ok {
		return Segment{}, false
	}
	seg := Segment{Start: tk.Start, End: tk.End, Kind: KindFlag, Title: tk.Text, Text: f.Text, Known: true}
	if f.Value {
		v := ""
		if *i+1 < len(toks) && !toks[*i+1].Op {
			*i++
			v = toks[*i].Text
			seg.End, seg.Title = toks[*i].End, tk.Text+" "+v
		}
		seg.Text = strings.ReplaceAll(f.Text, "{v}", v)
	}
	return seg, true
}

var reSlashFlag = regexp.MustCompile(`^/([A-Za-z?]{1,2}|[A-Za-z]+:.*)$`)

// isFlag: "-a", "--all" and Windows "/i", "/FI:x" are options; "/etc/hosts"
// and "/tmp" are paths unless the program lists them as options.
func isFlag(e *store.Explain, w string) bool {
	if !strings.HasPrefix(w, "/") {
		return reFlag.MatchString(w)
	}
	if e != nil {
		if _, _, ok := findFlag(e, splitValue(w)); ok {
			return true
		}
	}
	return reSlashFlag.MatchString(w)
}

// findFlag looks an option up as written ("-L" and "-l" differ on Linux),
// then in lower case (Windows options like /FI and /fi are the same).
func findFlag(e *store.Explain, name string) (store.Flag, string, bool) {
	if f, ok := e.Flags[name]; ok {
		return f, name, true
	}
	if f, ok := e.Flags[strings.ToLower(name)]; ok {
		return f, strings.ToLower(name), true
	}
	return store.Flag{}, "", false
}

// splitValue returns the option part of "--port=22" or "/FI:x".
func splitValue(w string) string {
	if i := strings.IndexAny(w, "=:"); i > 1 {
		return w[:i]
	}
	return w
}

func flagSegment(toks []Token, i *int, e *store.Explain, m Messages) Segment {
	tk := toks[*i]
	seg := Segment{Start: tk.Start, End: tk.End, Kind: KindFlag, Title: tk.Text, Text: m.UnknownFlag}
	if e == nil {
		return seg
	}
	name := splitValue(tk.Text)
	if f, _, ok := findFlag(e, name); ok {
		seg.Known = true
		seg.Text = f.Text
		if f.Value {
			v := ""
			if name != tk.Text { // --port=22, /R:2
				v = tk.Text[len(name)+1:]
			} else if *i+1 < len(toks) && !toks[*i+1].Op {
				*i++
				v = toks[*i].Text
				seg.End, seg.Title = toks[*i].End, tk.Text+" "+v
			}
			seg.Text = strings.ReplaceAll(f.Text, "{v}", v)
		}
		return seg
	}
	// combined single letters: -ano, -tulpn
	if len(e.Letters) > 0 && len(tk.Text) >= 2 && tk.Text[0] == '-' && tk.Text[1] != '-' {
		var parts []string
		known := true
		for _, r := range tk.Text[1:] {
			if d, ok := e.Letters[string(r)]; ok {
				parts = append(parts, string(r)+": "+d)
			} else {
				parts = append(parts, string(r)+": ?")
				known = false
			}
		}
		seg.Text, seg.Known = strings.Join(parts, " · "), known
		if len(tk.Text) == 2 && known { // "-I": just the meaning, no "I:" prefix
			seg.Text = e.Letters[tk.Text[1:]]
		}
	}
	return seg
}

func argSegment(tk Token, e *store.Explain, argi int, m Messages) Segment {
	seg := Segment{Start: tk.Start, End: tk.End, Kind: KindArg, Title: tk.Text, Text: m.Arg}
	if e != nil && len(e.Args) > 0 {
		if argi >= len(e.Args) {
			argi = len(e.Args) - 1
		}
		seg.Text, seg.Known = e.Args[argi], true
	}
	w := strings.Trim(tk.Text, `"'`)
	switch {
	case reUserAt.MatchString(w):
		p := reUserAt.FindStringSubmatch(w)
		seg.Text = fmtS(m.UserAt, p[1], p[2])
	case reIPv4.MatchString(w):
		seg.Text += " — " + m.IPv4
	case rePort.MatchString(w):
		seg.Text += " — " + fmtS(m.Port, rePort.FindStringSubmatch(w)[1])
	}
	return seg
}

func fmtS(f string, args ...string) string {
	for _, a := range args {
		f = strings.Replace(f, "%s", a, 1)
	}
	return f
}
