// Command tldr2pack turns tldr pages (https://github.com/tldr-pages/tldr,
// CC BY 4.0) into a Cmdex tool pack. It runs at build time on a local copy
// of the tldr repository; the app itself never reads tldr.
//
//	go run ./tools/tldr2pack -tldr ../tldr -page nmap -title Nmap -out packs/tools/nmap.json
//
// Texts are written as {"en": "...", "vi": ""} for translators to fill.
// When -out already exists, every translation, danger level and extra tag
// of a command that is still there (same platform and template) is kept,
// and commands that tldr no longer has are kept too (and listed).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thangkes/cmdex/internal/store"
)

func main() {
	var (
		root    = flag.String("tldr", "", "local copy of the tldr repository")
		pages   = flag.String("page", "", "tldr pages to read, comma separated (nmap,ncat)")
		dirs    = flag.String("platforms", "common,linux,windows", "tldr folders to read, in order")
		name    = flag.String("name", "", "pack name (default: the first page)")
		title   = flag.String("title", "", "pack title in English (default: the first page)")
		titleVi = flag.String("title-vi", "", "pack title in Vietnamese")
		tools   = flag.String("tools", "", "programs that bring the pack, comma separated (default: the pages)")
		out     = flag.String("out", "", "pack file to write (kept translations are read from it)")
		report  = flag.Bool("report", false, "only report how many texts of -out lack Vietnamese")
	)
	flag.Parse()
	if *report {
		if err := reportFile(os.Stdout, *out); err != nil {
			fail(err)
		}
		return
	}
	if *root == "" || *pages == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	names := splitList(*pages)
	opt := options{name: *name, title: *title, titleVi: *titleVi, tools: splitList(*tools)}
	if opt.name == "" {
		opt.name = names[0]
	}
	if opt.title == "" {
		opt.title = names[0]
	}
	if len(opt.tools) == 0 {
		opt.tools = names
	}
	var found []sourcePage
	for _, n := range names {
		for _, d := range splitList(*dirs) {
			b, err := os.ReadFile(filepath.Join(*root, "pages", d, n+".md"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				fail(err)
			}
			found = append(found, sourcePage{platform: platformOf(d), page: parsePage(string(b))})
		}
	}
	if len(found) == 0 {
		fail(fmt.Errorf("no page %s under %s/pages", *pages, *root))
	}
	p, warnings := build(found, opt)
	if old, err := os.ReadFile(*out); err == nil {
		op, err := store.ParsePack(old)
		if err != nil {
			fail(fmt.Errorf("%s: %w", *out, err))
		}
		warnings = append(warnings, merge(&p, op)...)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	b, err := encode(p)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("%s: %d commands\n", *out, len(p.Commands))
	reportFile(os.Stdout, *out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tldr2pack:", err)
	os.Exit(1)
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// platformOf maps a tldr folder to a Cmdex platform.
func platformOf(dir string) string {
	switch dir {
	case "linux":
		return store.Linux
	case "windows":
		return store.Windows
	}
	return store.Both
}

// ---- tldr pages -------------------------------------------------------------

type page struct {
	name, summary string
	examples      []example
}

type example struct{ desc, cmd string }

type sourcePage struct {
	platform string
	page     page
}

// parsePage reads a tldr page:
//
//	# nmap
//	> Network exploration tool.
//	> More information: <https://nmap.org>.
//	- Scan the top ports of a host:
//	`nmap {{host}}`
func parsePage(md string) page {
	var p page
	var summary []string
	desc := ""
	for _, l := range strings.Split(strings.ReplaceAll(md, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "# "):
			p.name = strings.TrimSpace(l[2:])
		case strings.HasPrefix(l, "> "):
			s := strings.TrimSpace(l[2:])
			if !strings.HasPrefix(s, "More information") && !strings.HasPrefix(s, "See also") && !strings.HasPrefix(s, "This command is an alias") {
				summary = append(summary, s)
			}
		case strings.HasPrefix(l, "- "):
			desc = strings.TrimSpace(l[2:])
		case len(l) > 2 && l[0] == '`' && l[len(l)-1] == '`' && desc != "":
			p.examples = append(p.examples, example{desc: desc, cmd: l[1 : len(l)-1]})
			desc = ""
		}
	}
	p.summary = strings.Join(summary, " ")
	return p
}

var (
	rePlaceholder = regexp.MustCompile(`\{\{(\{*.+?\}*)\}\}`)                            // {{{"key": "value"}}} holds braces
	reDescriptive = regexp.MustCompile(`^[a-z][a-z0-9_]*( [a-z][a-z0-9_]*)*( \.\.\.)?$`) // {{package}}, {{package1 package2 ...}}
	reWords       = regexp.MustCompile(`[a-z]+`)
	reOptionAlt   = regexp.MustCompile(`^\[([^|\]]*)\|([^\]]*)\]$`) // {{[-p|--port]}}
	reMnemonic    = regexp.MustCompile(`\[(\w+)\]`)                 // "[v]erbose"
	reNotKey      = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// converter turns one example's {{placeholders}} into Cmdex {params}.
type converter struct {
	keys   map[string]string // placeholder text -> key
	used   map[string]bool
	params []store.PackParam
}

func (c *converter) template(cmd string) string {
	return rePlaceholder.ReplaceAllStringFunc(cmd, func(m string) string {
		inner := m[2 : len(m)-2]
		if o := reOptionAlt.FindStringSubmatch(inner); o != nil {
			if o[2] != "" {
				return o[2] // the long option says what it does
			}
			return o[1]
		}
		if k, ok := c.keys[inner]; ok {
			return "{" + k + "}"
		}
		pp := describe(inner)
		base := pp.Key
		for i := 2; c.used[pp.Key]; i++ {
			pp.Key = fmt.Sprintf("%s%d", base, i)
		}
		c.used[pp.Key], c.keys[inner] = true, pp.Key
		c.params = append(c.params, pp)
		return "{" + pp.Key + "}"
	})
}

// describe makes a parameter of a placeholder's text. tldr writes either
// what to give ({{package}}, {{path/to/file}}) or an example to change
// ({{requirements.txt}}, {{8.5}}): an example becomes the default value.
func describe(inner string) store.PackParam {
	pp := store.PackParam{Type: "text", Required: true, Description: vi(inner)}
	if strings.Contains(inner, "|") && !strings.ContainsAny(inner, "{}") { // {{1|2|3}}: one of these
		choices := strings.Split(inner, "|")
		pp.Key, pp.Default, pp.Required = "choice", choices[0], false
		pp.Description = vi("one of: " + strings.Join(choices, ", "))
		return pp
	}
	word := strings.Fields(inner + " x")[0] // {{path/to/file1 path/to/file2 ...}}
	pathy := strings.HasPrefix(word, "path/to/") || strings.HasPrefix(word, `path\to\`)
	if i := strings.LastIndexAny(word, `/\`); i >= 0 {
		word = word[i+1:]
	}
	if j := strings.IndexByte(word, '.'); j > 0 {
		word = word[:j] // path/to/file.txt, requirements.txt: file, requirements
	}
	if !pathy && !reDescriptive.MatchString(inner) {
		pp.Default, pp.Required = inner, false
	}
	pp.Key = strings.Trim(reNotKey.ReplaceAllString(strings.ToLower(word), "_"), "_-")
	if len(pp.Key) > 32 {
		pp.Key = pp.Key[:32]
	}
	switch {
	case strings.Trim(inner, "0123456789") == "": // {{80}}
		pp.Key = "number"
	case pp.Key == "" || pp.Key[0] >= '0' && pp.Key[0] <= '9': // {{./...}}, {{8.5}}
		pp.Key = "value"
	}
	words := map[string]bool{}
	for _, w := range reWords.FindAllString(strings.ToLower(inner), -1) {
		words[w] = true
	}
	has := func(ws ...string) bool {
		for _, w := range ws {
			if words[w] {
				return true
			}
		}
		return false
	}
	switch {
	case has("password", "passphrase", "token", "secret"):
		pp.Type = "secret"
	case pathy || has("file", "filename", "directory", "folder", "dir") || strings.HasPrefix(inner, "/") || strings.HasPrefix(inner, "~"):
		pp.Type = "path"
	case has("host", "hostname", "ip", "domain", "url", "server", "address"):
		pp.Type = "host"
	case has("port", "count", "number", "seconds", "timeout", "size", "pid") || strings.Trim(inner, "0123456789") == "":
		pp.Type = "number"
	}
	return pp
}

func vi(en string) store.Text { return store.Text{"en": en, "vi": ""} }

type options struct {
	name, title, titleVi string
	tools                []string
}

// build makes the pack. Examples a store would refuse, and ones whose
// command has braces of its own (awk '{print}'), are left out with a
// warning: Cmdex would read those braces as parameters.
func build(pages []sourcePage, opt options) (store.Pack, []string) {
	title := store.Text{"en": opt.title, "vi": opt.titleVi}
	p := store.Pack{Name: opt.name, Title: title, Tools: opt.tools, Version: 1,
		Origin: "tldr-pages (CC BY 4.0), https://github.com/tldr-pages/tldr; adapted, with a Vietnamese translation"}
	var warnings []string
	seen := map[string]bool{}
	for _, sp := range pages {
		for _, ex := range sp.page.examples {
			c := converter{keys: map[string]string{}, used: map[string]bool{}}
			tpl := strings.TrimSpace(c.template(ex.cmd))
			if seen[tpl] {
				continue // the same command on another platform's page
			}
			seen[tpl] = true
			own := map[string]bool{}
			for _, pp := range c.params {
				own[pp.Key] = true
			}
			bad := ""
			for _, k := range store.PlaceholderKeys(tpl) {
				if !own[k] {
					bad = "{" + k + "} is not a tldr placeholder"
				}
			}
			if err := store.CheckCommandText(tpl); err != nil {
				bad = err.Error()
			}
			if bad != "" {
				warnings = append(warnings, fmt.Sprintf("left out %q: %s", ex.cmd, bad))
				continue
			}
			name := strings.TrimSuffix(reMnemonic.ReplaceAllString(ex.desc, "$1"), ":")
			platform := sp.platform
			if platform == store.Both { // "Activate the environment (Windows)"
				switch {
				case strings.Contains(name, "(Windows)"):
					platform = store.Windows
				case strings.Contains(name, "(Linux"):
					platform = store.Linux
				}
			}
			p.Commands = append(p.Commands, store.PackCommand{
				Name: vi(name), Platform: platform, Category: title, Template: tpl,
				Tags: []string{sp.page.name}, Params: c.params, Danger: store.DetectRisk(tpl, platform),
			})
		}
	}
	return p, warnings
}

// ---- keeping translations ----------------------------------------------------

func commandKey(c store.PackCommand) string { return c.Platform + " " + strings.TrimSpace(c.Template) }

// keep returns t with the texts of old that are not English (a copy: one
// title is every command's category).
func keep(t store.Text, old store.Text) store.Text {
	if t == nil && len(old) == 0 {
		return nil
	}
	out := store.Text{}
	for k, v := range t {
		out[k] = v
	}
	t = out
	for k, v := range old {
		if k != "en" && k != "" && v != "" {
			t[k] = v
		}
	}
	return t
}

// merge keeps what the old file had that tldr does not bring.
func merge(p *store.Pack, old store.Pack) []string {
	var warnings []string
	if old.Name != "" {
		p.Name = old.Name
	}
	p.Title = keep(p.Title, old.Title)
	if len(old.Tools) > 0 {
		p.Tools = old.Tools
	}
	p.Explain, p.Synonyms, p.Rules = old.Explain, old.Synonyms, old.Rules
	byKey := map[string]store.PackCommand{}
	for _, c := range old.Commands {
		byKey[commandKey(c)] = c
	}
	have := map[string]bool{}
	for i := range p.Commands {
		c := &p.Commands[i]
		have[commandKey(*c)] = true
		o, ok := byKey[commandKey(*c)]
		if !ok {
			continue
		}
		c.Name, c.Purpose, c.Notes = keep(c.Name, o.Name), keep(c.Purpose, o.Purpose), keep(c.Notes, o.Notes)
		c.Category = keep(c.Category, o.Category)
		c.Danger, c.Shell = max(c.Danger, o.Danger), o.Shell
		for _, t := range o.Tags {
			if !contains(c.Tags, t) {
				c.Tags = append(c.Tags, t)
			}
		}
		for j := range c.Params {
			for _, op := range o.Params {
				if op.Key == c.Params[j].Key {
					c.Params[j].Description = keep(c.Params[j].Description, op.Description)
					if op.Type != "" {
						c.Params[j].Type = op.Type
					}
				}
			}
		}
	}
	for _, c := range old.Commands {
		if !have[commandKey(c)] {
			p.Commands = append(p.Commands, c)
			warnings = append(warnings, fmt.Sprintf("kept %q: not in tldr (any more); delete it by hand if it should go", c.Template))
		}
	}
	return warnings
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ---- output --------------------------------------------------------------------

// encode writes the pack like the starter packs: UTF-8 with a BOM, one
// command per line.
func encode(p store.Pack) ([]byte, error) {
	var b bytes.Buffer
	b.Write(store.BOM)
	head := func(k string, v any) error {
		j, err := json.Marshal(v)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "  %q: %s,\n", k, j)
		return nil
	}
	b.WriteString("{\n")
	for _, f := range []struct {
		k string
		v any
	}{{"name", p.Name}, {"title", p.Title}, {"tools", p.Tools}, {"origin", p.Origin}, {"version", p.Version}} {
		if err := head(f.k, f.v); err != nil {
			return nil, err
		}
	}
	for _, f := range []struct {
		k string
		v any
		n int
	}{{"explain", p.Explain, len(p.Explain)}, {"synonyms", p.Synonyms, len(p.Synonyms)}, {"rules", p.Rules, len(p.Rules)}} {
		if f.n > 0 {
			if err := head(f.k, f.v); err != nil {
				return nil, err
			}
		}
	}
	b.WriteString("  \"commands\": [\n")
	for i, c := range p.Commands {
		j, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		b.WriteString("    ")
		b.Write(j)
		if i < len(p.Commands)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	if _, err := store.ParsePack(b.Bytes()); err != nil { // what Cmdex will read
		return nil, err
	}
	return b.Bytes(), nil
}

// untranslated counts the texts of p that have no Vietnamese.
func untranslated(p store.Pack) (missing, total int) {
	count := func(t store.Text) {
		if t.In("en") == "" {
			return
		}
		total++
		if !t.Has("vi") {
			missing++
		}
	}
	count(p.Title)
	for _, c := range p.Commands {
		count(c.Name)
		count(c.Purpose)
		count(c.Notes)
		for _, pp := range c.Params {
			count(pp.Description)
		}
	}
	return missing, total
}

func reportFile(w io.Writer, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p, err := store.ParsePack(b)
	if err != nil {
		return err
	}
	missing, total := untranslated(p)
	fmt.Fprintf(w, "%s: %d of %d texts still need Vietnamese\n", path, missing, total)
	return nil
}
