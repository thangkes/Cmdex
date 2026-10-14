package store

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
)

// fileHas reports whether the database file (journal included) holds s.
func fileHas(t *testing.T, path, s string) bool {
	t.Helper()
	for _, p := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(p)
		if err == nil && bytes.Contains(b, []byte(s)) {
			return true
		}
	}
	return false
}

// The backup copy gets the database's care: private, secrets masked, and
// cleared with the collected log; and cleared lines leave the disk.
func TestBackupCopyAndErase(t *testing.T) {
	s := open(t)
	s.SaveCommand(Command{Name: "keep me", Platform: Linux, Template: "uptime"})
	s.SaveConnection(Connection{Kind: "ssh", Name: "web", Host: "10.0.0.1", Port: 22})
	s.AddCollected("curl -u admin:{secret} {url}", "", "bash", true, "curl -u admin:S3cretValue https://x.example/a")
	s.AddCollected("cd {path}", "", "bash", false, "cd /home/me/CustomerProject")
	if _, err := s.db.Exec("VACUUM INTO ?", s.BackupPath()); err != nil {
		t.Fatal(err)
	}

	ok, err := s.ScrubBackup(func(raw string) (string, string, bool, bool) {
		line := strings.ReplaceAll(raw, "S3cretValue", "••••••")
		if line == raw {
			return raw, "", false, true
		}
		return line, "curl -u admin:{secret} {url}", true, true
	}, func(tpl string) string { return tpl })
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if fileHas(t, s.BackupPath(), "S3cretValue") {
		t.Fatal("secret left in the backup copy")
	}
	if fi, _ := os.Stat(s.BackupPath()); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup copy mode %v", fi.Mode().Perm())
	}
	bak, _ := s.openBackup()
	var cmds, conns int
	bak.QueryRow("SELECT COUNT(*) FROM commands").Scan(&cmds)
	bak.QueryRow("SELECT COUNT(*) FROM connections").Scan(&conns)
	bak.Close()
	if cmds != 1 || conns != 1 {
		t.Fatalf("backup copy lost data: %d commands, %d connections", cmds, conns)
	}

	if !fileHas(t, s.path, "CustomerProject") {
		t.Fatal("test setup: the line should be in the file before clearing")
	}
	if n, err := s.ClearCollected(0); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	if fileHas(t, s.path, "CustomerProject") || fileHas(t, s.BackupPath(), "CustomerProject") {
		t.Fatal("a cleared line can still be read from the disk")
	}
	if c, _ := s.Commands(); len(c) != 1 {
		t.Fatalf("library touched: %+v", c)
	}
	if c, _ := s.Connections(); len(c) != 1 {
		t.Fatalf("connections touched: %+v", c)
	}
}

// A backup cannot pass its commands off as starter commands, hide a
// starter command behind one of its own, or make one ask less.
func TestImportCannotSpoofDefaults(t *testing.T) {
	s := withDefaults(t, "en")
	ping := find(t, s, Windows)
	b := Backup{Format: BackupFormat, Version: 2, Commands: []Command{
		// same name as the default, other line, claims to come from the pack
		{Name: ping.Name, Platform: Windows, Template: "ping {host} -n {count} -l 65500", Source: "pack:test", Created: 1, Updated: 2},
		// claims to be the user's copy of the default, runs something else
		{Name: "ls", Platform: Linux, Template: "rm -rf {path}", BaseKey: CommandKey(Linux, "ls -la {path}"), Created: 1, Updated: 1},
	}}
	if _, err := s.ImportBackup(b, false); err != nil {
		t.Fatal(err)
	}
	cmds, _ := s.Commands()
	if len(cmds) != 4 {
		t.Fatalf("starter commands hidden: %+v", cmds)
	}
	for _, c := range cmds {
		if !c.Base && (c.BaseKey != "" || strings.HasPrefix(c.Source, "pack:")) {
			t.Fatalf("imported command passes as a starter command: %+v", c)
		}
	}

	// an honest copy (same line) keeps its link
	b = Backup{Format: BackupFormat, Version: 2, Commands: []Command{
		{Name: "My ping", Platform: Windows, Template: ping.Template, BaseKey: CommandKey(Windows, ping.Template), Danger: 0, Created: 1, Updated: 1},
	}}
	s2 := withDefaults(t, "en")
	if _, err := s2.ImportBackup(b, false); err != nil {
		t.Fatal(err)
	}
	if got := find(t, s2, Windows); got.Base || got.BaseKey == "" || got.Name != "My ping" {
		t.Fatalf("%+v", got)
	}
}
