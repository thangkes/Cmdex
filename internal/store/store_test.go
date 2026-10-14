package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cmdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

const packJSON = `{
  "name": "test", "version": 1,
  "commands": [
    {"name": {"en": "Ping a host", "vi": "Ping máy"}, "platform": "windows", "shell": "cmd", "template": "ping {host} -n {count}",
     "purpose": {"en": "Checks a host replies", "vi": "Kiểm tra máy có phản hồi"}, "tags": ["ping"],
     "params": [{"key": "host", "type": "host", "default": "8.8.8.8", "description": {"en": "Host", "vi": "Địa chỉ máy"}, "required": true}, {"key": "count", "type": "number", "default": "4"}]},
    {"name": "List files", "platform": "linux", "template": "ls -la {path}"}
  ],
  "explain": [{"cmd": "ping", "summary": {"en": "Sends ICMP echo", "vi": "Gửi gói ICMP"}, "flags": {"-n": {"text": {"en": "send {v} requests", "vi": "gửi {v} gói"}, "value": true}}, "args": [{"en": "target host", "vi": "máy đích"}]}],
  "synonyms": {"cong": ["port"]},
  "rules": [{"kind": "ignore", "match": "cd,ls"}]
}`

func TestImportPackIsIdempotentAndLocalized(t *testing.T) {
	s := open(t)
	p, err := ParsePack([]byte(packJSON))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ImportPack(p, "vi")
	if err != nil || r.Added != 2 || r.Explains != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = s.ImportPack(p, "vi")
	if err != nil || r.Added != 0 || r.Skipped != 2 {
		t.Fatalf("second import: %+v %v", r, err)
	}
	cmds, _ := s.Commands()
	var ping Command
	for _, c := range cmds {
		if c.Platform == Windows {
			ping = c
		}
	}
	if ping.Name != "Ping máy" || ping.Params[0].Description != "Địa chỉ máy" || ping.Source != "pack:test" {
		t.Fatalf("%+v", ping)
	}
	var ls Command
	for _, c := range cmds {
		if c.Platform == Linux {
			ls = c
		}
	}
	if len(ls.Params) != 1 || ls.Params[0].Key != "path" {
		t.Fatalf("params from template: %+v", ls.Params)
	}
	e, ok := s.Explain("PING")
	if !ok || e.Summary != "Gửi gói ICMP" || !e.Flags["-n"].Value {
		t.Fatalf("%+v", e)
	}
	syn, _ := s.Synonyms()
	if len(syn["cong"]) != 1 {
		t.Fatalf("synonyms %+v", syn)
	}
	rules, _ := s.Rules()
	if len(rules) != 1 {
		t.Fatalf("rules not deduplicated: %+v", rules)
	}
}

func TestUserExplainSurvivesPackImport(t *testing.T) {
	s := open(t)
	s.SaveExplain(Explain{Cmd: "ping", Summary: "my own words"})
	p, _ := ParsePack([]byte(packJSON))
	s.ImportPack(p, "en")
	if e, _ := s.Explain("ping"); e.Summary != "my own words" {
		t.Fatalf("pack overwrote the user's explanation: %q", e.Summary)
	}
}

func TestDuplicateTemplate(t *testing.T) {
	s := open(t)
	c := Command{Name: "a", Platform: Linux, Template: "ls"}
	if _, err := s.SaveCommand(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveCommand(c); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	c.Platform = Windows
	if _, err := s.SaveCommand(c); err != nil {
		t.Fatalf("same template on another platform: %v", err)
	}
}

func TestCollectedGroups(t *testing.T) {
	s := open(t)
	s.AddCollected("ping {host} -n {count}", "net", "powershell", false, "ping 10.0.0.1 -n 4")
	s.AddCollected("ping {host} -n {count}", "net", "powershell", false, "ping 10.0.0.1 -n 4")
	s.AddCollected("ping {host} -n {count}", "net", "powershell", false, "ping 8.8.8.8 -n 2")
	list, err := s.CollectedList(StatusPending)
	if err != nil || len(list) != 1 || list[0].Total != 3 || len(list[0].Variants) != 2 || list[0].Variants[0].Count != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	s.SetCollectedStatus(list[0].ID, StatusIgnored, 0)
	s.AddCollected("ping {host} -n {count}", "net", "powershell", false, "ping 1.1.1.1 -n 1")
	if l, _ := s.CollectedList(StatusPending); len(l) != 0 {
		t.Fatal("ignored group came back")
	}
}

func TestFillAndKeys(t *testing.T) {
	if got := Fill("ssh {user}@{host} -p {port} {x y}", map[string]string{"user": "admin", "host": "h", "port": ""}); got != "ssh admin@h -p {port} {x y}" {
		t.Fatalf("%q", got)
	}
	if k := PlaceholderKeys("a {b} {c} {b} ${d} {e f}"); len(k) != 4 || k[3] != "d" {
		t.Fatalf("%v", k)
	}
}

// A database file from elsewhere must not run functions through triggers
// or views it defines.
func TestUntrustedSchema(t *testing.T) {
	s := open(t)
	var v int
	if err := s.db.QueryRow("PRAGMA trusted_schema").Scan(&v); err != nil || v != 0 {
		t.Fatalf("trusted_schema = %d (%v), want 0", v, err)
	}
}

// Clearing the collected log drops the lines and the groups waiting for
// review; decisions stay, so those commands do not come back for review.
func TestClearCollected(t *testing.T) {
	s := open(t)
	id, _ := s.SaveCommand(Command{Name: "p", Platform: Windows, Template: "ping {host}"})
	s.AddCollected("ping {host}", "", "powershell", false, "ping 1.1.1.1")
	s.AddCollected("tracert {host}", "", "powershell", false, "tracert 1.1.1.1")
	s.AddCollected("ipconfig /all", "", "powershell", false, "ipconfig /all")
	list, _ := s.CollectedList("")
	for _, g := range list {
		switch g.Signature {
		case "ping {host}":
			s.SetCollectedAuto(g.ID, id)
		case "ipconfig /all":
			s.SetCollectedStatus(g.ID, StatusIgnored, 0)
		}
	}
	// a merged group passes its new runs on to the library command
	if tgt, err := s.AddCollected("ping {host}", "", "powershell", false, "ping 8.8.8.8"); err != nil || tgt != id {
		t.Fatalf("%d %v", tgt, err)
	}
	if tgt, _ := s.AddCollected("tracert {host}", "", "powershell", false, "tracert 8.8.8.8"); tgt != 0 {
		t.Fatalf("pending group returned target %d", tgt)
	}

	if n, err := s.ClearCollected(now() - 3600); err != nil || n != 0 {
		t.Fatalf("nothing is an hour old: %d %v", n, err)
	}
	if n, err := s.ClearCollected(0); err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	list, _ = s.CollectedList("")
	if len(list) != 2 {
		t.Fatalf("decisions must stay: %+v", list)
	}
	for _, g := range list {
		if g.Total != 0 || len(g.Variants) != 0 || g.Status == StatusPending {
			t.Fatalf("%+v", g)
		}
		if g.Signature == "ping {host}" && (!g.Auto || g.Target != id) {
			t.Fatalf("auto merge lost: %+v", g)
		}
	}
	// an ignored command run again stays ignored
	s.AddCollected("ipconfig /all", "", "powershell", false, "ipconfig /all")
	if p, _ := s.CollectedList(StatusPending); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
}

// The device type of connections is a new column: older connections keep
// every value and read as "guess" (Linux over SSH).
func TestConnectionDeviceType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	// as a database of 0.1.4 had it: schema version 3, without the column
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	old := &Store{db: db}
	if err := old.migrateTo(3); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO connections(kind, name, host, port, user, auth, key_path, serial, created) VALUES('ssh', 'web', '10.0.0.1', 22, 'root', 'key', '~/.ssh/k', '{}', 5)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, _ := s.Connections()
	if len(c) != 1 || c[0].Name != "web" || c[0].Host != "10.0.0.1" || c[0].User != "root" || c[0].KeyPath != "~/.ssh/k" || c[0].Created != 5 || c[0].Platform != "" {
		t.Fatalf("%+v", c)
	}
	if c[0].SessionPlatform() != Linux {
		t.Fatal(c[0].SessionPlatform())
	}
	c[0].Platform = Cisco
	if _, err := s.SaveConnection(c[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Connection(c[0].ID); got.SessionPlatform() != Cisco {
		t.Fatalf("%+v", got)
	}
	c[0].Platform = "both"
	if _, err := s.SaveConnection(c[0]); err == nil {
		t.Fatal("a connection cannot be Windows and Linux at once")
	}
}

// The platform filter, as chosen with the user: a Windows + Linux command
// shows when either is chosen, and hides when only devices are.
func TestShown(t *testing.T) {
	cases := []struct {
		p      string
		filter []string
		want   bool
	}{
		{Both, nil, true},
		{Both, []string{Windows}, true},
		{Both, []string{Linux}, true},
		{Both, []string{Windows, Cisco}, true},
		{Both, []string{Cisco}, false},
		{Both, []string{Cisco, PfSense}, false},
		{Windows, []string{Linux}, false},
		{Cisco, []string{Windows}, false},
		{Cisco, []string{Windows, Cisco}, true},
		{PfSense, nil, true},
	}
	for _, c := range cases {
		if got := Shown(c.p, c.filter); got != c.want {
			t.Errorf("%s with %v: %v, want %v", c.p, c.filter, got, c.want)
		}
	}
}
