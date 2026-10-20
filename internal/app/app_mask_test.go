package app

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thangkes/cmdex/internal/mask"
	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/internal/viewer"
)

func maskApp(t *testing.T) *App {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &App{store: s, sessions: map[string]*session{}, pending: map[int64]*pendingHost{}}
}

// fakeViewer stands for the window process: what Cmdex sends arrives on
// got, and requests can be written to send.
func fakeViewer(t *testing.T, a *App) (got chan viewer.Msg, send *viewer.Writer) {
	toV, fromMain := io.Pipe()
	toMain, fromV := io.Pipe()
	got = make(chan viewer.Msg, 100)
	go viewer.Read(toV, 64<<20, func(m viewer.Msg) { got <- m })
	done := make(chan struct{})
	v := &viewerProc{out: viewer.NewWriter(fromMain), in: fromMain, wait: func() { <-done }, kill: func() {}}
	m := a.maskState()
	m.mu.Lock()
	m.view = v
	m.mu.Unlock()
	go a.viewerLoop(v, toMain)
	t.Cleanup(func() { fromV.Close(); fromMain.Close(); close(done) })
	return got, viewer.NewWriter(fromV)
}

func next(t *testing.T, got chan viewer.Msg, kind string) viewer.Msg {
	t.Helper()
	for {
		select {
		case m := <-got:
			if m.T == kind {
				return m
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %q message", kind)
		}
	}
}

func TestMaskedLogReachesTheViewerOnly(t *testing.T) {
	a := maskApp(t)
	a.newLog("s1", "ssh")
	if err := a.SessionLog("s1", []string{"SW1#show ip int br", "Vlan1  10.1.1.1  YES manual up  up", "password hunter2"}); err != nil {
		t.Fatal(err)
	}
	got, send := fakeViewer(t, a)
	send.Send(viewer.Msg{T: "ready"})
	next(t, got, "hello")
	tabs := next(t, got, "tabs")
	if len(tabs.Tabs) != 1 || tabs.Tabs[0].Label != "SSH 1" {
		t.Fatalf("tabs %+v", tabs.Tabs) // the connection's name never goes there
	}
	r := next(t, got, "reset")
	all := strings.Join(r.Lines, "\n")
	if strings.Contains(all, "10.1.1.1") || strings.Contains(all, "hunter2") || strings.Contains(all, "SW1") {
		t.Fatalf("raw value reached the viewer: %q", all)
	}
	if r.Total != 3 { // the IP, the password, the host
		t.Errorf("total %d stats %v", r.Total, r.Stats)
	}

	// new lines follow
	a.SessionLog("s1", []string{"ping 10.1.1.1"})
	if add := next(t, got, "add"); add.Lines[0] != "ping IP-1" {
		t.Errorf("add %q", add.Lines)
	}

	// "mask this too" from the window: a rule, and everything masked again
	send.Send(viewer.Msg{T: "mask", Text: "manual"})
	r = next(t, got, "reset")
	if strings.Contains(strings.Join(r.Lines, "\n"), "manual") {
		t.Errorf("not masked again: %q", r.Lines)
	}
	if rules := a.maskRules(); len(rules) != 1 || rules[0].Value != "manual" || rules[0].Kind != "text" {
		t.Errorf("rules %+v", rules)
	}
	// rules are sealed in the database like other private settings
	if raw := a.store.Setting("mask.rules", ""); strings.Contains(raw, "manual") && a.store.SealedSetting("mask.rules") != raw {
		t.Errorf("rule stored in the clear: %q", raw)
	}

	// the main window sees the real values behind the names
	info := a.MaskInfo("s1")
	found := false
	for _, p := range info.Mapping {
		found = found || (p.Name == "IP-1" && p.Value == "10.1.1.1")
	}
	if !found || info.Lines != 4 {
		t.Errorf("info %+v", info)
	}

	// closing the tab forgets everything
	a.LogForget("s1")
	if tabs := next(t, got, "tabs"); len(tabs.Tabs) != 0 {
		t.Errorf("tabs after close %+v", tabs.Tabs)
	}
	if info := a.MaskInfo("s1"); info.Lines != 0 || len(info.Mapping) != 0 {
		t.Errorf("kept after close: %+v", info)
	}
}

func TestMaskSettings(t *testing.T) {
	a := maskApp(t)
	a.newLog("s1", "local")
	a.SessionLog("s1", []string{"ip 10.2.2.2"})
	if err := a.SetMaskCategory(mask.IP, false); err != nil {
		t.Fatal(err)
	}
	if info := a.MaskInfo("s1"); info.Total != 0 {
		t.Errorf("IP still masked after turning it off: %+v", info)
	}
	if err := a.SetMaskCategory("nope", true); err == nil {
		t.Error("unknown category accepted")
	}
	if err := a.AddMaskRule(mask.Rule{Kind: "pattern", Value: ".*"}); err == nil {
		t.Error("empty-matching pattern accepted")
	}
	a.AddMaskRule(mask.Rule{Kind: "after", Value: "site", Label: "site"})
	a.AddMaskRule(mask.Rule{Kind: "after", Value: "SITE", Label: "x"}) // the same rule
	cfg := a.MaskSettings()
	if len(cfg.Rules) != 1 || len(cfg.Cats) != len(mask.Categories) {
		t.Fatalf("settings %+v", cfg)
	}
	for _, c := range cfg.Cats {
		if c.Cat == mask.IP && c.On {
			t.Error("IP shown on")
		}
	}
	if err := a.DeleteMaskRule(0); err != nil || len(a.maskRules()) != 0 {
		t.Errorf("delete: %v %+v", err, a.maskRules())
	}
}

func TestLogKeepsTheNewestLines(t *testing.T) {
	a := maskApp(t)
	a.newLog("s1", "local")
	batch := make([]string, maxLogBatch)
	for i := range batch {
		batch[i] = "line"
	}
	for i := 0; i < maxLogLines/maxLogBatch+2; i++ {
		if err := a.SessionLog("s1", batch); err != nil {
			t.Fatal(err)
		}
	}
	if n := a.MaskInfo("s1").Lines; n > maxLogLines {
		t.Errorf("%d lines kept", n)
	}
	if err := a.SessionLog("s1", make([]string, maxLogBatch+1)); err == nil {
		t.Error("oversized batch accepted")
	}
}
