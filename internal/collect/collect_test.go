package collect

import (
	"os"
	"path/filepath"
	"strconv"
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
		"username admin privilege 15 secret P@ssw0rd":               "username admin privilege 15 secret " + Mask,
		"enable secret 5 $1$abcd$xyz":                               "enable secret 5 " + Mask,
		"snmp-server community s3cr3t RO":                           "snmp-server community " + Mask + " RO",
		"crypto key generate rsa modulus 2048":                      "crypto key generate rsa modulus 2048",
		"show running-config | include secret":                      "show running-config | include secret",
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
	if m := MatchLibrary("ping {host} -n {n}", lib, DefaultSimilarity); m.Kind != "existing" || m.Target != 1 {
		t.Fatalf("%+v", m)
	}
	if m := MatchLibrary("netstat -ano | findstr {n}", lib, DefaultSimilarity); m.Kind != "near" || m.Target != 2 {
		t.Fatalf("%+v", m)
	}
	if m := MatchLibrary("ipconfig /all", lib, DefaultSimilarity); m.Kind != "new" {
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
	// cd and ls are ignored by the user's rule here; a lone "cls" is kept
	if len(list) != 2 || list[0].Total != 2 || list[0].Category != "Network" || list[1].Signature != "cls" {
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

// Secrets in real-world command lines never reach the database.
func TestRedactKeepsNoSecret(t *testing.T) {
	secrets := []string{"S3cretPass", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "glpat-abcdefghijklmnop1234",
		"sk-proj-abcdefghijklmnopqrstuvwxyz0123", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "xoxb-1234567890-abcdefghijkl",
		"abcdef0123456789", "P@ssw0rd", "P@ss", "eyJhbGciOiJIUzI1NiJ9"}
	for _, in := range []string{
		"curl -u admin:S3cretPass https://x/api",
		"curl --user admin:S3cretPass https://x/api",
		"git clone https://ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@github.com/a/b.git",
		"git push https://oauth2:glpat-abcdefghijklmnop1234@gitlab.com/a/b.git",
		"export GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		"curl -H 'X-Api-Key: sk-proj-abcdefghijklmnopqrstuvwxyz0123' https://api",
		"aws configure set aws_secret_access_key wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"slack-cli post xoxb-1234567890-abcdefghijkl",
		"curl -H 'Cookie: session=abcdef0123456789' https://x",
		`net use Z: \\server\share P@ssw0rd /user:corp\bob`,
		"docker login -u bob -p S3cretPass registry.io",
		"psql postgresql://bob:S3cretPass@db:5432/app",
		"New-LocalUser bob -Password (ConvertTo-SecureString 'P@ss' -AsPlainText -Force)",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnopqrstuvwxyz",
	} {
		out, secret := Redact(in, nil)
		if !secret {
			t.Errorf("not flagged: %s", in)
		}
		for _, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("%q leaks %q", out, s)
			}
		}
	}
	// and nothing harmless is hidden
	for _, in := range []string{"ssh -p 2222 bob@h", "docker run -p 8080:80 nginx", "curl -u bob https://x", "git clone https://github.com/a/b.git", `net use Z: \\srv\share /persistent:yes`} {
		if out, _ := Redact(in, nil); out != in {
			t.Errorf("masked a harmless line: %s -> %s", in, out)
		}
	}
}

// Lines stored by an older Cmdex (before a masking rule existed) are cleaned
// once and regrouped; the second run does nothing.
func TestRescrubOldData(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// as an old version would have stored them, secret in clear
	s.AddCollected("curl -u admin:S3cretPass {url}", "", "bash", false, "curl -u admin:S3cretPass https://a/x")
	s.AddCollected("curl -u admin:S3cretPass {url}", "", "bash", false, "curl -u admin:S3cretPass https://a/x")
	s.AddCollected("git clone https://ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@github.com/a/b.git", "", "bash", false,
		"git clone https://ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@github.com/a/b.git")
	id, _ := s.SaveCommand(store.Command{Name: "x", Platform: store.Linux, Template: "curl -u admin:S3cretPass {url}", Source: "collected"})

	c := &Collector{Store: s}
	n, err := c.Rescrub()
	if err != nil || n == 0 {
		t.Fatalf("%d %v", n, err)
	}
	list, _ := s.CollectedList("")
	for _, g := range list {
		for _, v := range g.Variants {
			if strings.Contains(v.Raw, "S3cret") || strings.Contains(v.Raw, "ghp_") || strings.Contains(g.Signature, "S3cret") || strings.Contains(g.Signature, "ghp_") {
				t.Fatalf("secret left: %+v", g)
			}
		}
		if strings.HasPrefix(g.Signature, "curl") && (g.Total != 2 || !g.Secret) {
			t.Fatalf("count or flag lost: %+v", g)
		}
	}
	if cmd, _ := s.Command(id); strings.Contains(cmd.Template, "S3cret") {
		t.Fatalf("collected command keeps the secret: %q", cmd.Template)
	}
	if n, _ := c.Rescrub(); n != 0 {
		t.Fatal("ran twice")
	}
}

// Places given to cd, dir, ls… become {path}; the moves every newcomer
// learns (.., -, ~) stay as they are.
func TestLocationTemplates(t *testing.T) {
	cases := map[string]string{
		`cd Desktop`:                  "cd {path}",
		`cd "C:\Program Files\Cmdex"`: "cd {path}",
		`cd C:\Program Files\My App`:  "cd {path}",
		`dir C:\a D:\b`:               "dir {path} {path2}",
		`cd /d D:\Work`:               "cd /d {path}",
		`cd ..`:                       "cd ..",
		`cd -`:                        "cd -",
		`cd ~`:                        "cd ~",
		`cd`:                          "cd",
		`ls -la Documents`:            "ls -la {path}",
		`ls *.log`:                    "ls {path}",
		`dir Downloads /s`:            "dir {path} /s",
		`Get-ChildItem -Path C:\Logs -Filter *.log`: "get-childitem -Path {path} -Filter {filter}",
		`gci src -Recurse -Depth 2`:                 "gci {path} -Recurse -Depth {depth}",
		`tree -L 2 projects`:                        "tree -L {l} {path}",
		`Set-Location Projects; dir`:                "set-location {path} ; dir",
		`cd app && npm install express`:             "cd {path} && npm install express",
	}
	for in, want := range cases {
		l, ok := Normalize(in, nil)
		if !ok || l.Signature != want {
			t.Errorf("%s\n got %s\nwant %s", in, l.Signature, want)
		}
	}
}

// Lines Cmdex 0.1 skipped in already-read history are collected once, and
// what it collected then is not counted again.
func TestBackfill(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := &Collector{Store: s}
	hist := filepath.Join(dir, "h.txt")
	read := "cd Desktop\nping 1.1.1.1 -n 4\nls\ncd ..\n"
	os.WriteFile(hist, []byte(read+"cd Music\n"), 0o600) // the last line was not read yet
	c.Add("ping 1.1.1.1 -n 4", "bash")                   // collected by 0.1
	s.SetSetting("collect.offset."+hist, strconv.Itoa(len(read)))

	all := func(string) bool { return true }
	n, err := c.backfill([]Source{{ID: "bash", Path: hist}}, all)
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	if n, _ := c.backfill([]Source{{ID: "bash", Path: hist}}, all); n != 0 {
		t.Fatal("ran twice")
	}
	list, _ := s.CollectedList("")
	got := map[string]int{}
	for _, g := range list {
		got[g.Signature] = g.Total
	}
	if len(got) != 4 || got["ping {host} -n {count}"] != 1 || got["cd {path}"] != 1 || got["ls"] != 1 || got["cd .."] != 1 {
		t.Fatalf("%+v", got)
	}
}

// Runs of a command merged into the library count on that command.
func TestMergedRunsCount(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.SaveCommand(store.Command{Name: "p", Platform: store.Windows, Template: "ping {host}"})
	c := &Collector{Store: s}
	c.Add("ping 1.1.1.1", "powershell")
	g, _ := s.CollectedList("")
	s.SetCollectedStatus(g[0].ID, store.StatusMerged, id)
	c.Add("ping 8.8.8.8", "powershell")
	c.Add("ping 9.9.9.9", "powershell")
	if cmd, _ := s.Command(id); cmd.Uses != 2 {
		t.Fatalf("uses %d, want 2", cmd.Uses)
	}
}
