// Package collect turns the command lines a user runs into reusable
// templates: secrets are hidden first, then values (hosts, ports, paths,
// numbers…) become {parameters}, so lines that differ only in values fall
// into one group waiting for the user's review.
package collect

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/thangkes/cmdex/internal/explain"
)

// Mask replaces every secret value that is kept in raw lines.
const Mask = "••••••"

// Line is one collected command line after processing.
type Line struct {
	Raw       string // as run, secrets masked
	Signature string // template with {parameters}
	Program   string // first program, for rules
	Secret    bool   // a secret was hidden
}

var (
	// key=value and key: value secrets, also in URLs and headers
	reKV = regexp.MustCompile(`(?i)((?:password|passwd|pwd|pass|secret|token|api[_-]?key|access[_-]?key|auth)\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&;]+)`)
	// --password X, --password=X, -Password X (PowerShell), --token X
	reFlagSecret = regexp.MustCompile(`(?i)((?:^|\s)--?(?:password|passwd|pass|token|secret|api-?key|access-?key)(?:\s+|=))("[^"]*"|'[^']*'|\S+)`)
	// Authorization: Bearer X / Basic X
	reBearer = regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic)\s+)("[^"]*"|'[^']*'|[^\s"']+)`)
	// scheme://user:pass@host
	reURLPass = regexp.MustCompile(`(://[^/\s:@]+:)([^@\s/]+)(@)`)
	// ConvertTo-SecureString "X" -AsPlainText
	reSecure = regexp.MustCompile(`(?i)(ConvertTo-SecureString\s+(?:-String\s+)?)("[^"]*"|'[^']*'|\S+)`)
	// sshpass -p X
	reSSHPass = regexp.MustCompile(`(?i)(\bsshpass\s+-p\s*)("[^"]*"|'[^']*'|\S+)`)
	// mysql/mysqldump/mariadb -pSECRET (glued; "-p" alone prompts and is fine)
	reMySQL = regexp.MustCompile(`(?i)(\b(?:mysql|mysqldump|mariadb|mysqladmin)\b[^|;&]*?\s-p)([^\s]+)`)
	// curl -u user:pass, --user user:pass, --proxy-user user:pass
	reUserPass = regexp.MustCompile(`(?i)((?:^|\s)(?:-u|-U|--user|--proxy-user)(?:\s+|=)["']?[^\s:"']+:)([^\s"']+)`)
	// a token as the user part of a URL: https://ghp_xxx@github.com
	reURLToken = regexp.MustCompile(`(://)([A-Za-z0-9_\-]{20,})(@)`)
	// cookies: Cookie: a=b; c=d  /  curl -b / --cookie
	reCookie = regexp.MustCompile(`(?i)((?:\bcookie:\s*|(?:^|\s)(?:-b|--cookie)(?:\s+|=)))("[^"]*"|'[^']*'|[^\s"']+)`)
	// aws configure set aws_secret_access_key VALUE (and other key/token/password settings)
	reConfigSet = regexp.MustCompile(`(?i)(\bconfigure\s+set\s+\S*(?:secret|token|password|key)\S*\s+)(\S+)`)
	// net use Z: \\server\share PASSWORD /user:x
	reNetUse = regexp.MustCompile(`(?i)(\bnet\s+use\s+(?:[A-Z]:\s+|\*\s+)?\\\\\S+\s+)([^\s/*][^\s]*)`)
	// docker / podman / helm / oc login -p X
	reLoginP = regexp.MustCompile(`(?i)(\b(?:docker|podman|helm|oc|nerdctl)\b[^|;&]*?\blogin\b[^|;&]*?\s(?:-p|--password)(?:\s+|=))("[^"]*"|'[^']*'|\S+)`)
	// well-known token shapes anywhere in a line
	reKnownToken = regexp.MustCompile(`()(\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_\-]{16,}|xox[abprs]-[A-Za-z0-9\-]{10,}|sk-[A-Za-z0-9_\-]{20,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_\-]{30,}|eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,})\b)`)
)

// Redact hides secrets in a command line. extra lists further option names
// whose value is secret (from the user's "secret" rules), like "-k" or "--pin".
func Redact(line string, extra []string) (string, bool) {
	secret := false
	mask := func(re *regexp.Regexp, s string) string {
		return re.ReplaceAllStringFunc(s, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if strings.Trim(sub[2], `"'`) == "" || strings.Contains(sub[2], Mask) {
				return m
			}
			secret = true
			rest := ""
			if len(sub) > 3 {
				rest = sub[3]
			}
			return sub[1] + Mask + rest
		})
	}
	for _, re := range []*regexp.Regexp{reURLPass, reURLToken, reBearer, reCookie, reSecure, reSSHPass, reMySQL, reUserPass,
		reConfigSet, reNetUse, reLoginP, reFlagSecret, reKV, reKnownToken} {
		line = mask(re, line)
	}
	for _, name := range extra {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		re, err := regexp.Compile(`((?:^|\s)` + regexp.QuoteMeta(name) + `(?:\s+|=))("[^"]*"|'[^']*'|\S+)`)
		if err == nil {
			line = mask(re, line)
		}
	}
	return line, secret
}

var (
	reIPv4       = regexp.MustCompile(`^\d{1,3}(?:\.\d{1,3}){3}$`)
	reIPv4Port   = regexp.MustCompile(`^(\d{1,3}(?:\.\d{1,3}){3}):(\d{1,5})$`)
	rePort       = regexp.MustCompile(`^:(\d{1,5})$`)
	reUserAt     = regexp.MustCompile(`^([^@\s/]+)@([^@\s/:]+)(?::(\d+))?$`)
	reURL        = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://\S+$`)
	reHost       = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	reNumber     = regexp.MustCompile(`^\d+$`)
	reFlagName   = regexp.MustCompile(`^(?:--?|/)([A-Za-z][\w-]*)$`)
	reRemotePath = regexp.MustCompile(`^(?:([^@\s/]+)@)?([A-Za-z0-9][\w.-]*):(/\S*|~\S*)$`)
	reMAC        = regexp.MustCompile(`(?i)^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$`)
)

var fileExt = map[string]bool{
	"txt": true, "log": true, "json": true, "yml": true, "yaml": true, "xml": true, "conf": true, "cfg": true, "ini": true, "csv": true,
	"md": true, "ps1": true, "sh": true, "py": true, "js": true, "go": true, "exe": true, "msi": true, "zip": true, "gz": true, "tgz": true,
	"tar": true, "deb": true, "rpm": true, "iso": true, "jpg": true, "png": true, "pdf": true, "html": true, "service": true, "bak": true, "db": true,
}

// Normalize turns a raw command line into a template. Program names keep
// their spelling but are lower-cased (Windows treats PING and ping alike);
// options, keywords and operators stay; values become {parameters}.
func Normalize(raw string, secretFlags []string) (Line, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || len(raw) > 600 {
		return Line{}, false
	}
	masked, secret := Redact(raw, secretFlags)
	toks := explain.Lex(masked)
	if len(toks) == 0 || toks[0].Op {
		return Line{}, false
	}
	names := map[string]int{}
	name := func(base string) string {
		names[base]++
		if n := names[base]; n > 1 {
			return "{" + base + strconv.Itoa(n) + "}"
		}
		return "{" + base + "}"
	}
	var out []string
	newCmd := true
	program := ""
	prevFlag := ""
	for _, tk := range toks {
		w := tk.Text
		if tk.Op {
			out = append(out, w)
			newCmd, prevFlag = true, ""
			continue
		}
		if newCmd {
			p := explain.Program(w)
			if program == "" {
				program = p
			}
			out = append(out, strings.ToLower(strings.Trim(w, `"'`)))
			newCmd = p == "sudo" || p == "doas" || p == "nohup" || p == "time"
			continue
		}
		if strings.Contains(w, Mask) {
			out = append(out, strings.Replace(w, Mask, name("secret"), 1))
			prevFlag = ""
			continue
		}
		if m := reFlagName.FindStringSubmatch(w); m != nil {
			out = append(out, w)
			prevFlag = strings.ToLower(m[1])
			continue
		}
		if strings.HasPrefix(w, "-") && strings.Contains(w, "=") { // --port=22
			i := strings.IndexByte(w, '=')
			v := valueParam(w[i+1:], strings.TrimLeft(strings.ToLower(w[:i]), "-"), name)
			out = append(out, w[:i+1]+v)
			prevFlag = ""
			continue
		}
		out = append(out, valueParam(w, prevFlag, name))
		prevFlag = ""
	}
	return Line{Raw: masked, Signature: strings.Join(out, " "), Program: program, Secret: secret}, true
}

// valueParam decides whether a word is a value, and which parameter.
func valueParam(w, afterFlag string, name func(string) string) string {
	u := strings.Trim(w, `"'`)
	quoted := u != w
	switch {
	case reURL.MatchString(u):
		return name("url")
	case reRemotePath.MatchString(u): // scp/rsync: user@host:/path
		m := reRemotePath.FindStringSubmatch(u)
		s := name("host") + ":" + name("path")
		if m[1] != "" {
			s = name("user") + "@" + s
		}
		return s
	case reUserAt.MatchString(u):
		m := reUserAt.FindStringSubmatch(u)
		s := name("user") + "@" + name("host")
		if m[3] != "" {
			s += ":" + name("port")
		}
		return s
	case reIPv4Port.MatchString(u):
		return name("host") + ":" + name("port")
	case reIPv4.MatchString(u):
		return name("host")
	case rePort.MatchString(u):
		return ":" + name("port")
	case reMAC.MatchString(u):
		return name("mac")
	case reNumber.MatchString(u):
		if afterFlag != "" && len(afterFlag) > 1 {
			return name(afterFlag)
		}
		if afterFlag != "" {
			return name(numberName(afterFlag))
		}
		return name("n")
	case isPath(u):
		return name("path")
	case reHost.MatchString(u) && !fileExt[strings.ToLower(u[strings.LastIndexByte(u, '.')+1:])]:
		return name("host")
	case quoted:
		return name("text")
	case afterFlag != "" && len(afterFlag) > 1:
		return name(afterFlag)
	}
	return w
}

// numberName gives single-letter options a readable parameter name when
// the meaning is near-universal; others keep the letter.
func numberName(flag string) string {
	switch flag {
	case "p":
		return "port"
	case "n", "c":
		return "count"
	case "t":
		return "timeout"
	}
	return flag
}

func isPath(s string) bool {
	switch {
	case strings.HasPrefix(s, "~/"), strings.HasPrefix(s, "./"), strings.HasPrefix(s, "../"):
		return true
	case len(s) > 2 && s[1] == ':' && (s[2] == '\\' || s[2] == '/'):
		return true // C:\x
	case strings.HasPrefix(s, `\\`):
		return true // \\server\share
	case strings.HasPrefix(s, "/") && len(s) > 1:
		return true
	case strings.Count(s, "/") >= 1 && !strings.Contains(s, "://") && strings.IndexByte(s, '/') > 0:
		return true // dir/file
	case strings.Count(s, `\`) >= 1:
		return true
	}
	return false
}

// Shape removes parameter names, so templates written with different names
// for the same values ({count} vs {n}) compare equal.
func Shape(template string) string {
	var b strings.Builder
	for i := 0; i < len(template); i++ {
		if template[i] == '{' {
			if j := strings.IndexByte(template[i+1:], '}'); j > 0 {
				b.WriteString("{}")
				i += j + 1
				continue
			}
		}
		b.WriteByte(template[i])
	}
	return strings.Join(strings.Fields(strings.ToLower(b.String())), " ")
}

// Similarity is the share of words two templates have in common (0..1),
// counting parameters by position only.
func Similarity(a, b string) float64 {
	wa, wb := strings.Fields(Shape(a)), strings.Fields(Shape(b))
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	count := map[string]int{}
	for _, w := range wa {
		count[w]++
	}
	common := 0
	for _, w := range wb {
		if count[w] > 0 {
			count[w]--
			common++
		}
	}
	return 2 * float64(common) / float64(len(wa)+len(wb))
}
