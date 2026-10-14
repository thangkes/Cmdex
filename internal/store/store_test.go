package store

import (
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
