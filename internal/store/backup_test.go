package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fill makes a library with something in every table.
func fill(t *testing.T, s *Store) {
	t.Helper()
	p, _ := ParsePack([]byte(packJSON))
	s.ImportPack(p, "vi")
	id, err := s.SaveCommand(Command{Name: "Mine", Platform: Linux, Template: "df -h {path}", Tags: []string{"disk"}, Envs: []string{"Lab"}, Params: []Param{{Key: "path", Default: "/"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.AddUses(id, 5)
	s.SaveExplain(Explain{Cmd: "df", Summary: "my words", Source: "user"})
	s.SaveConnection(Connection{Kind: "ssh", Name: "web", Host: "10.0.0.5", Port: 22, User: "root", Auth: "key", KeyPath: "~/.ssh/id"})
	s.SaveConnection(Connection{Kind: "serial", Name: "router", Serial: Serial{Port: "COM3", Baud: 9600}})
	s.AddCollected("ping {host} -c {count}", "Mạng", "bash", false, "ping 1.1.1.1 -c 2")
	s.AddCollected("ping {host} -c {count}", "Mạng", "bash", false, "ping 1.1.1.1 -c 2")
	s.AddCollected("df -h {path}", "", "bash", false, "df -h /var")
	list, _ := s.CollectedList("")
	for _, g := range list {
		if g.Signature == "df -h {path}" {
			s.SetCollectedStatus(g.ID, StatusMerged, id)
		}
	}
	s.SetSetting("lang", "vi")
	s.SetSetting("theme", "dusk")
	s.SetSetting("collect.offset.C:/x/history", "123") // machine-specific: must not travel
}

func counts(t *testing.T, s *Store) map[string]int {
	t.Helper()
	m := map[string]int{}
	for _, tb := range []string{"commands", "explain", "rules", "synonyms", "connections", "collected", "variants"} {
		var n int
		s.db.QueryRow("SELECT COUNT(*) FROM " + tb).Scan(&n)
		m[tb] = n
	}
	return m
}

func TestBackupRoundTrip(t *testing.T) {
	a := open(t)
	fill(t, a)
	b, err := a.ExportBackup("0.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Settings["collect.offset.C:/x/history"]; ok {
		t.Fatal("machine-specific setting exported")
	}
	data, _ := json.Marshal(b)
	if !IsBackup(data) {
		t.Fatal("not recognized as a backup")
	}
	b2, err := ParseBackup(data)
	if err != nil {
		t.Fatal(err)
	}

	dst := open(t)
	r, err := dst.ImportBackup(b2, false)
	if err != nil {
		t.Fatal(err)
	}
	got, want := counts(t, dst), counts(t, a)
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %d, want %d (%+v)", k, got[k], v, r)
		}
	}
	if dst.Setting("theme", "") != "dusk" || dst.Setting("collect.offset.C:/x/history", "none") != "none" {
		t.Fatal("settings")
	}
	cmds, _ := dst.Commands()
	var mine Command
	for _, c := range cmds {
		if c.Template == "df -h {path}" {
			mine = c
		}
	}
	if mine.Uses != 5 || len(mine.Envs) != 1 || mine.Params[0].Default != "/" {
		t.Fatalf("command lost details: %+v", mine)
	}
	list, _ := dst.CollectedList(StatusMerged)
	if len(list) != 1 || list[0].Target != mine.ID {
		t.Fatalf("merged group lost its target: %+v", list)
	}
	if e, _ := dst.Explain("df"); e.Summary != "my words" {
		t.Fatal("explanation")
	}

	// importing the same backup again changes nothing
	before := counts(t, dst)
	r, err = dst.ImportBackup(b2, false)
	if err != nil || r.Added != 0 || r.Connections != 0 || r.Collected != 0 || r.Rules != 0 {
		t.Fatalf("second import: %+v %v", r, err)
	}
	if after := counts(t, dst); after["variants"] != before["variants"] || after["connections"] != before["connections"] {
		t.Fatalf("second import changed counts: %v -> %v", before, after)
	}
	var cnt int
	dst.db.QueryRow("SELECT count FROM variants WHERE raw = ?", "ping 1.1.1.1 -c 2").Scan(&cnt)
	if cnt != 2 {
		t.Fatalf("run count doubled: %d", cnt)
	}
}

func TestBackupMergeKeepsNewerAndLocal(t *testing.T) {
	a := open(t)
	fill(t, a)
	b, _ := a.ExportBackup("")

	dst := open(t)
	id, _ := dst.SaveCommand(Command{Name: "Local name", Platform: Linux, Template: "df -h {path}"})
	dst.db.Exec("UPDATE commands SET updated = updated + 1000 WHERE id = ?", id) // edited later here
	dst.SaveExplain(Explain{Cmd: "df", Summary: "local words", Source: "user"})
	dst.SaveCommand(Command{Name: "Only here", Platform: Windows, Template: "dir"})

	if _, err := dst.ImportBackup(b, false); err != nil {
		t.Fatal(err)
	}
	c, _ := dst.Command(id)
	if c.Name != "Local name" || c.Uses != 5 {
		t.Fatalf("merge: %+v", c)
	}
	if e, _ := dst.Explain("df"); e.Summary != "local words" {
		t.Fatal("local explanation overwritten in merge")
	}
	cmds, _ := dst.Commands()
	found := false
	for _, x := range cmds {
		found = found || x.Template == "dir"
	}
	if !found {
		t.Fatal("merge removed a local command")
	}

	// an older local copy is replaced by the newer one from the backup
	dst.db.Exec("UPDATE commands SET updated = 1 WHERE id = ?", id)
	dst.ImportBackup(b, false)
	if c, _ := dst.Command(id); c.Name != "Mine" {
		t.Fatalf("newer copy not taken: %q", c.Name)
	}
}

func TestBackupReplace(t *testing.T) {
	a := open(t)
	fill(t, a)
	b, _ := a.ExportBackup("")
	dst := open(t)
	dst.SaveCommand(Command{Name: "Only here", Platform: Windows, Template: "dir"})
	dst.SetSetting("collect.offset.D:/h", "77")
	if _, err := dst.ImportBackup(b, true); err != nil {
		t.Fatal(err)
	}
	got, want := counts(t, dst), counts(t, a)
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %d, want %d", k, got[k], v)
		}
	}
	if dst.Setting("collect.offset.D:/h", "") != "77" {
		t.Fatal("this computer's history positions were dropped")
	}
}

func TestDatabaseFileExportAndImport(t *testing.T) {
	a := open(t)
	fill(t, a)
	dir := t.TempDir()
	file := filepath.Join(dir, "export.db")
	if err := a.ExportDatabase(file); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(file)
	b, err := BackupFromDatabase(file, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatal("source database file was changed")
	}
	dst := open(t)
	if _, err := dst.ImportBackup(b, false); err != nil {
		t.Fatal(err)
	}
	if got, want := counts(t, dst), counts(t, a); got["commands"] != want["commands"] || got["collected"] != want["collected"] {
		t.Fatalf("%v vs %v", got, want)
	}
	os.WriteFile(filepath.Join(dir, "junk.db"), []byte("not sqlite at all, just text"), 0o600)
	if _, err := BackupFromDatabase(filepath.Join(dir, "junk.db"), t.TempDir()); err == nil {
		t.Fatal("junk accepted")
	}
}

func TestParseBackupRejects(t *testing.T) {
	if _, err := ParseBackup([]byte(`{"format":"cmdex-backup","version":99}`)); err == nil {
		t.Fatal("future version accepted")
	}
	if _, err := ParseBackup([]byte(packJSON)); err == nil {
		t.Fatal("pack accepted as backup")
	}
}
