package app

import (
	"errors"
	"strings"
	"time"

	"github.com/thangkes/cmdex/internal/collect"
	"github.com/thangkes/cmdex/internal/explain"
	"github.com/thangkes/cmdex/internal/store"
)

// collectLoop reads new shell history every minute while collecting is on.
func (a *App) collectLoop() {
	time.Sleep(5 * time.Second)
	if a.store.Setting("collect.enabled", "1") == "1" {
		if n, _ := a.coll.Backfill(a.sourceEnabled); n > 0 {
			a.emit("collect:new", n)
		}
	}
	for {
		if a.store.Setting("collect.enabled", "1") == "1" {
			if n, _ := a.ScanNow(); n > 0 {
				a.emit("collect:new", n)
			}
		}
		time.Sleep(time.Minute)
	}
}

// ScanNow reads the enabled history files at once.
func (a *App) ScanNow() (int, error) {
	return a.coll.Scan(a.sourceEnabled)
}

func (a *App) sourceEnabled(id string) bool { return a.store.Setting("collect.src."+id, "1") == "1" }

// CollectSource is a history file with its switch.
type CollectSource struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

// CollectSettings is the Collect screen's switches.
type CollectSettings struct {
	Enabled bool            `json:"enabled"`
	Session bool            `json:"session"`
	Sources []CollectSource `json:"sources"`
}

// GetCollectSettings returns the switches and the history files found.
func (a *App) GetCollectSettings() CollectSettings {
	cs := CollectSettings{
		Enabled: a.store.Setting("collect.enabled", "1") == "1",
		Session: a.store.Setting("collect.session", "1") == "1",
		Sources: []CollectSource{},
	}
	for _, s := range collect.Sources() {
		cs.Sources = append(cs.Sources, CollectSource{ID: s.ID, Path: s.Path, Enabled: a.store.Setting("collect.src."+s.ID, "1") == "1"})
	}
	return cs
}

// SetCollectSwitch turns collecting ("enabled"), session commands
// ("session") or one history file ("src.<id>") on or off.
func (a *App) SetCollectSwitch(name string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return a.store.SetSetting("collect."+name, v)
}

// CollectedView is a collected group with how it relates to the library.
type CollectedView struct {
	store.Collected
	Match collect.Match `json:"match"`
}

// Collected lists groups with the given status ("" all, "pending", …).
func (a *App) Collected(status string) ([]CollectedView, error) {
	list, err := a.store.CollectedList(status)
	if err != nil {
		return nil, err
	}
	cmds, err := a.store.Commands()
	if err != nil {
		return nil, err
	}
	out := make([]CollectedView, 0, len(list))
	for _, c := range list {
		v := CollectedView{Collected: c}
		if c.Status == store.StatusPending {
			v.Match = collect.MatchLibrary(c.Signature, cmds)
		} else {
			v.Match = collect.Match{Kind: c.Status, Target: c.Target}
		}
		out = append(out, v)
	}
	return out, nil
}

// CollectDecide applies the user's choice for a group: "add" (new library
// command), "merge" (count its runs on the matching command), "ignore", or
// "undo". It returns the library command's ID for "add"/"merge".
func (a *App) CollectDecide(id int64, action string, target int64) (int64, error) {
	list, err := a.store.CollectedList("")
	if err != nil {
		return 0, err
	}
	var g *store.Collected
	for i := range list {
		if list[i].ID == id {
			g = &list[i]
		}
	}
	if g == nil {
		return 0, errors.New("group not found")
	}
	switch action {
	case "add":
		c := store.Command{
			Name:     nameFor(g.Signature),
			Platform: platformFor(g.Source),
			Category: g.Category,
			Template: g.Signature,
			Uses:     g.Total,
			Source:   "collected",
			Params:   store.ParamsFromTemplate(g.Signature),
		}
		for i, p := range c.Params {
			if strings.HasPrefix(p.Key, "secret") {
				c.Params[i].Type = "secret"
			}
			c.Params[i].Default = defaultFor(g, p.Key)
		}
		newID, err := a.store.SaveCommand(c)
		if errors.Is(err, store.ErrDuplicate) {
			return 0, errors.New("this template is already in the library")
		}
		if err != nil {
			return 0, err
		}
		return newID, a.store.SetCollectedStatus(id, store.StatusAdded, newID)
	case "merge":
		if target == 0 {
			return 0, errors.New("no command to merge into")
		}
		if err := a.store.AddUses(target, g.Total); err != nil {
			return 0, err
		}
		return target, a.store.SetCollectedStatus(id, store.StatusMerged, target)
	case "ignore":
		return 0, a.store.SetCollectedStatus(id, store.StatusIgnored, 0)
	case "undo":
		return 0, a.store.SetCollectedStatus(id, store.StatusPending, 0)
	}
	return 0, errors.New("unknown action")
}

// nameFor makes a first name from a template: its programs, "ping" or
// "netstat | findstr". The user renames it in the library.
func nameFor(sig string) string {
	var progs []string
	newCmd := true
	for _, t := range explain.Lex(sig) {
		if t.Op {
			newCmd = true
			continue
		}
		if newCmd {
			progs = append(progs, t.Text)
			newCmd = false
		}
	}
	return strings.Join(progs, " | ")
}

func platformFor(source string) string {
	switch source {
	case "powershell":
		return store.Windows
	case "bash", "zsh":
		return store.Linux
	}
	return store.Both
}

// defaultFor takes a parameter's default from the most-run line, except
// for secrets.
func defaultFor(g *store.Collected, key string) string {
	if strings.HasPrefix(key, "secret") || len(g.Variants) == 0 {
		return ""
	}
	tpl := explain.Lex(g.Signature)
	raw := explain.Lex(g.Variants[0].Raw)
	if len(tpl) != len(raw) {
		return ""
	}
	for i, t := range tpl {
		if t.Text == "{"+key+"}" {
			return strings.Trim(raw[i].Text, `"'`)
		}
	}
	return ""
}

// Rules returns the classification rules.
func (a *App) Rules() ([]store.Rule, error) { return a.store.Rules() }

// SaveRule adds or changes a rule.
func (a *App) SaveRule(r store.Rule) (int64, error) { return a.store.SaveRule(r) }

// DeleteRule removes a rule.
func (a *App) DeleteRule(id int64) error { return a.store.DeleteRule(id) }
