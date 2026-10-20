package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/thangkes/cmdex/internal/mask"
	"github.com/thangkes/cmdex/internal/viewer"
)

// Log masking. The page hands over each finished line of a terminal; the
// raw lines stay in memory for the life of the tab and are never written
// anywhere. Closing the tab forgets them. The masked lines go to the shared
// log window (package viewer), the only place they are shown.

const (
	maxLogLines = 20000 // per tab; the oldest go first
	maxLogLine  = 8 << 10
	maxLogBatch = 2000
)

type tabLog struct {
	label  string
	ended  bool
	raw    []string // as printed: memory only
	masked []string
	st     *mask.State
}

type maskState struct {
	mu     sync.Mutex
	logs   map[string]*tabLog
	order  []string
	counts map[string]int // labels: kind -> last number
	masker *mask.Masker
	active string
	view   *childProc // the shared log window
}

func (a *App) maskState() *maskState {
	a.maskOnce.Do(func() { a.mask = &maskState{logs: map[string]*tabLog{}, counts: map[string]int{}} })
	return a.mask
}

// newLog starts the log of a session (addSession).
func (a *App) newLog(id, kind string) {
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[kind]++
	label := map[string]string{"local": "Terminal", "ssh": "SSH", "telnet": "Telnet", "serial": "Serial"}[kind]
	if label == "" {
		label = "Session"
	}
	m.logs[id] = &tabLog{label: fmt.Sprintf("%s %d", label, m.counts[kind]), st: a.masker().New()}
	m.order = append(m.order, id)
	m.sendTabs()
}

// endLog marks a session ended: its tab (and log) stays until closed.
func (a *App) endLog(id string) {
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.logs[id]; l != nil {
		l.ended = true
		m.sendTabs()
	}
}

// SessionLog adds finished lines of a terminal to its log.
func (a *App) SessionLog(id string, lines []string) error {
	if len(lines) > maxLogBatch {
		return errors.New("too many lines at once")
	}
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.logs[id]
	if l == nil {
		return nil // the tab was closed meanwhile
	}
	added := make([]string, 0, len(lines))
	for _, s := range lines {
		if len(s) > maxLogLine {
			s = s[:maxLogLine]
		}
		s = strings.ToValidUTF8(s, "�")
		l.raw = append(l.raw, s)
		added = append(added, l.st.Line(s))
	}
	l.masked = append(l.masked, added...)
	if len(l.raw) > maxLogLines {
		// drop the oldest, and mask what is left again so no pseudonym
		// points at a line that is gone
		cut := len(l.raw) - maxLogLines + maxLogLines/10
		l.raw = append([]string(nil), l.raw[cut:]...)
		a.remask(l)
		m.send(viewer.Msg{T: "reset", Tab: id, Lines: l.masked, Stats: l.st.Stats(), Total: l.st.Total()})
	} else {
		m.send(viewer.Msg{T: "add", Tab: id, Lines: added, Stats: l.st.Stats(), Total: l.st.Total()})
	}
	a.emit("mask:count", id, l.st.Total())
	return nil
}

// LogForget erases a tab's log: the tab was closed.
func (a *App) LogForget(id string) {
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.logs[id]; l != nil {
		l.st.Erase()
		l.raw, l.masked = nil, nil
		delete(m.logs, id)
		m.order = remove(m.order, id)
		m.sendTabs()
	}
}

// LogActive follows the tab shown in the main window (the shared window
// shows it unless the user picks another).
func (a *App) LogActive(id string) {
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = id
	m.sendTabs()
}

func remove(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// remask masks a log again from its raw lines (rules changed).
func (a *App) remask(l *tabLog) {
	l.st.Erase()
	l.st = a.masker().New()
	l.masked = make([]string, len(l.raw))
	for i, s := range l.raw {
		l.masked[i] = l.st.Line(s)
	}
}

// ---- rules ------------------------------------------------------------------

// MaskCategory is a built-in category and whether it is on.
type MaskCategory struct {
	Cat string `json:"cat"`
	On  bool   `json:"on"`
}

// MaskConfig is what the page edits.
type MaskConfig struct {
	Cats  []MaskCategory `json:"cats"`
	Rules []mask.Rule    `json:"rules"`
}

func (a *App) maskCats() map[string]bool {
	cats := map[string]bool{}
	json.Unmarshal([]byte(a.store.Setting("mask.cats", "{}")), &cats)
	return cats
}

// maskRules are sealed: a rule may be the secret itself.
func (a *App) maskRules() []mask.Rule {
	rules := []mask.Rule{}
	if v := a.store.SealedSetting("mask.rules"); v != "" {
		json.Unmarshal([]byte(v), &rules)
	}
	return rules
}

// masker is the compiled rules (a.mask.mu need not be held).
func (a *App) masker() *mask.Masker {
	a.maskerMu.Lock()
	defer a.maskerMu.Unlock()
	if a.compiled == nil {
		cfg := mask.Config{Known: map[string][]string{}}
		if a.store != nil {
			cfg.Cats, cfg.Rules = a.maskCats(), a.maskRules()
		}
		if h, err := os.Hostname(); err == nil {
			cfg.Known[mask.Host] = []string{h}
		}
		if u, err := user.Current(); err == nil {
			name := u.Username[strings.LastIndexAny(u.Username, `\/`)+1:]
			cfg.Known[mask.User] = []string{name}
		}
		a.compiled = mask.Compile(cfg)
	}
	return a.compiled
}

// MaskSettings returns the categories and the user's rules.
func (a *App) MaskSettings() MaskConfig {
	cats := a.maskCats()
	out := MaskConfig{Rules: a.maskRules()}
	for _, c := range mask.Categories {
		on, ok := cats[c]
		if !ok {
			on = mask.DefaultOn(c)
		}
		out.Cats = append(out.Cats, MaskCategory{Cat: c, On: on})
	}
	return out
}

// SetMaskCategory turns a built-in category on or off.
func (a *App) SetMaskCategory(cat string, on bool) error {
	known := false
	for _, c := range mask.Categories {
		known = known || c == cat
	}
	if !known {
		return errors.New("unknown category")
	}
	cats := a.maskCats()
	cats[cat] = on
	b, _ := json.Marshal(cats)
	if err := a.store.SetSetting("mask.cats", string(b)); err != nil {
		return err
	}
	a.rulesChanged()
	return nil
}

// AddMaskRule adds a rule (an identical one is not added twice).
func (a *App) AddMaskRule(r mask.Rule) error {
	r.Value, r.Label = strings.TrimSpace(r.Value), strings.TrimSpace(r.Label)
	if r.Kind == "pattern" {
		r.Value = strings.TrimSpace(r.Value)
	}
	if err := r.Check(); err != nil {
		return err
	}
	rules := a.maskRules()
	for _, x := range rules {
		if x.Kind == r.Kind && strings.EqualFold(x.Value, r.Value) {
			return nil
		}
	}
	if len(rules) >= mask.MaxRules {
		return fmt.Errorf("at most %d rules", mask.MaxRules)
	}
	return a.saveMaskRules(append(rules, r))
}

// DeleteMaskRule removes the rule at index i.
func (a *App) DeleteMaskRule(i int) error {
	rules := a.maskRules()
	if i < 0 || i >= len(rules) {
		return errors.New("no such rule")
	}
	return a.saveMaskRules(append(rules[:i], rules[i+1:]...))
}

func (a *App) saveMaskRules(rules []mask.Rule) error {
	b, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if err := a.store.SetSealedSetting("mask.rules", string(b)); err != nil {
		return err
	}
	a.rulesChanged()
	return nil
}

// rulesChanged masks every log again and tells both windows.
func (a *App) rulesChanged() {
	a.maskerMu.Lock()
	a.compiled = nil
	a.maskerMu.Unlock()
	m := a.maskState()
	m.mu.Lock()
	for _, id := range m.order {
		l := m.logs[id]
		a.remask(l)
		m.send(viewer.Msg{T: "reset", Tab: id, Lines: l.masked, Stats: l.st.Stats(), Total: l.st.Total()})
		a.emit("mask:count", id, l.st.Total())
	}
	m.mu.Unlock()
	a.emit("mask:rules")
}

// MaskInfo is what the main window shows about a tab's log: counts and the
// pseudonyms with their real values (never shown in the shared window).
type MaskInfo struct {
	Lines   int            `json:"lines"`
	Total   int            `json:"total"`
	Stats   map[string]int `json:"stats"`
	Mapping []mask.Pair    `json:"mapping"`
	Label   string         `json:"label"`
	Viewer  bool           `json:"viewer"`
}

// MaskInfo describes the log of tab id.
func (a *App) MaskInfo(id string) MaskInfo {
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := MaskInfo{Stats: map[string]int{}, Mapping: []mask.Pair{}, Viewer: m.view != nil}
	if l := m.logs[id]; l != nil {
		out.Lines, out.Total, out.Stats, out.Mapping, out.Label = len(l.raw), l.st.Total(), l.st.Stats(), l.st.Mapping(), l.label
	}
	return out
}

// ---- the shared log window ------------------------------------------------

// OpenViewer starts the shared log window (once).
func (a *App) OpenViewer() error {
	m := a.maskState()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.view != nil {
		return nil
	}
	c, err := startChild("viewer", 64<<10, a.viewerMsg, a.viewerEnd)
	if err != nil {
		return err
	}
	m.view = c
	a.emit("viewer:state", true)
	return nil
}

// CloseViewer closes the shared log window.
func (a *App) CloseViewer() {
	m := a.maskState()
	m.mu.Lock()
	v := m.view
	m.mu.Unlock()
	if v != nil {
		v.close()
	}
}

func (a *App) viewerMsg(v *childProc, msg viewer.Msg) {
	switch msg.T {
	case "ready":
		m := a.maskState()
		m.mu.Lock()
		v.ready = true
		m.hello(a)
		m.sendTabs()
		for _, id := range m.order {
			l := m.logs[id]
			m.send(viewer.Msg{T: "reset", Tab: id, Lines: l.masked, Stats: l.st.Stats(), Total: l.st.Total()})
		}
		m.mu.Unlock()
	case "mask":
		text := strings.TrimSpace(msg.Text)
		if text == "" || utf8.RuneCountInString(text) > viewer.MaxText || strings.ContainsAny(text, "\r\n") {
			return
		}
		if err := a.AddMaskRule(mask.Rule{Kind: "text", Value: text}); err == nil {
			a.emit("mask:added")
		}
	}
}

func (a *App) viewerEnd(v *childProc) {
	m := a.maskState()
	m.mu.Lock()
	if m.view == v {
		m.view = nil
	}
	started := v.ready
	m.mu.Unlock()
	a.emit("viewer:state", false, !started) // ended before its page was ready: it failed
}

// send passes a message to the window when it is ready (m.mu held).
func (m *maskState) send(msg viewer.Msg) {
	if m.view == nil || !m.view.ready {
		return
	}
	if msg.Lines == nil && (msg.T == "reset") {
		msg.Lines = []string{}
	}
	if err := m.view.send(msg); err != nil {
		m.view.ready = false // gone: viewerEnd clears it
	}
}

func (m *maskState) sendTabs() {
	tabs := []viewer.Tab{}
	for _, id := range m.order {
		l := m.logs[id]
		tabs = append(tabs, viewer.Tab{ID: id, Label: l.label, Active: id == m.active, Ended: l.ended})
	}
	m.send(viewer.Msg{T: "tabs", Tabs: tabs})
}

func (m *maskState) hello(a *App) { m.send(a.helloMsg(false)) }

// helloMsg is the language and theme a window starts with (and protection,
// for a window that holds private things).
func (a *App) helloMsg(protect bool) viewer.Msg {
	h := viewer.Msg{T: "hello", Lang: systemLang(), Theme: "graphite", OS: runtime.GOOS}
	if a.store != nil {
		h.Lang, h.Theme = a.lang(), a.store.Setting("theme", "graphite")
		h.Protect = protect && a.store.Setting("protect.capture", "1") == "1"
	}
	return h
}

// windowsLook passes a language or theme change to the other windows.
func (a *App) windowsLook() {
	m := a.maskState()
	m.mu.Lock()
	m.hello(a)
	m.mu.Unlock()
	a.notesSend(a.helloMsg(true))
}

// closeLogs forgets every log and closes the window (shutdown).
func (a *App) closeLogs() {
	m := a.maskState()
	m.mu.Lock()
	for id, l := range m.logs {
		l.st.Erase()
		l.raw, l.masked = nil, nil
		delete(m.logs, id)
	}
	m.order = nil
	v := m.view
	m.mu.Unlock()
	if v != nil {
		v.in.Close()
		v.kill()
	}
}
