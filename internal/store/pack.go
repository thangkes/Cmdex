package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Text is a string that a pack may give in several languages:
// "plain text" or {"en": "...", "vi": "..."}.
type Text map[string]string

// UnmarshalJSON accepts a plain string or a language map.
func (t *Text) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = Text{"": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*t = m
	return nil
}

// MarshalJSON writes a single-language text as a plain string.
func (t Text) MarshalJSON() ([]byte, error) {
	if len(t) == 1 {
		for _, v := range t {
			return json.Marshal(v)
		}
	}
	return json.Marshal(map[string]string(t))
}

// In returns the text in lang, falling back to English, then to any.
func (t Text) In(lang string) string {
	if v, ok := t[lang]; ok {
		return v
	}
	if v, ok := t[""]; ok {
		return v
	}
	if v, ok := t["en"]; ok {
		return v
	}
	for _, v := range t {
		return v
	}
	return ""
}

// Pack is a shareable set of commands, explanations, synonyms and rules:
// the starter packs shipped with Cmdex and the files Import/Export use.
type Pack struct {
	Name     string              `json:"name"`
	Version  int                 `json:"version"`
	Commands []PackCommand       `json:"commands"`
	Explain  []PackExplain       `json:"explain,omitempty"`
	Synonyms map[string][]string `json:"synonyms,omitempty"`
	Rules    []PackRule          `json:"rules,omitempty"`
}

// PackRule is a classification rule in a pack; the group name may be
// given in several languages.
type PackRule struct {
	Kind   string `json:"kind"`
	Match  string `json:"match"`
	Result Text   `json:"result,omitempty"`
}

// PackCommand is a command in a pack.
type PackCommand struct {
	Name     Text        `json:"name"`
	Platform string      `json:"platform"`
	Shell    string      `json:"shell,omitempty"`
	Category Text        `json:"category,omitempty"`
	Template string      `json:"template"`
	Purpose  Text        `json:"purpose,omitempty"`
	Notes    Text        `json:"notes,omitempty"`
	Danger   int         `json:"danger,omitempty"`
	Tags     []string    `json:"tags,omitempty"`
	Params   []PackParam `json:"params,omitempty"`
}

// PackParam is a template parameter in a pack.
type PackParam struct {
	Key         string `json:"key"`
	Type        string `json:"type,omitempty"`
	Default     string `json:"default,omitempty"`
	Description Text   `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PackExplain explains one program in a pack.
type PackExplain struct {
	Cmd     string              `json:"cmd"`
	Summary Text                `json:"summary"`
	Flags   map[string]PackFlag `json:"flags,omitempty"`
	Letters map[string]Text     `json:"letters,omitempty"`
	Args    []Text              `json:"args,omitempty"`
}

// PackFlag explains one option in a pack.
type PackFlag struct {
	Text  Text `json:"text"`
	Value bool `json:"value,omitempty"`
}

// ImportResult says what an import changed.
type ImportResult struct {
	Added    int `json:"added"`
	Skipped  int `json:"skipped"` // already in the library
	Explains int `json:"explains"`
}

// ParsePack reads a pack file.
func ParsePack(b []byte) (Pack, error) {
	var p Pack
	if err := json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	if len(p.Commands) == 0 && len(p.Explain) == 0 {
		return p, errors.New("the file has no commands")
	}
	return p, nil
}

// ImportPack adds a pack's content in language lang. Commands already in the
// library (same platform and template) are left as they are, and so are
// explanations the user wrote or edited.
func (s *Store) ImportPack(p Pack, lang string) (ImportResult, error) {
	var r ImportResult
	source := "user"
	if p.Name != "" {
		source = "pack:" + p.Name
	}
	for _, pc := range p.Commands {
		c := Command{
			Name: pc.Name.In(lang), Platform: pc.Platform, Shell: pc.Shell, Category: pc.Category.In(lang),
			Template: pc.Template, Purpose: pc.Purpose.In(lang), Notes: pc.Notes.In(lang), Danger: pc.Danger,
			Tags: pc.Tags, Source: source, Params: []Param{},
		}
		for _, pp := range pc.Params {
			c.Params = append(c.Params, Param{Key: pp.Key, Type: pp.Type, Default: pp.Default, Description: pp.Description.In(lang), Required: pp.Required})
		}
		if len(c.Params) == 0 {
			c.Params = ParamsFromTemplate(c.Template)
		}
		_, err := s.SaveCommand(c)
		switch {
		case errors.Is(err, ErrDuplicate):
			r.Skipped++
		case err != nil:
			return r, err
		default:
			r.Added++
		}
	}
	for _, pe := range p.Explain {
		if old, ok := s.Explain(pe.Cmd); ok && !strings.HasPrefix(old.Source, "pack:") {
			continue // the user's own explanation wins
		}
		e := Explain{Cmd: pe.Cmd, Summary: pe.Summary.In(lang), Flags: map[string]Flag{}, Letters: map[string]string{}, Source: source}
		for k, f := range pe.Flags {
			e.Flags[k] = Flag{Text: f.Text.In(lang), Value: f.Value}
		}
		for k, v := range pe.Letters {
			e.Letters[k] = v.In(lang)
		}
		for _, a := range pe.Args {
			e.Args = append(e.Args, a.In(lang))
		}
		if err := s.SaveExplain(e); err != nil {
			return r, err
		}
		r.Explains++
	}
	if len(p.Synonyms) > 0 {
		have, err := s.Synonyms()
		if err != nil {
			return r, err
		}
		for w, alts := range p.Synonyms {
			merged := append([]string{}, have[w]...)
			for _, a := range alts {
				if !contains(merged, a) {
					merged = append(merged, a)
				}
			}
			if err := s.SetSynonym(w, merged); err != nil {
				return r, err
			}
		}
	}
	if len(p.Rules) > 0 {
		have, err := s.Rules()
		if err != nil {
			return r, err
		}
		for _, pr := range p.Rules {
			dup := false
			for _, h := range have {
				if h.Kind == pr.Kind && h.Match == pr.Match {
					dup = true
				}
			}
			if !dup {
				if _, err := s.SaveRule(Rule{Kind: pr.Kind, Match: pr.Match, Result: pr.Result.In(lang)}); err != nil {
					return r, err
				}
			}
		}
	}
	return r, nil
}

// ExportPack writes the whole library (commands, explanations, synonyms,
// rules) as a pack another computer can import.
func (s *Store) ExportPack() (Pack, error) {
	p := Pack{Name: "export", Version: 1}
	cmds, err := s.Commands()
	if err != nil {
		return p, err
	}
	for _, c := range cmds {
		pc := PackCommand{Name: Text{"": c.Name}, Platform: c.Platform, Shell: c.Shell, Category: Text{"": c.Category}, Template: c.Template,
			Purpose: Text{"": c.Purpose}, Notes: Text{"": c.Notes}, Danger: c.Danger, Tags: c.Tags}
		for _, pr := range c.Params {
			pc.Params = append(pc.Params, PackParam{Key: pr.Key, Type: pr.Type, Default: pr.Default, Description: Text{"": pr.Description}, Required: pr.Required})
		}
		p.Commands = append(p.Commands, pc)
	}
	rows, err := s.db.Query("SELECT cmd FROM explain ORDER BY cmd")
	if err != nil {
		return p, err
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		e, _ := s.Explain(n)
		pe := PackExplain{Cmd: e.Cmd, Summary: Text{"": e.Summary}, Flags: map[string]PackFlag{}, Letters: map[string]Text{}}
		for k, f := range e.Flags {
			pe.Flags[k] = PackFlag{Text: Text{"": f.Text}, Value: f.Value}
		}
		for k, v := range e.Letters {
			pe.Letters[k] = Text{"": v}
		}
		for _, a := range e.Args {
			pe.Args = append(pe.Args, Text{"": a})
		}
		p.Explain = append(p.Explain, pe)
	}
	if p.Synonyms, err = s.Synonyms(); err != nil {
		return p, err
	}
	rules, err := s.Rules()
	if err != nil {
		return p, err
	}
	for _, r := range rules {
		p.Rules = append(p.Rules, PackRule{Kind: r.Kind, Match: r.Match, Result: Text{"": r.Result}})
	}
	return p, nil
}

// LocalizePack rewrites, in language lang, the commands of the pack that the
// user never edited, the pack's explanations and the group names of its
// rules. Whatever the user changed stays as it is. It returns how many
// commands changed.
func (s *Store) LocalizePack(p Pack, lang string) (int, error) {
	if p.Name == "" {
		return 0, nil
	}
	source := "pack:" + p.Name
	n := 0
	for _, pc := range p.Commands {
		params := []Param{}
		for _, pp := range pc.Params {
			params = append(params, Param{Key: pp.Key, Type: pp.Type, Default: pp.Default, Description: pp.Description.In(lang), Required: pp.Required})
		}
		if len(params) == 0 {
			params = ParamsFromTemplate(pc.Template)
		}
		pj, _ := json.Marshal(params)
		s.mu.Lock()
		res, err := s.db.Exec(`UPDATE commands SET name = ?, category = ?, purpose = ?, notes = ?, params = ?
			WHERE platform = ? AND template = ? AND source = ? AND updated = created`,
			pc.Name.In(lang), pc.Category.In(lang), pc.Purpose.In(lang), pc.Notes.In(lang), string(pj), pc.Platform, pc.Template, source)
		s.mu.Unlock()
		if err != nil {
			return n, err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			n++
		}
	}
	for _, pe := range p.Explain {
		if old, ok := s.Explain(pe.Cmd); ok && old.Source != source {
			continue
		}
		e := Explain{Cmd: pe.Cmd, Summary: pe.Summary.In(lang), Flags: map[string]Flag{}, Letters: map[string]string{}, Source: source}
		for k, f := range pe.Flags {
			e.Flags[k] = Flag{Text: f.Text.In(lang), Value: f.Value}
		}
		for k, v := range pe.Letters {
			e.Letters[k] = v.In(lang)
		}
		for _, a := range pe.Args {
			e.Args = append(e.Args, a.In(lang))
		}
		if err := s.SaveExplain(e); err != nil {
			return n, err
		}
	}
	for _, pr := range p.Rules {
		if len(pr.Result) == 0 {
			continue
		}
		words, _ := json.Marshal(textValues(pr.Result))
		s.mu.Lock()
		// only a group name that is still one of the pack's own wordings
		_, err := s.db.Exec("UPDATE rules SET result = ? WHERE kind = ? AND match = ? AND result IN (SELECT value FROM json_each(?))",
			pr.Result.In(lang), pr.Kind, pr.Match, string(words))
		s.mu.Unlock()
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func textValues(t Text) []string {
	out := make([]string, 0, len(t))
	for _, v := range t {
		out = append(out, v)
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ParamsFromTemplate lists the {placeholders} of a template, in order, as
// text parameters.
func ParamsFromTemplate(tpl string) []Param {
	out := []Param{}
	seen := map[string]bool{}
	for _, k := range PlaceholderKeys(tpl) {
		if !seen[k] {
			seen[k] = true
			out = append(out, Param{Key: k, Type: "text", Required: true})
		}
	}
	return out
}

// PlaceholderKeys returns the names inside {braces} in order of appearance.
func PlaceholderKeys(tpl string) []string {
	var out []string
	for i := 0; i < len(tpl); i++ {
		if tpl[i] != '{' {
			continue
		}
		j := strings.IndexByte(tpl[i+1:], '}')
		if j <= 0 {
			continue
		}
		k := tpl[i+1 : i+1+j]
		if isKey(k) {
			out = append(out, k)
		}
		i += j + 1
	}
	return out
}

func isKey(k string) bool {
	for _, r := range k {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return k != ""
}

// Fill replaces each {key} in tpl with values[key]; unknown keys stay as they are.
func Fill(tpl string, values map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(tpl); i++ {
		if tpl[i] == '{' {
			if j := strings.IndexByte(tpl[i+1:], '}'); j > 0 {
				k := tpl[i+1 : i+1+j]
				if v, ok := values[k]; ok && isKey(k) && v != "" {
					b.WriteString(v)
					i += j + 1
					continue
				}
			}
		}
		b.WriteByte(tpl[i])
	}
	return b.String()
}
