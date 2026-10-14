package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// secrets that must not be readable in the files once encrypted
var plainWitnesses = []string{"10.66.77.88", "ops-admin", "~/.ssh/witness_key", "Witness Server", "COM77",
	"curl https://witness.example/api", "witness-template {host}", "My witness command"}

// byKind finds the ssh and the serial profile of the witnesses.
func byKind(cs []Connection) (ssh, serial Connection) {
	for _, c := range cs {
		if c.Kind == "ssh" {
			ssh = c
		} else {
			serial = c
		}
	}
	return
}

func fillWitnesses(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.SaveConnection(Connection{Kind: "ssh", Name: "Witness Server", Host: "10.66.77.88", Port: 22, User: "ops-admin", Auth: "key", KeyPath: "~/.ssh/witness_key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConnection(Connection{Kind: "serial", Name: "console", Serial: Serial{Port: "COM77", Baud: 9600}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveCommand(Command{Name: "My witness command", Platform: Linux, Template: "witness-template {host}", Notes: "notes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddCollected("curl {url}", "", "bash", false, "curl https://witness.example/api"); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdex.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fillWitnesses(t, s)
	if _, err := s.db.Exec("VACUUM INTO ?", s.BackupPath()); err != nil { // a backup copy, as on upgrade
		t.Fatal(err)
	}
	if !fileHas(t, path, "10.66.77.88") {
		t.Fatal("test setup: plain values expected before encryption")
	}
	key := NewDatabaseKey()
	if err := s.SetUpKey(KeyKeychain, "test", nil, key); err != nil {
		t.Fatal(err)
	}
	for _, w := range plainWitnesses {
		if fileHas(t, path, w) || fileHas(t, s.BackupPath(), w) {
			t.Errorf("%q readable in the files after encryption", w)
		}
	}

	// everything still works through the key
	conns, _ := s.Connections()
	ssh, serial := byKind(conns)
	if len(conns) != 2 || ssh.Host != "10.66.77.88" || ssh.User != "ops-admin" || ssh.KeyPath != "~/.ssh/witness_key" || serial.Serial.Port != "COM77" {
		t.Fatalf("%+v", conns)
	}
	cmds, _ := s.Commands()
	if len(cmds) != 1 || cmds[0].Template != "witness-template {host}" || cmds[0].Name != "My witness command" {
		t.Fatalf("%+v", cmds)
	}
	if _, err := s.SaveCommand(Command{Name: "dup", Platform: Linux, Template: "witness-template {host}"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate not found through the lookup hash: %v", err)
	}
	s.AddCollected("curl {url}", "", "bash", false, "curl https://witness.example/api") // same group, same line
	g, _ := s.CollectedList("")
	if len(g) != 1 || g[0].Total != 2 || len(g[0].Variants) != 1 || g[0].Variants[0].Raw != "curl https://witness.example/api" {
		t.Fatalf("%+v", g)
	}
	b, err := s.ExportBackup("t")
	if err != nil || len(b.Connections) != 2 || b.Collected[0].Variants[0].Raw != "curl https://witness.example/api" {
		t.Fatalf("%+v %v", b, err)
	}
	s.Close()

	// opening again needs the key
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if mode, id := s.KeyState(); mode != KeyKeychain || id != "test" || !s.Locked() {
		t.Fatalf("%s %s locked=%v", mode, id, s.Locked())
	}
	if _, err := s.Connections(); !errors.Is(err, ErrLocked) {
		t.Fatalf("read without the key: %v", err)
	}
	if err := s.Unlock(NewDatabaseKey()); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("wrong key: %v", err)
	}
	if err := s.Unlock(key); err != nil {
		t.Fatal(err)
	}
	if conns, _ := s.Connections(); len(conns) != 2 {
		t.Fatalf("%+v", conns)
	} else if ssh, _ := byKind(conns); ssh.Host != "10.66.77.88" {
		t.Fatalf("%+v", conns)
	}

	// a copy made here imports here; elsewhere it does not open
	file := filepath.Join(t.TempDir(), "copy.db")
	if err := s.ExportDatabase(file); err != nil {
		t.Fatal(err)
	}
	if b, err := s.BackupFromDatabase(file, t.TempDir()); err != nil || len(b.Connections) != 2 {
		t.Fatalf("%v %+v", err, b)
	}
	other := open(t)
	other.SetUpKey(KeyKeychain, "other", nil, NewDatabaseKey())
	if _, err := other.BackupFromDatabase(file, t.TempDir()); !errors.Is(err, ErrForeignKey) {
		t.Fatalf("foreign copy: %v", err)
	}
}

// A master password derives the key; a JSON backup round-trips through an
// encrypted database without leaving its key behind.
func TestPasswordKeyAndBackup(t *testing.T) {
	s := open(t)
	fillWitnesses(t, s)
	salt := []byte("0123456789abcdef")
	if err := s.SetUpKey(KeyPassword, "", salt, PasswordKey("correct horse battery", salt)); err != nil {
		t.Fatal(err)
	}
	if string(s.PasswordSalt()) != string(salt) {
		t.Fatal("salt")
	}
	b, _ := s.ExportBackup("t")
	for k := range b.Settings {
		if len(k) >= 7 && k[:7] == "crypto." {
			t.Fatalf("key setting %s in a backup", k)
		}
	}
	d := open(t)
	d.SetUpKey(KeyKeychain, "d", nil, NewDatabaseKey())
	if _, err := d.ImportBackup(b, true); err != nil {
		t.Fatal(err)
	}
	if m, _ := d.KeyState(); m != KeyKeychain || d.Locked() {
		t.Fatal("a replace import touched the key")
	}
	if c, _ := d.Connections(); len(c) != 2 {
		t.Fatalf("%+v", c)
	} else if ssh, _ := byKind(c); ssh.Host != "10.66.77.88" {
		t.Fatalf("%+v", c)
	}
	if g, _ := d.CollectedList(""); len(g) != 1 || g[0].Variants[0].Raw != "curl https://witness.example/api" {
		t.Fatalf("%+v", g)
	}
}
