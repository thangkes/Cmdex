package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testPack(t *testing.T) Pack {
	t.Helper()
	p, err := ParsePack([]byte(packJSON))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// withDefaults opens a user database next to the defaults built from packJSON.
func withDefaults(t *testing.T, lang string) *Store {
	t.Helper()
	s := open(t)
	if err := s.UseDefaults([]Pack{testPack(t)}, lang); err != nil {
		t.Fatal(err)
	}
	return s
}

func find(t *testing.T, s *Store, platform string) Command {
	t.Helper()
	cmds, err := s.Commands()
	if err != nil {
		t.Fatal(err)
	}
	var out []Command
	for _, c := range cmds {
		if c.Platform == platform {
			out = append(out, c)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%s: want 1 command, got %+v", platform, out)
	}
	return out[0]
}

func userRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM commands").Scan(&n)
	return n
}

func TestDefaultsAreReadNotCopied(t *testing.T) {
	s := withDefaults(t, "vi")
	ping := find(t, s, Windows)
	if !ping.Base || ping.ID >= 0 || ping.Name != "Ping máy" || ping.Params[0].Description != "Địa chỉ máy" {
		t.Fatalf("%+v", ping)
	}
	if n := userRows(t, s); n != 0 {
		t.Fatalf("defaults copied into the user's database: %d rows", n)
	}
	if got, err := s.Command(ping.ID); err != nil || got.Template != ping.Template {
		t.Fatalf("%+v %v", got, err)
	}
	if e, ok := s.Explain("ping"); !ok || e.Summary != "Gửi gói ICMP" {
		t.Fatalf("%+v", e)
	}
	if syn, _ := s.Synonyms(); len(syn["cong"]) != 1 {
		t.Fatalf("%+v", syn)
	}
	if r, _ := s.Rules(); len(r) != 1 || r[0].ID >= 0 {
		t.Fatalf("%+v", r)
	}
	// the same template cannot be added twice
	if _, err := s.SaveCommand(Command{Name: "x", Platform: Windows, Template: ping.Template}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
}

func TestEditDeleteRestoreDefault(t *testing.T) {
	s := withDefaults(t, "en")
	ping := find(t, s, Windows)
	s.AddUses(ping.ID, 3)
	if got := find(t, s, Windows); got.Uses != 3 {
		t.Fatalf("uses %d", got.Uses)
	}

	ping.Name = "My ping"
	id, err := s.SaveCommand(ping)
	if err != nil || id <= 0 {
		t.Fatalf("%d %v", id, err)
	}
	got := find(t, s, Windows) // the copy replaces the default
	if got.ID != id || got.Base || got.Name != "My ping" || got.Uses != 3 || got.BaseKey != CommandKey(Windows, ping.Template) {
		t.Fatalf("%+v", got)
	}
	// saving it again keeps one copy
	got.Name = "My ping 2"
	if id2, err := s.SaveCommand(got); err != nil || id2 != id {
		t.Fatalf("%d %v", id2, err)
	}

	if err := s.DeleteCommand(id); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := s.Commands(); len(cmds) != 1 {
		t.Fatalf("deleting the copy must not bring the default back: %+v", cmds)
	}
	ls := find(t, s, Linux)
	if err := s.DeleteCommand(ls.ID); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := s.Commands(); len(cmds) != 0 {
		t.Fatalf("%+v", cmds)
	}
	if n, err := s.RestoreDefaults(); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	if cmds, _ := s.Commands(); len(cmds) != 2 {
		t.Fatalf("%+v", cmds)
	}
}

func TestDefaultsRebuildKeepsUserState(t *testing.T) {
	s := withDefaults(t, "en")
	ping := find(t, s, Windows)
	s.AddUses(ping.ID, 5)
	ls := find(t, s, Linux)
	s.DeleteCommand(ls.ID)
	first := s.DefaultsPath()

	// the language changes: the defaults are rebuilt, the user's state stays
	if err := s.UseDefaults([]Pack{testPack(t)}, "vi"); err != nil {
		t.Fatal(err)
	}
	if s.DefaultsPath() == first {
		t.Fatal("not rebuilt")
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("old defaults file left: %v", err)
	}
	got := find(t, s, Windows)
	if got.Name != "Ping máy" || got.Uses != 5 || got.ID != ping.ID {
		t.Fatalf("%+v", got)
	}
	if cmds, _ := s.Commands(); len(cmds) != 1 {
		t.Fatalf("hidden default came back: %+v", cmds)
	}

	// an update brings a new default and drops none of the user's state
	p := testPack(t)
	p.Commands = append(p.Commands, PackCommand{Name: Text{"": "Who"}, Platform: Linux, Template: "whoami"})
	if err := s.UseDefaults([]Pack{p}, "vi"); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := s.Commands(); len(cmds) != 2 {
		t.Fatalf("%+v", cmds)
	}
}

func TestDefaultRulesAndSynonyms(t *testing.T) {
	s := withDefaults(t, "en")
	rules, _ := s.Rules()
	if _, err := s.SaveRule(Rule{ID: rules[0].ID, Kind: "ignore", Match: "cd"}); err != nil {
		t.Fatal(err)
	}
	rules, _ = s.Rules()
	if len(rules) != 1 || rules[0].ID <= 0 || rules[0].Match != "cd" {
		t.Fatalf("%+v", rules)
	}
	s.SetSynonym("cong", []string{"port", "socket"})
	if syn, _ := s.Synonyms(); len(syn["cong"]) != 2 {
		t.Fatalf("%+v", syn)
	}
	s.SetSynonym("cong", []string{"port"}) // back to the default: nothing stored
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM synonyms").Scan(&n)
	if n != 0 {
		t.Fatalf("%d synonym rows", n)
	}
	s.SetSynonym("cong", nil)
	if syn, _ := s.Synonyms(); syn["cong"] != nil {
		t.Fatalf("%+v", syn)
	}
}

// A library made by Cmdex 0.1 (starter packs copied in) upgrades cleanly.
func TestUpgradeFromCopiedPacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdex.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := testPack(t)
	old.ImportPack(p, "en")
	old.SetSetting("packs.imported", "2026-10-13T00:00:00Z")
	old.SaveRule(Rule{Kind: "ignore", Match: retiredRules[0].Match})
	old.SaveRule(Rule{Kind: "category", Match: "git", Result: "Mine"})
	var ping, ls Command
	cmds, _ := old.Commands()
	for _, c := range cmds {
		if c.Platform == Windows {
			ping = c
		} else {
			ls = c
		}
	}
	old.AddUses(ping.ID, 7)
	ls.Name = "My listing" // edited (a second later, as in real use)
	old.SaveCommand(ls)
	old.db.Exec("UPDATE commands SET updated = updated + 1 WHERE id = ?", ls.ID)
	old.SaveCommand(Command{Name: "mine", Platform: Linux, Template: "uptime"})
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UseDefaults([]Pack{p}, "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak-v1"); err != nil {
		t.Fatalf("no backup before the upgrade: %v", err)
	}
	cmds, _ = s.Commands()
	if len(cmds) != 3 {
		t.Fatalf("want 3 commands, got %+v", cmds)
	}
	for _, c := range cmds {
		switch c.Template {
		case ping.Template:
			if !c.Base || c.Uses != 7 {
				t.Fatalf("unedited copy not replaced by the default: %+v", c)
			}
		case ls.Template:
			if c.Base || c.Name != "My listing" || c.BaseKey == "" {
				t.Fatalf("edited copy lost: %+v", c)
			}
		case "uptime":
		default:
			t.Fatalf("unexpected %+v", c)
		}
	}
	if n := userRows(t, s); n != 2 {
		t.Fatalf("%d user rows", n)
	}
	var src string
	if s.db.QueryRow("SELECT source FROM explain WHERE cmd = 'ping'").Scan(&src) == nil {
		t.Fatal("old copy of a default explanation left")
	}
	rules, _ := s.Rules()
	if len(rules) != 2 { // the default ignore of the test pack, and "Mine"
		t.Fatalf("%+v", rules)
	}
	for _, r := range rules {
		if r.Match == retiredRules[0].Match {
			t.Fatal("retired rule left")
		}
	}
	if s.Setting("packs.imported", "") != "" {
		t.Fatal("packs.imported left: going back to 0.1 would not bring the starters")
	}
	// running again (every start) changes nothing
	if err := s.UseDefaults([]Pack{p}, "en"); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Commands(); len(again) != 3 {
		t.Fatalf("%+v", again)
	}
}

func TestBackupKeepsWhatUserDidToDefaults(t *testing.T) {
	a := withDefaults(t, "en")
	ping := find(t, a, Windows)
	a.AddUses(ping.ID, 4)
	ls := find(t, a, Linux)
	ls.Name = "My listing"
	a.SaveCommand(ls)
	a.AddCollected("ping {host} -n {count}", "", "powershell", false, "ping 1.1.1.1 -n 1")
	g, _ := a.CollectedList("")
	a.SetCollectedStatus(g[0].ID, StatusMerged, ping.ID)
	b, err := a.ExportBackup("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Commands) != 1 || b.Commands[0].BaseKey == "" || len(b.BaseState) != 1 {
		t.Fatalf("%+v", b)
	}
	if b.Collected[0].Target == nil || b.Collected[0].Target.Template != ping.Template {
		t.Fatalf("%+v", b.Collected[0])
	}

	c := withDefaults(t, "en")
	if _, err := c.ImportBackup(b, false); err != nil {
		t.Fatal(err)
	}
	cmds, _ := c.Commands()
	if len(cmds) != 2 {
		t.Fatalf("%+v", cmds)
	}
	if got := find(t, c, Windows); !got.Base || got.Uses != 4 {
		t.Fatalf("%+v", got)
	}
	if got := find(t, c, Linux); got.Base || got.Name != "My listing" {
		t.Fatalf("%+v", got)
	}
	if g, _ := c.CollectedList(""); g[0].Target != ping.ID {
		t.Fatalf("collected target %d, want %d", g[0].Target, ping.ID)
	}
}
