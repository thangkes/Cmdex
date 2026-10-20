package mask

import (
	"strings"
	"testing"
)

func all(cfg Config, lines ...string) (*State, []string) {
	s := Compile(cfg).New()
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = s.Line(l)
	}
	return s, out
}

func TestMaskOutput(t *testing.T) {
	cases := []struct{ in, want string }{
		// secrets
		{"username admin privilege 15 secret 5 $1$mERr$hx5rVt7rPNoS4wqbXKX7m0", "username USER-1 privilege 15 secret 5 " + Hidden},
		{"enable password 7 0822455D0A16", "enable password 7 " + Hidden},
		{"snmp-server community s3cretC0mm RO", "snmp-server community " + Hidden + " RO"},
		{" key-string MyKeyChainPass", " key-string " + Hidden},
		{"crypto isakmp key VpnPsk123 address IP-1", "crypto isakmp key " + Hidden + " address IP-1"},
		{"    Key Content            : wifi-pass 123", "    Key Content            : " + Hidden},
		{"\t\t<password>hunter2</password>", "\t\t<password>" + Hidden + "</password>"},
		{"root:$6$abcdefgh$0123456789abcdef:19000:0:99999:7:::", "root:" + Hidden + ":19000:0:99999:7:::"},
		{"export GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123", "export GITHUB_TOKEN=" + Hidden},
		{"wpa_passphrase=correct horse", "wpa_passphrase=" + Hidden + " horse"},
		// addresses
		{"Internet address is 10.20.30.40/24", "Internet address is IP-1/24"},
		{"ip address 10.20.30.40 255.255.255.0", "ip address IP-1 255.255.255.0"},
		{"permit ip 0.0.0.0 0.0.0.255 any", "permit ip 0.0.0.0 0.0.0.255 any"},
		{"ping 127.0.0.1", "ping 127.0.0.1"},
		{"Version 15.2.4.5.6", "Version 15.2.4.5.6"},
		{"Link-local IPv6 Address . . . . . : fe80::1c2d:3e4f:5a6b:7c8d%12", "Link-local IPv6 Address . . . . . : IP-1"},
		{"time 10:30:45 std::string ::1", "time 10:30:45 std::string ::1"},
		{"Physical Address. . . . . . . . . : 00-1A-2B-3C-4D-5E", "Physical Address. . . . . . . . . : MAC-1"},
		{"  10    0011.2233.4455    DYNAMIC     Gi0/1", "  10    MAC-1    DYNAMIC     Gi0/1"},
		{"ff:ff:ff:ff:ff:ff", "ff:ff:ff:ff:ff:ff"},
		{"contact ops.team@example.com now", "contact EMAIL-1 now"},
		// identity
		{"Processor board ID FOC1234X5YZ", "Processor board ID SN-1"},
		{"NAME: \"1\", DESCR: \"WS-C2960\"  PID: WS-C2960-24TT-L , VID: V02 , SN: FOC1234X5YZ", "NAME: \"1\", DESCR: \"WS-C2960\"  PID: WS-C2960-24TT-L , VID: V02 , SN: SN-1"},
		{"Serial Number: To Be Filled By O.E.M.", "Serial Number: To Be Filled By O.E.M."},
		{"PS C:\\Users\\Alice\\Desktop> dir", "PS C:\\Users\\USER-1\\Desktop> dir"},
		{"alice@build-box:~$ ls /home/alice/src", "USER-1@HOST-1:~$ ls /home/USER-1/src"},
		{"hostname CORE-SW-HCM", "hostname HOST-1"},
		{"CORE-SW-HCM#show ver", "HOST-1#show ver"},
		{"SW2(config-if)# shutdown", "HOST-1(config-if)# shutdown"},
		{"interface GigabitEthernet0/1", "interface GigabitEthernet0/1"},
	}
	for _, c := range cases {
		_, out := all(Config{}, c.in)
		if out[0] != c.want {
			t.Errorf("%q\n got %q\nwant %q", c.in, out[0], c.want)
		}
	}
}

func TestPseudonymsStayTheSame(t *testing.T) {
	s, out := all(Config{},
		"Neighbor 10.0.0.1 up", "Neighbor 10.0.0.2 down", "ping 10.0.0.1",
		"mac 00:11:22:33:44:55", "mac 0011.2233.4455")
	want := []string{"Neighbor IP-1 up", "Neighbor IP-2 down", "ping IP-1", "mac MAC-1", "mac MAC-1"}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, out[i], want[i])
		}
	}
	st := s.Stats()
	if st[IP] != 2 || st[MAC] != 1 || s.Total() != 3 {
		t.Errorf("stats %v total %d", st, s.Total())
	}
	m := s.Mapping()
	if len(m) != 3 {
		t.Fatalf("mapping %v", m)
	}
	for _, p := range m {
		if p.Name == "IP-2" && p.Value != "10.0.0.2" {
			t.Errorf("IP-2 = %q", p.Value)
		}
	}
	s.Erase()
	if len(s.Mapping()) != 0 || s.Total() != 0 {
		t.Error("erase kept values")
	}
}

func TestLearnedValuesAreMaskedLater(t *testing.T) {
	_, out := all(Config{}, "hostname EDGE-RTR-07", "Building configuration for edge-rtr-07 ...", "EDGE-RTR-07-backup stays")
	if out[1] != "Building configuration for HOST-1 ..." {
		t.Errorf("got %q", out[1])
	}
	if out[2] != "EDGE-RTR-07-backup stays" && out[2] != "HOST-1-backup stays" {
		t.Errorf("got %q", out[2])
	}
}

func TestPrivateKeyBlock(t *testing.T) {
	s, out := all(Config{}, "before", "-----BEGIN OPENSSH PRIVATE KEY-----", "b3BlbnNzaC1rZXktdjEAAAAA", "AAAAAAAAAAAAAAAAAAAAAAAA", "-----END OPENSSH PRIVATE KEY-----", "after")
	want := []string{"before", "-----BEGIN OPENSSH PRIVATE KEY-----", Hidden, Hidden, "-----END OPENSSH PRIVATE KEY-----", "after"}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, out[i], want[i])
		}
	}
	if s.Stats()[Key] != 1 {
		t.Errorf("stats %v", s.Stats())
	}
}

func TestSerialTableHeader(t *testing.T) {
	_, out := all(Config{}, "SerialNumber", "", "5CD1234XYZ  ", "next")
	if out[2] != "SN-1  " || out[3] != "next" {
		t.Errorf("got %q", out)
	}
}

func TestCategoriesOff(t *testing.T) {
	_, out := all(Config{Cats: map[string]bool{IP: false, Secret: false}}, "ip 10.1.1.1 password hunter2")
	if out[0] != "ip 10.1.1.1 password hunter2" {
		t.Errorf("got %q", out[0])
	}
	_, out = all(Config{Cats: map[string]bool{Domain: true}}, "resolve git.example.com and notes.txt")
	if out[0] != "resolve HOST-1 and notes.txt" {
		t.Errorf("got %q", out[0])
	}
}

func TestKnownValues(t *testing.T) {
	_, out := all(Config{Known: map[string][]string{Host: {"DESKTOP-7QK2"}, User: {"Alice"}}},
		"Host is desktop-7qk2 owner Alice", "show admin alice")
	if out[0] != "Host is HOST-1 owner USER-1" {
		t.Errorf("got %q", out[0])
	}
	if out[1] != "show admin alice" { // a known user name is matched with its exact case only
		t.Errorf("got %q", out[1])
	}
}

func TestUserRules(t *testing.T) {
	rules := []Rule{
		{Kind: "text", Value: "Acme Corp"},
		{Kind: "after", Value: "site-code", Label: "site"},
		{Kind: "pattern", Value: `ticket #(\d{4,})`, Hide: true},
	}
	s, out := all(Config{Rules: rules}, "Customer ACME corp, site-code HN04 ticket #123456", "again HN04")
	if out[0] != "Customer TEXT-1, site-code SITE-1 ticket #"+Hidden {
		t.Errorf("got %q", out[0])
	}
	if out[1] != "again SITE-1" {
		t.Errorf("got %q", out[1])
	}
	if st := s.Stats(); st["site"] != 1 || st["text"] != 2 || st[""] != 0 {
		t.Errorf("stats %v", st)
	}
}

func TestRuleCheck(t *testing.T) {
	bad := []Rule{
		{Kind: "text", Value: "ab"},
		{Kind: "pattern", Value: `a*`},
		{Kind: "pattern", Value: `(`},
		{Kind: "script", Value: "abc"},
		{Kind: "text", Value: "abc", Label: "<b>"},
		{Kind: "text", Value: strings.Repeat("x", 400)},
	}
	for _, r := range bad {
		if r.Check() == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	if err := (Rule{Kind: "after", Value: "vpn-psk", Label: "Mã site"}).Check(); err != nil {
		t.Error(err)
	}
	// a bad rule never reaches the masker
	_, out := all(Config{Rules: []Rule{{Kind: "pattern", Value: `x*`}}}, "xxx")
	if out[0] != "xxx" {
		t.Errorf("got %q", out[0])
	}
}

func TestPseudonymsAreNotMaskedAgain(t *testing.T) {
	_, out := all(Config{Rules: []Rule{{Kind: "text", Value: "IP-1"}}}, "a 10.9.9.9 b")
	if out[0] != "a IP-1 b" {
		t.Errorf("got %q", out[0])
	}
}
