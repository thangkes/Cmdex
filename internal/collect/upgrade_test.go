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
