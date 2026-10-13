package store

import (
	"errors"
	"testing"
)

func TestCheckCommandText(t *testing.T) {
	for _, ok := range []string{"ls -la", "echo 'xin chào'", "a\tb", "Get-ChildItem | Sort-Object"} {
		if err := CheckCommandText(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"ls\nrm -rf ~",           // a second command after a newline
		"echo hi\r",              // carriage return
		"echo \x1b[8mhidden",     // terminal escape
		"rm -rf /tmp/\u202Egpj.", // right-to-left override
		"cur\u200Bl evil",        // zero-width space
		"\ufeffls",               // byte order mark
	} {
		if err := CheckCommandText(bad); !errors.Is(err, ErrHiddenChars) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestHiddenCharsNeverStored(t *testing.T) {
	s := open(t)
	if _, err := s.SaveCommand(Command{Name: "x", Platform: Linux, Template: "ls\nrm -rf ~"}); !errors.Is(err, ErrHiddenChars) {
		t.Fatalf("saved: %v", err)
	}
	if _, err := s.SaveCommand(Command{Name: "x", Platform: Linux, Template: "ping {h}", Params: []Param{{Key: "h", Default: "1.1.1.1\nreboot"}}}); !errors.Is(err, ErrHiddenChars) {
		t.Fatalf("default saved: %v", err)
	}
	p, _ := ParsePack([]byte(`{"name":"evil","commands":[{"name":"looks fine","platform":"linux","template":"ls\u202e -la"},{"name":"ok","platform":"linux","template":"ls -la"}]}`))
	r, _ := s.ImportPack(p, "en")
	if r.Added != 1 {
		t.Fatalf("pack import: %+v", r)
	}
	b := Backup{Format: BackupFormat, Version: 1, Commands: []Command{{Name: "evil", Platform: Linux, Template: "id\nrm -rf ~"}}}
	br, err := s.ImportBackup(b, false)
	if err != nil || br.Added != 0 || br.Rejected != 1 {
		t.Fatalf("backup import: %+v %v", br, err)
	}
}
