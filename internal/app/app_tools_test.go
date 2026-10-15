package app

import (
	"path/filepath"
	"testing"

	"github.com/thangkes/cmdex/internal/store"
)

func TestToolPacks(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetSetting("lang", "vi")
	a := &App{store: s}

	oldPacks, oldFind := toolPacks, findTools
	defer func() { toolPacks, findTools = oldPacks, oldFind }()
	toolPacks = func() []store.Pack {
		return []store.Pack{
			{Name: "demo", Title: store.Text{"en": "Demo", "vi": "Thử"}, Tools: []string{"demo"}, Origin: "test", Commands: []store.PackCommand{
				{Name: store.Text{"en": "Scan a host", "vi": "Quét máy"}, Platform: store.Both, Template: "demo-tool-scan {host}"},
				{Name: store.Text{"en": "Show the version", "vi": ""}, Platform: store.Both, Template: "demo-tool --version"},
			}},
			{Name: "other", Title: store.Text{"en": "Other"}, Tools: []string{"other"}, Origin: "test", Commands: []store.PackCommand{
				{Name: store.Text{"en": "Other"}, Platform: store.Both, Template: "other-tool"},
			}},
		}
	}
	findTools = func(names []string) map[string]string { return map[string]string{"demo": "/usr/bin/demo"} }
	if err := s.UseDefaults(a.activePacks(), a.lang()); err != nil {
		t.Fatal(err)
	}
	has := func(tpl string) bool {
		cmds, _ := s.Commands()
		for _, c := range cmds {
			if c.Template == tpl {
				return true
			}
		}
		return false
	}

	list := a.ToolPacks()
	if len(list) != 2 || list[0].Title != "Thử" || list[0].Found["demo"] != "/usr/bin/demo" || len(list[1].Found) != 0 {
		t.Fatalf("list: %+v", list)
	}
	if list[0].Commands != 2 || list[0].Translated != 1 {
		t.Errorf("counts: %+v", list[0])
	}
	if n := a.NewToolPacks(); len(n) != 1 || n[0].Name != "demo" {
		t.Fatalf("offers: %+v", n)
	}
	if has("demo-tool-scan {host}") {
		t.Fatal("a tool pack is in the library before it is turned on")
	}

	if err := a.SetToolPack("demo", true); err != nil {
		t.Fatal(err)
	}
	if !has("demo-tool-scan {host}") || !has("demo-tool --version") || has("other-tool") {
		t.Fatal("turning demo on")
	}
	if len(a.NewToolPacks()) != 0 {
		t.Error("offered again once on")
	}
	cmds, _ := s.Commands()
	for _, c := range cmds {
		if c.Template == "demo-tool --version" && c.Name != "Show the version" {
			t.Errorf("an empty translation should fall back to English, got %q", c.Name)
		}
	}

	if err := a.SetToolPack("demo", false); err != nil {
		t.Fatal(err)
	}
	if has("demo-tool-scan {host}") {
		t.Fatal("turning demo off left its commands")
	}
	if l := a.ToolPacks(); l[0].On || !l[0].Declined || len(a.NewToolPacks()) != 0 {
		t.Errorf("after off: %+v", l[0])
	}

	if err := a.DeclineToolPack("other"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "nope", "demo,other"} {
		if a.SetToolPack(bad, true) == nil || a.DeclineToolPack(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got := s.Setting("tools.off", ""); got != "demo,other" {
		t.Errorf("tools.off = %q", got)
	}
}
