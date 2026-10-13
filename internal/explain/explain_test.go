package explain

import (
	"strings"
	"testing"

	"github.com/thangkes/cmdex/internal/store"
)

func kb(prog string) (store.Explain, bool) {
	m := map[string]store.Explain{
		"netstat":  {Cmd: "netstat", Summary: "shows connections", Letters: map[string]string{"a": "all", "n": "numeric", "o": "pid"}},
		"findstr":  {Cmd: "findstr", Summary: "keeps matching lines", Args: []string{"text to find"}, Flags: map[string]store.Flag{"/i": {Text: "ignore case"}}},
		"ssh":      {Cmd: "ssh", Summary: "remote shell", Flags: map[string]store.Flag{"-p": {Text: "port {v}", Value: true}}, Args: []string{"target host"}},
		"ls":       {Cmd: "ls", Summary: "lists files", Letters: map[string]string{"l": "long", "a": "all"}, Args: []string{"path"}},
		"tasklist": {Cmd: "tasklist", Summary: "lists processes", Flags: map[string]store.Flag{"/fi": {Text: "filter {v}", Value: true}}},
		"apt":      {Cmd: "apt", Summary: "packages", Args: []string{"action", "package"}},
		"curl":     {Cmd: "curl", Summary: "transfers", Flags: map[string]store.Flag{"--max-time": {Text: "give up after {v} s", Value: true}}},
	}
	e, ok := m[prog]
	return e, ok
}

func kinds(r Result) string {
	var k []string
	for _, s := range r.Segments {
		k = append(k, s.Kind)
	}
	return strings.Join(k, " ")
}

func TestLexOperatorsWithoutSpaces(t *testing.T) {
	var got []string
	for _, tk := range Lex(`ls -la|grep "my file">out.txt 2>&1`) {
		got = append(got, tk.Text)
	}
	want := `ls|-la|||grep|"my file"|>|out.txt|2>&1`
	if strings.Join(got, "|") != want {
		t.Fatalf("got %q", strings.Join(got, "|"))
	}
}

func TestExplainPipeline(t *testing.T) {
	r := Explain("netstat -ano | findstr :8080", kb, MessagesEN)
	if kinds(r) != "cmd flag op cmd arg" {
		t.Fatalf("kinds: %s", kinds(r))
	}
	if r.Segments[1].Text != "a: all · n: numeric · o: pid" || !r.Segments[1].Known {
		t.Fatalf("letters: %+v", r.Segments[1])
	}
	if r.Segments[4].Text != "text to find — port 8080" {
		t.Fatalf("arg: %q", r.Segments[4].Text)
	}
	if strings.Join(r.Programs, ",") != "netstat,findstr" || len(r.Missing) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestFlagWithValueAndUserAt(t *testing.T) {
	r := Explain("ssh admin@10.0.0.12 -p 2222", kb, MessagesEN)
	if kinds(r) != "cmd arg flag" {
		t.Fatalf("kinds: %s", kinds(r))
	}
	if r.Segments[1].Text != "user admin on host 10.0.0.12" {
		t.Fatalf("%q", r.Segments[1].Text)
	}
	f := r.Segments[2]
	if f.Title != "-p 2222" || f.Text != "port 2222" || f.End != len([]rune("ssh admin@10.0.0.12 -p 2222")) {
		t.Fatalf("%+v", f)
	}
	r = Explain("curl --max-time=5 x", kb, MessagesEN)
	if r.Segments[1].Text != "give up after 5 s" {
		t.Fatalf("--opt=value: %+v", r.Segments[1])
	}
}

func TestSlashOptionsVersusPaths(t *testing.T) {
	r := Explain("findstr /i error C:\\logs\\a.txt", kb, MessagesEN)
	if kinds(r) != "cmd flag arg arg" || r.Segments[1].Text != "ignore case" {
		t.Fatalf("%s %+v", kinds(r), r.Segments)
	}
	r = Explain("ls -la /tmp /etc/hosts", kb, MessagesEN)
	if kinds(r) != "cmd flag arg arg" {
		t.Fatalf("paths read as options: %s", kinds(r))
	}
	r = Explain(`tasklist /FI "PID eq 4312"`, kb, MessagesEN)
	if r.Segments[1].Text != `filter "PID eq 4312"` {
		t.Fatalf("%+v", r.Segments[1])
	}
}

func TestSudoAndUnknown(t *testing.T) {
	r := Explain("sudo -u root apt install nginx && foo --bar", kb, MessagesVI)
	if kinds(r) != "cmd flag cmd arg arg op unknown flag" {
		t.Fatalf("kinds: %s", kinds(r))
	}
	if r.Segments[4].Text != "package" || strings.Join(r.Missing, ",") != "sudo,foo" {
		t.Fatalf("%+v %v", r.Segments[4], r.Missing)
	}
}

func TestProgramName(t *testing.T) {
	for in, want := range map[string]string{`C:\Windows\System32\PING.EXE`: "ping", "/usr/bin/ls": "ls", `"netstat.exe"`: "netstat", "Get-NetTCPConnection": "get-nettcpconnection"} {
		if got := Program(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestRuneOffsets(t *testing.T) {
	r := Explain(`echo "xin chào" | findstr chào`, kb, MessagesEN)
	last := r.Segments[len(r.Segments)-1]
	if string([]rune(r.Line)[last.Start:last.End]) != "chào" {
		t.Fatalf("offsets are not in runes: %+v", last)
	}
}

func TestParseManOptions(t *testing.T) {
	man := "NAME\n       ls - list directory contents\n\nOPTIONS\n       -a, --all\n              do not ignore entries starting with .\n\n       -w, --width=COLS\n              set output width to COLS\n       -l     use a long listing format\n"
	e := parseOptionList(man)
	if e.Summary != "list directory contents" {
		t.Fatalf("summary %q", e.Summary)
	}
	if e.Flags["--all"].Text != "do not ignore entries starting with ." || e.Flags["-a"].Value {
		t.Fatalf("all: %+v", e.Flags)
	}
	if !e.Flags["--width"].Value || !e.Flags["-w"].Value {
		t.Fatalf("width: %+v", e.Flags)
	}
	if e.Flags["-l"].Text != "use a long listing format" {
		t.Fatalf("-l: %+v", e.Flags["-l"])
	}
}

func TestParseWindowsSlashHelp(t *testing.T) {
	help := "\r\nDisplays protocol statistics and current TCP/IP network connections.\r\n\r\nNETSTAT [-a] [-n]\r\n\r\n  -a            Displays all connections and listening ports.\r\n  -n            Displays addresses and port numbers in numerical form.\r\n  -p proto      Shows connections for the protocol specified by proto.\r\n"
	e := parseOptionList(help)
	if !strings.HasPrefix(e.Summary, "Displays protocol statistics") || e.Flags["-a"].Text != "Displays all connections and listening ports." || !e.Flags["-p"].Value {
		t.Fatalf("%+v", e)
	}
}

func TestParsePowerShellHelp(t *testing.T) {
	help := "\nNAME\n    Get-NetTCPConnection\n\nSYNOPSIS\n    Gets TCP connections.\n\n\nSYNTAX\n    Get-NetTCPConnection [...]\n\nPARAMETERS\n    -LocalPort <UInt16[]>\n        Specifies an array of local ports.\n\n    -AsJob [<SwitchParameter>]\n        Runs the cmdlet as a background job.\n"
	e := parsePowerShellHelp(help)
	if e.Summary != "Gets TCP connections." || !e.Flags["-localport"].Value || e.Flags["-localport"].Text != "Specifies an array of local ports. ({v})" {
		t.Fatalf("%+v", e)
	}
}
