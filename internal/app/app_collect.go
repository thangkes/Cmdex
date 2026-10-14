package app

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thangkes/cmdex/internal/collect"
	"github.com/thangkes/cmdex/internal/explain"
	"github.com/thangkes/cmdex/internal/store"
)

// collectLoop reads new shell history every minute while collecting is on,
// and clears the collected log as often as the user chose.
func (a *App) collectLoop() {
	time.Sleep(5 * time.Second)
	if a.store.Setting("collect.enabled", "1") == "1" {
		if n, _ := a.coll.Backfill(a.sourceEnabled); n > 0 {
			a.autoMerge()
			a.emit("collect:new", n)
		}
	}
	var cleared time.Time
	for {
		if a.store.Setting("collect.enabled", "1") == "1" {
			if n, _ := a.ScanNow(); n > 0 {
				a.emit("collect:new", n)
			}
		}
		if days := a.autoClearDays(); days > 0 && time.Since(cleared) > time.Hour {
			cleared = time.Now()
			if n, _ := a.store.ClearCollected(time.Now().AddDate(0, 0, -days).Unix()); n > 0 {
				a.emit("collect:new", 0) // the page reloads its counts
			}
		}
		time.Sleep(time.Minute)
	}
}

// ScanNow reads the enabled history files at once (then merges what the
// automatic merge may merge).
func (a *App) ScanNow() (int, error) {
	n, err := a.coll.Scan(a.sourceEnabled)
	if n > 0 {
		a.autoMerge()
	}
	return n, err
}

// Levels the user may choose in the Collect settings.
var (
	mergeLevels = []int{50, 75, 85, 100} // percent of words in common
	clearDays   = []int{0, 7, 30, 90}    // 0: never
)

func oneOf(v int, allowed []int) bool {
	for _, x := range allowed {
		if v == x {
			return true
		}
	}
	return false
}

// mergeAt is the similarity (percent) from which a collected group is
// offered for merging, and merged automatically when that is on.
func (a *App) mergeAt() int {
	v, err := strconv.Atoi(a.store.Setting("collect.mergeAt", ""))
	if err != nil || !oneOf(v, mergeLevels) {
		return collect.DefaultSimilarity
	}
	return v
}

func (a *App) autoClearDays() int {
	v, err := strconv.Atoi(a.store.Setting("collect.autoClear", "0"))
	if err != nil || !oneOf(v, clearDays) {
		return 0
	}
	return v
}

var mergeMu sync.Mutex // one automatic merge at a time (scan loop and page)

// autoMerge merges, when the user turned it on, every group waiting for
// review that resembles a library command at least as much as the chosen
// level: its runs are counted on that command. New commands are never
// added on their own; they wait for the user as always. It returns how
// many groups were merged.
func (a *App) autoMerge() int {
	if a.store.Setting("collect.automerge", "0") != "1" {
		return 0
	}
	mergeMu.Lock()
	defer mergeMu.Unlock()
	list, err := a.store.CollectedList(store.StatusPending)
	if err != nil || len(list) == 0 {
		return 0
	}
	cmds, err := a.store.Commands()
	if err != nil {
		return 0
	}
	at, n := a.mergeAt(), 0
	for _, g := range list {
		m := collect.MatchLibrary(g.Signature, cmds, at)
		if m.Kind == "new" || m.Target == 0 || m.Percent < at {
			continue
		}
		if a.store.AddUses(m.Target, g.Total) == nil && a.store.SetCollectedAuto(g.ID, m.Target) == nil {
			n++
		}
	}
	if n > 0 {
		a.emit("collect:merged", n)
	}
	return n
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
	Enabled   bool            `json:"enabled"`
	Session   bool            `json:"session"`
	Sources   []CollectSource `json:"sources"`
	AutoMerge bool            `json:"autoMerge"` // merge resembling groups without asking
	MergeAt   int             `json:"mergeAt"`   // similarity level, percent
	AutoClear int             `json:"autoClear"` // clear the log after this many days (0 never)
	Levels    []int           `json:"levels"`
	ClearDays []int           `json:"clearDays"`
}

// GetCollectSettings returns the switches and the history files found.
func (a *App) GetCollectSettings() CollectSettings {
	cs := CollectSettings{
		Enabled:   a.store.Setting("collect.enabled", "1") == "1",
		Session:   a.store.Setting("collect.session", "1") == "1",
		Sources:   []CollectSource{},
		AutoMerge: a.store.Setting("collect.automerge", "0") == "1",
		MergeAt:   a.mergeAt(),
		AutoClear: a.autoClearDays(),
		Levels:    mergeLevels,
		ClearDays: clearDays,
	}
	for _, s := range collect.Sources() {
		cs.Sources = append(cs.Sources, CollectSource{ID: s.ID, Path: s.Path, Enabled: a.store.Setting("collect.src."+s.ID, "1") == "1"})
	}
	return cs
}

// SetCollectSwitch turns collecting ("enabled"), session commands
// ("session"), one history file ("src.<id>") or the automatic merge
// ("automerge") on or off. Turning the automatic merge on applies it at once
// to the groups waiting for review.
func (a *App) SetCollectSwitch(name string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	if err := a.store.SetSetting("collect."+name, v); err != nil {
		return err
	}
	if name == "automerge" && on {
		a.autoMerge()
	}
	return nil
}

// SetCollectLevel sets the similarity level for merging ("mergeAt": 50, 75,
// 85 or 100) or the days after which the collected log is cleared
// ("autoClear": 0, 7, 30 or 90).
func (a *App) SetCollectLevel(name string, value int) error {
	switch {
	case name == "mergeAt" && oneOf(value, mergeLevels):
		if err := a.store.SetSetting("collect.mergeAt", strconv.Itoa(value)); err != nil {
			return err
		}
		a.autoMerge()
		return nil
	case name == "autoClear" && oneOf(value, clearDays):
		return a.store.SetSetting("collect.autoClear", strconv.Itoa(value))
	}
	return errors.New("unknown setting or level")
}

// ClearCollected deletes the collected log now: the command lines seen and
// the groups waiting for review. Decisions already made stay, so those
// commands do not come back for review. It returns how many groups were
// cleared.
func (a *App) ClearCollected() (int, error) { return a.store.ClearCollected(0) }

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
			v.Match = collect.MatchLibrary(c.Signature, cmds, a.mergeAt())
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
// for secrets and folders: a folder names the user's own work (a customer,
// a project, the user name), and library commands travel in shared packs.
func defaultFor(g *store.Collected, key string) string {
	if strings.HasPrefix(key, "secret") || strings.HasPrefix(key, "path") || len(g.Variants) == 0 {
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
