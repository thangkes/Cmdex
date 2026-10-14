package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/thangkes/cmdex/internal/explain"
	"github.com/thangkes/cmdex/internal/search"
	"github.com/thangkes/cmdex/internal/store"
)

// Commands returns the whole library.
func (a *App) Commands() ([]store.Command, error) { return a.store.Commands() }

// SaveCommand adds or updates a command and returns its ID.
func (a *App) SaveCommand(c store.Command) (int64, error) {
	if c.Params == nil {
		c.Params = store.ParamsFromTemplate(c.Template)
	}
	return a.store.SaveCommand(c)
}

// DeleteCommand removes a command.
func (a *App) DeleteCommand(id int64) error { return a.store.DeleteCommand(id) }

// TemplateParams lists the parameters a template uses, keeping what is
// already known about each from current.
func (a *App) TemplateParams(template string, current []store.Param) []store.Param {
	have := map[string]store.Param{}
	for _, p := range current {
		have[p.Key] = p
	}
	out := []store.Param{}
	for _, p := range store.ParamsFromTemplate(template) {
		if old, ok := have[p.Key]; ok {
			p = old
		}
		out = append(out, p)
	}
	return out
}

// Fill builds the command line from a template and parameter values.
func (a *App) Fill(template string, values map[string]string) string {
	return store.Fill(template, values)
}

// Search ranks the library against a request. platform ("windows", "linux"
// or "") puts commands for the active session first.
func (a *App) Search(query, platform string, limit int) ([]search.Result, error) {
	cmds, err := a.store.Commands()
	if err != nil {
		return nil, err
	}
	syn, _ := a.store.Synonyms()
	res := search.Rank(cmds, query, normalizeSyn(syn), platform, limit)
	if res == nil {
		res = []search.Result{}
	}
	return res, nil
}

func normalizeSyn(syn map[string][]string) map[string][]string {
	out := make(map[string][]string, len(syn))
	for k, v := range syn {
		out[search.Normalize(k)] = v
	}
	return out
}

// LooksLikeCommand tells the page whether the request box holds a command
// line (explain it) or plain words (search).
func (a *App) LooksLikeCommand(text string) bool {
	toks := explain.Lex(text)
	if len(toks) == 0 {
		return false
	}
	prog := explain.Program(toks[0].Text)
	if _, ok := a.store.Explain(prog); ok {
		return true
	}
	for _, t := range toks[1:] {
		if t.Op || (len(t.Text) > 1 && (t.Text[0] == '-' || (t.Text[0] == '/' && len(t.Text) <= 4))) {
			return true
		}
	}
	return false
}

// Explain breaks a command line down. Programs the library knows nothing
// about are looked up once in the help installed on this computer, and what
// is found is kept for next time.
func (a *App) Explain(line string) explain.Result {
	msgs := explain.MessagesEN
	if a.lang() == "vi" {
		msgs = explain.MessagesVI
	}
	lookup := func(p string) (store.Explain, bool) { return a.store.Explain(p) }
	res := explain.Explain(line, lookup, msgs)
	learned := false
	for _, prog := range res.Missing {
		if a.store.Setting("help.miss."+prog, "") != "" {
			continue
		}
		if e, ok := explain.FromHelp(context.Background(), prog); ok {
			a.store.SaveExplain(e)
			learned = true
		} else {
			a.store.SetSetting("help.miss."+prog, "1") // do not run help again for it
		}
	}
	if learned {
		res = explain.Explain(line, lookup, msgs)
	}
	return res
}

// GetExplain returns what is known about one program, for editing.
func (a *App) GetExplain(program string) store.Explain {
	e, ok := a.store.Explain(explain.Program(program))
	if !ok {
		e = store.Explain{Cmd: explain.Program(program), Flags: map[string]store.Flag{}, Letters: map[string]string{}, Args: []string{}}
	}
	return e
}

// SaveExplain stores the user's own explanation of a program.
func (a *App) SaveExplain(e store.Explain) error {
	e.Source = "user"
	return a.store.SaveExplain(e)
}

// ReimportStarter brings back the default commands the user deleted
// (edited ones are left alone).
func (a *App) ReimportStarter() (store.ImportResult, error) {
	n, err := a.store.RestoreDefaults()
	return store.ImportResult{Added: n}, err
}

// ImportFile asks for a pack file and imports it.
func (a *App) ImportFile() (store.ImportResult, error) {
	if a.ctx == nil {
		return store.ImportResult{}, errNoDialogs
	}
	path, err := wr.OpenFileDialog(a.ctx, wr.OpenDialogOptions{
		Title:   "Cmdex",
		Filters: []wr.FileFilter{{DisplayName: "Cmdex pack (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return store.ImportResult{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return store.ImportResult{}, err
	}
	p, err := store.ParsePack(b)
	if err != nil {
		return store.ImportResult{}, err
	}
	p.Name = "" // imported content becomes the user's own
	return a.store.ImportPack(p, a.lang())
}

// ExportFile saves the whole library as a pack file and returns its path.
func (a *App) ExportFile() (string, error) {
	if a.ctx == nil {
		return "", errNoDialogs
	}
	path, err := wr.SaveFileDialog(a.ctx, wr.SaveDialogOptions{
		Title: "Cmdex", DefaultFilename: "cmdex-library.json",
		Filters: []wr.FileFilter{{DisplayName: "Cmdex pack (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	p, err := a.store.ExportPack()
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o600)
}

// Synonyms returns the search synonyms.
func (a *App) Synonyms() (map[string][]string, error) { return a.store.Synonyms() }

// SetSynonym changes the alternatives of a word.
func (a *App) SetSynonym(word string, alts []string) error {
	if word = search.Normalize(word); word == "" {
		return errors.New("empty word")
	}
	return a.store.SetSynonym(word, alts)
}

var errNoDialogs = errors.New("file dialogs need the desktop app")

func shellLabel() string {
	if runtime.GOOS == "windows" {
		return "PowerShell"
	}
	return "bash"
}
