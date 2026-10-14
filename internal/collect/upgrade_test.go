package collect

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/packs"
)

// dump reads every row of the user's tables, as text, to compare.
func dump(t *testing.T, path string) map[string][]string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	out := map[string][]string{}
	for _, tbl := range []string{"commands", "explain", "collected", "variants", "rules", "synonyms", "connections", "settings", "base_state"} {
		rows, err := db.Query("SELECT * FROM " + tbl + " ORDER BY 1, 2")
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			var b strings.Builder
			for i, v := range vals {
				fmt.Fprintf(&b, "%s=%v;", cols[i], v)
			}
			if tbl == "settings" && (strings.Contains(b.String(), "collect.bakScrub") || strings.Contains(b.String(), "collect.scrub")) {
				continue // bookkeeping of the once-only cleaning
			}
			out[tbl] = append(out[tbl], b.String())
		}
		rows.Close()
	}
	return out
}

// startup runs what the app does when it opens a library.
func startup(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := &Collector{Store: s}
	c.Rescrub()
	if err := s.UseDefaults(packs.All(), "vi"); err != nil {
		t.Fatal(err)
	}
	if err := c.ScrubBackup(); err != nil {
		t.Fatal(err)
	}
	return s
}

// A library in use with Cmdex 0.1.3 comes out of this version's start-up
// exactly as it went in: library, edits of starter commands, deleted ones,
// run counts, connections, collected decisions and lines, settings.
func TestUpgradeLeavesUserDataAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdex.db")
	s := startup(t, path)
	s.SetSetting("lang", "vi")
	s.SetSetting("theme", "dusk")
	mine, _ := s.SaveCommand(store.Command{Name: "Lệnh riêng", Platform: store.Windows, Template: "Get-Date -Format o", Danger: 1})
	cmds, _ := s.Commands()
	var edited, hidden, ran store.Command
	for _, c := range cmds {
		switch {
		case c.Base && edited.ID == 0:
			edited = c
		case c.Base && hidden.ID == 0:
			hidden = c
		case c.Base && ran.ID == 0:
			ran = c
		}
	}
	edited.Name = "Bản của tôi"
	s.SaveCommand(edited)
	s.DeleteCommand(hidden.ID)
	s.AddUses(ran.ID, 5)
	s.AddUses(mine, 2)
	s.SaveConnection(store.Connection{Kind: "ssh", Name: "web-01", Host: "10.0.0.10", Port: 22, User: "root", Auth: "key"})
	s.SaveRule(store.Rule{Kind: "category", Match: "git", Result: "Code"})
	s.SetSynonym("ma nguon", []string{"git"})
	syn, _ := s.Synonyms()
	for w, alts := range syn { // the user trims a default list: it stays trimmed
		if len(alts) > 1 {
			s.SetSynonym(w, alts[:1])
			break
		}
	}
	s.SaveExplain(store.Explain{Cmd: "mytool", Summary: "của tôi"})
	c := &Collector{Store: s}
	c.Add("ping 1.1.1.1 -n 4", "powershell")
	c.Add("cd C:\\Work\\Project", "powershell")
	g, _ := s.CollectedList("")
	s.SetCollectedStatus(g[0].ID, store.StatusIgnored, 0)
	s.Close()

	before := dump(t, path)
	for i := 0; i < 2; i++ { // and again: every start
		startup(t, path).Close()
		if after := dump(t, path); !reflect.DeepEqual(before, after) {
			for tbl := range before {
				if !reflect.DeepEqual(before[tbl], after[tbl]) {
					t.Errorf("start %d changed %s:\n before %v\n after  %v", i+1, tbl, before[tbl], after[tbl])
				}
			}
			t.FailNow()
		}
	}
}

// view is what the user sees of a library, through the store (decrypted).
func view(t *testing.T, s *store.Store) string {
	t.Helper()
	var b strings.Builder
	cmds, err := s.Commands()
	if err != nil {
		t.Fatal(err)
	}
	conns, err := s.Connections()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := s.CollectedList("")
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := s.Rules()
	syn, _ := s.Synonyms()
	for _, c := range cmds {
		c.Created, c.Updated = 0, 0
		fmt.Fprintf(&b, "cmd %+v\n", c)
	}
	for _, c := range conns {
		fmt.Fprintf(&b, "conn %+v\n", c)
	}
	for _, g := range groups {
		fmt.Fprintf(&b, "group %+v\n", g)
	}
	fmt.Fprintf(&b, "rules %+v\nsynonyms %v\nlang %s theme %s\n", rules, syn, s.Setting("lang", ""), s.Setting("theme", ""))
	return b.String()
}

// Encrypting a library (first start of 0.1.6) and opening it again with its
// key show exactly the same library.
func TestEncryptionKeepsLibrary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdex.db")
	s := startup(t, path)
	s.SetSetting("theme", "dusk")
	s.SaveCommand(store.Command{Name: "Mine", Platform: store.Windows, Template: "Get-Date -Format o", Danger: 1, Tags: []string{"date"}})
	cmds, _ := s.Commands()
	edited := cmds[0]
	edited.Name = "Edited default"
	s.SaveCommand(edited)
	s.DeleteCommand(cmds[1].ID)
	s.AddUses(cmds[2].ID, 4)
	s.SaveConnection(store.Connection{Kind: "ssh", Name: "web-01", Host: "10.0.0.10", Port: 22, User: "root", Auth: "key", KeyPath: "~/.ssh/k", Platform: store.Cisco})
	s.SaveConnection(store.Connection{Kind: "serial", Name: "console", Serial: store.Serial{Port: "COM3", Baud: 9600, DataBits: 8, Parity: "none", StopBits: "1", LineEnd: "cr"}})
	c := &Collector{Store: s}
	c.Add("ping 1.1.1.1 -n 4", "powershell")
	c.Add("ping 8.8.8.8 -n 2", "powershell")
	c.Add(`cd C:\Work\Client`, "powershell")
	g, _ := s.CollectedList("")
	s.SetCollectedStatus(g[0].ID, store.StatusIgnored, 0)
	s.SaveRule(store.Rule{Kind: "category", Match: "git", Result: "Code"})
	before := view(t, s)

	key := store.NewDatabaseKey()
	if err := s.SetUpKey(store.KeyKeychain, "t", nil, key); err != nil {
		t.Fatal(err)
	}
	if after := view(t, s); after != before {
		t.Fatalf("encryption changed the library:\nbefore\n%s\nafter\n%s", before, after)
	}
	s.Close()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Unlock(key); err != nil {
		t.Fatal(err)
	}
	if err := s.UseDefaults(packs.All(), "vi"); err != nil {
		t.Fatal(err)
	}
	if again := view(t, s); again != before {
		t.Fatalf("reopened library differs:\nbefore\n%s\nagain\n%s", before, again)
	}
}
