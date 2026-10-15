package app

import (
	"errors"
	"sort"
	"strings"

	"github.com/thangkes/cmdex/internal/detect"
	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/packs"
)

// Tool packs: the built-in commands of programs such as nmap or python.
// The user turns one on (Cmdex offers it when it finds the program on
// PATH), and it joins the defaults database like a starter pack; turning it
// off takes its defaults away again (commands the user edited stay theirs).
// Settings: tools.on (packs turned on) and tools.off (offers declined).

var (
	toolPacks = packs.Tools // replaced by tests
	findTools = detect.Find
)

// ToolPack describes a tool pack for the page.
type ToolPack struct {
	Name       string            `json:"name"`
	Title      string            `json:"title"`
	Origin     string            `json:"origin"`
	Tools      []string          `json:"tools"`
	Found      map[string]string `json:"found"` // program -> where it is on this computer
	On         bool              `json:"on"`
	Declined   bool              `json:"declined"`
	Commands   int               `json:"commands"`
	Translated int               `json:"translated"` // commands named in the interface language
}

// activePacks are the packs the defaults are built from: the starter packs
// and the tool packs turned on.
func (a *App) activePacks() []store.Pack {
	out := packs.All()
	on := a.toolSet("tools.on")
	for _, p := range toolPacks() {
		if on[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) toolSet(key string) map[string]bool {
	out := map[string]bool{}
	for _, n := range strings.Split(a.store.Setting(key, ""), ",") {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = true
		}
	}
	return out
}

func (a *App) setToolSet(key string, set map[string]bool) error {
	names := make([]string, 0, len(set))
	for n, on := range set {
		if on {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return a.store.SetSetting(key, strings.Join(names, ","))
}

// ToolPacks lists every tool pack, with the programs found on this computer.
func (a *App) ToolPacks() []ToolPack {
	all := toolPacks()
	var names []string
	for _, p := range all {
		names = append(names, p.Tools...)
	}
	found := findTools(names)
	on, off := a.toolSet("tools.on"), a.toolSet("tools.off")
	lang := a.lang()
	out := []ToolPack{}
	for _, p := range all {
		tp := ToolPack{Name: p.Name, Title: p.Title.In(lang), Origin: p.Origin, Tools: p.Tools, Found: map[string]string{},
			On: on[p.Name], Declined: off[p.Name], Commands: len(p.Commands)}
		for _, t := range p.Tools {
			if where, ok := found[t]; ok {
				tp.Found[t] = where
			}
		}
		for _, c := range p.Commands {
			if lang == "en" || c.Name.Has(lang) {
				tp.Translated++
			}
		}
		out = append(out, tp)
	}
	return out
}

// NewToolPacks lists the tool packs worth offering: a program of theirs is
// on this computer, and the user has neither turned them on nor declined.
func (a *App) NewToolPacks() []ToolPack {
	out := []ToolPack{}
	for _, tp := range a.ToolPacks() {
		if len(tp.Found) > 0 && !tp.On && !tp.Declined {
			out = append(out, tp)
		}
	}
	return out
}

// SetToolPack turns a tool pack on or off, and rebuilds the defaults.
// Turning one off also stops offering it.
func (a *App) SetToolPack(name string, on bool) error {
	if !knownToolPack(name) {
		return errUnknownToolPack
	}
	onSet, offSet := a.toolSet("tools.on"), a.toolSet("tools.off")
	onSet[name], offSet[name] = on, !on
	if err := a.setToolSet("tools.on", onSet); err != nil {
		return err
	}
	if err := a.setToolSet("tools.off", offSet); err != nil {
		return err
	}
	return a.store.UseDefaults(a.activePacks(), a.lang())
}

// DeclineToolPack stops offering a tool pack (it stays off; the list of
// tool packs can still turn it on).
func (a *App) DeclineToolPack(name string) error {
	if !knownToolPack(name) {
		return errUnknownToolPack
	}
	offSet := a.toolSet("tools.off")
	offSet[name] = true
	return a.setToolSet("tools.off", offSet)
}

var errUnknownToolPack = errors.New("unknown tool pack")

func knownToolPack(name string) bool {
	for _, p := range toolPacks() {
		if p.Name == name {
			return true
		}
	}
	return false
}
