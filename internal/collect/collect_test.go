package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thangkes/cmdex/internal/store"
)

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"mysql -u root -pS3cret -h 10.0.0.40":                       "mysql -u root -p" + Mask + " -h 10.0.0.40",
		"mysql -u root -p -h db":                                    "mysql -u root -p -h db",
		"curl -H 'Authorization: Bearer abc.def' https://x/api":     "curl -H 'Authorization: Bearer " + Mask + "' https://x/api",
		"git clone https://bob:hunter2@git.example.com/r.git":       "git clone https://bob:" + Mask + "@git.example.com/r.git",
		"app --password=hunter2 --user bob":                         "app --password=" + Mask + " --user bob",
		"Connect-X -Password 'p@ss w' -Server s":                    "Connect-X -Password " + Mask + " -Server s",
		"export API_KEY=sk-123":                                     "export API_KEY=" + Mask,
		`$p = ConvertTo-SecureString "hunter2" -AsPlainText -Force`: `$p = ConvertTo-SecureString ` + Mask + ` -AsPlainText -Force`,
		"sshpass -p hunter2 ssh bob@h":                              "sshpass -p " + Mask + " ssh bob@h",
		"ssh -p 2222 bob@h":                                         "ssh -p 2222 bob@h",
	}
	for in, want := range cases {
		got, _ := Redact(in, nil)
		if got != want {
			t.Errorf("%s\n got %s\nwant %s", in, got, want)
		}
	}
	if got, s := Redact("tool -k abc123 x", []string{"-k"}); got != "tool -k "+Mask+" x" || !s {
		t.Errorf("extra secret flag: %s", got)
	}
}

func TestNormalizeGroupsVariants(t *testing.T) {
	sig := func(raw string) string {
		l, ok := Normalize(raw, nil)
		if !ok {
			t.Fatalf("dropped %q", raw)
		}
		return l.Signature
	}
	a, b, c := sig("ping 192.168.1.1 -n 4"), sig("ping 10.0.0.12 -n 10"), sig("PING 8.8.8.8 -n 4")
	if a != "ping {host} -n {count}" || a != b || a != c {
		t.Fatalf("%q %q %q", a, b, c)
	}
	for raw, want := range map[string]string{
		"ssh admin@10.0.0.12 -p 2222":                      "ssh {user}@{host} -p {port}",
		"netstat -ano | findstr :8080":                     "netstat -ano | findstr :{port}",
		"Get-Service -Name Spooler | Restart-Service":      "get-service -Name {name} | restart-service",
		"scp ./build.tar.gz root@server.lan:/opt/app/":     "scp {path} {user}@{host}:{path2}",
		"curl -s https://example.com/a?b=1 -o out.txt":     "curl -s {url} -o out.txt",
		"Test-NetConnection db01.corp.local -Port 1433":    "test-netconnection {host} -Port {port}",
		"tail -n 200 /var/log/syslog":                      "tail -n {count} {path}",
		"sudo systemctl restart nginx":                     "sudo systemctl restart nginx",
		"mysql -u root -pS3cret -h 10.0.0.40":              "mysql -u root -p{secret} -h {host}",
		"copy C:\\data\\a.txt D:\\backup\\":                "copy {path} {path2}",
		"arp -d 00:1a:2b:3c:4d:5e":                         "arp -d {mac}",
		"docker run --name web -p 8080:80 nginx --port=81": "docker run --name {name} -p 8080:80 nginx --port={port}",
	} {
		if got := sig(raw); got != want {
			t.Errorf("%s\n got %s\nwant %s", raw, got, want)
		}
	}
}

func TestMatchLibrary(t *testing.T) {
	lib := []store.Command{
		{ID: 1, Name: "Ping", Template: "ping {host} -n {count}"},
		{ID: 2, Name: "Port owner", Template: "netstat -ano | findstr :{port}"},
	}
	if m := MatchLibrary("ping {host} -n {n}", lib); m.Kind != "existing" || m.Target != 1 {
		t.Fatalf("%+v", m)
	}
	if m := MatchLibrary("netstat -ano | findstr {n}", lib); m.Kind != "near" || m.Target != 2 {
		t.Fatalf("%+v", m)
	}
	if m := MatchLibrary("ipconfig /all", lib); m.Kind != "new" {
		t.Fatalf("%+v", m)
	}
}

func TestScanReadsOnlyNewLines(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SaveRule(store.Rule{Kind: "ignore", Match: "cd, ls"})
	s.SaveRule(store.Rule{Kind: "category", Match: "ping,tracert", Result: "Network"})

	lines, n := readLines(strings.NewReader("#1700000000\nping 1.1.1.1 -c 2\nls -la\nunfinished"), "bash")
	if len(lines) != 2 || lines[0] != "ping 1.1.1.1 -c 2" || n != int64(len("#1700000000\nping 1.1.1.1 -c 2\nls -la\n")) {
		t.Fatalf("%q %d", lines, n)
	}
	lines, _ = readLines(strings.NewReader(": 1700000000:0;ssh bob@h\nGet-Thing `\n  -Name x\n"), "zsh")
	if lines[0] != "ssh bob@h" || lines[1] != "Get-Thing -Name x" {
		t.Fatalf("%q", lines)
	}

	c := &Collector{Store: s}
	for _, l := range []string{"ping 1.1.1.1 -n 4", "cd C:\\x", "ls -la", "cls", "ping 8.8.8.8 -n 2"} {
		c.Add(l, "test")
	}
	list, _ := s.CollectedList("")
	if len(list) != 1 || list[0].Total != 2 || list[0].Category != "Network" {
		t.Fatalf("%+v", list)
	}

	// file offsets: append, re-scan, shrink
	hist := filepath.Join(dir, "h.txt")
	os.WriteFile(hist, []byte("ipconfig /all\n"), 0o600)
	scanFile := func() int {
		key := "collect.offset." + hist
		_ = key
		n, _ := c.scanPath("powershell", hist, c.rules())
		return n
	}
	if scanFile() != 1 || scanFile() != 0 {
		t.Fatal("rescan counted twice")
	}
	f, _ := os.OpenFile(hist, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("ipconfig /all\nnslookup example.com\n")
	f.Close()
	if got := scanFile(); got != 2 {
		t.Fatalf("appended: %d", got)
	}
	os.WriteFile(hist, []byte("x y\n"), 0o600)
	if got := scanFile(); got != 0 {
		t.Fatalf("shrunk file re-read: %d", got)
	}
}
