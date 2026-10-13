// Package explain breaks a command line into its parts (programs, options,
// values, operators) and says what each part does, using what the library
// knows and, failing that, the help already installed on this computer.
package explain

import (
	"path"
	"strings"
)

// Token is one word or operator of a command line. Start and End count
// characters (runes), which is what the interface positions labels by.
type Token struct {
	Text  string
	Start int
	End   int
	Op    bool
}

var operators = []string{"2>&1", "&&", "||", ">>", "2>", "|", ";", ">", "<", "&"}

// Lex splits a command line into words and operators. Quotes keep spaces
// inside a word; operators split words even without spaces ("ls|grep x").
func Lex(line string) []Token {
	rs := []rune(line)
	var out []Token
	i := 0
	for i < len(rs) {
		if rs[i] == ' ' || rs[i] == '\t' {
			i++
			continue
		}
		if op := opAt(rs, i); op != "" {
			n := len([]rune(op))
			out = append(out, Token{Text: op, Start: i, End: i + n, Op: true})
			i += n
			continue
		}
		start := i
		for i < len(rs) && rs[i] != ' ' && rs[i] != '\t' {
			if q := rs[i]; q == '"' || q == '\'' {
				i++
				for i < len(rs) && rs[i] != q {
					i++
				}
				if i < len(rs) {
					i++
				}
				continue
			}
			if opAt(rs, i) != "" {
				break
			}
			i++
		}
		out = append(out, Token{Text: string(rs[start:i]), Start: start, End: i})
	}
	return out
}

func opAt(rs []rune, i int) string {
	for _, op := range operators {
		o := []rune(op)
		if i+len(o) <= len(rs) && string(rs[i:i+len(o)]) == op {
			// "2>" only when the 2 starts a word: "file2>x" is not "2>"
			if o[0] == '2' && i > 0 && rs[i-1] != ' ' {
				continue
			}
			return op
		}
	}
	return ""
}

// Program turns the first word of a command into the name explanations are
// stored under: no path, no .exe, lower case. "C:\Windows\System32\PING.EXE"
// becomes "ping".
func Program(word string) string {
	w := strings.Trim(word, `"'`)
	w = strings.ReplaceAll(w, `\`, "/")
	w = path.Base(w)
	lw := strings.ToLower(w)
	for _, ext := range []string{".exe", ".com", ".cmd", ".bat", ".ps1"} {
		lw = strings.TrimSuffix(lw, ext)
	}
	return lw
}
