package packs

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/thangkes/cmdex/internal/explain"
	"github.com/thangkes/cmdex/internal/search"
	"github.com/thangkes/cmdex/internal/store"
)

// The starter packs must parse, build the defaults cleanly in both
// languages, have no duplicate templates, describe every parameter, and make the examples in
// the README work.
func TestStarterPacks(t *testing.T) {
	all := All()
	if len(all) != 5 {
		t.Fatalf("want 5 packs, got %d", len(all))
	}
	for _, lang := range []string{"en", "vi"} {
		s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for _, p := range all {
			for _, c := range p.Commands {
				if c.Name.In(lang) == "" || c.Purpose.In(lang) == "" || c.Category.In(lang) == "" {
					t.Errorf("%s/%s: %q lacks a %s name, purpose or category", p.Name, lang, c.Template, lang)
				}
				if _, ok := c.Name["vi"]; !ok {
					t.Errorf("%s: %q has no Vietnamese name", p.Name, c.Template)
				}
				keys := store.PlaceholderKeys(c.Template)
				have := map[string]bool{}
				for _, pp := range c.Params {
					have[pp.Key] = true
					if pp.Description.In(lang) == "" {
						t.Errorf("%s: %q param %s has no description", p.Name, c.Template, pp.Key)
					}
				}
				for _, k := range keys {
					if !have[k] {
						t.Errorf("%s: %q uses {%s} without describing it", p.Name, c.Template, k)
					}
				}
			}
			total += len(p.Commands)
		}
		// the packs are read as the defaults database, as in the app
		if err := s.UseDefaults(all, lang); err != nil {
			t.Fatal(err)
		}
		cmds, _ := s.Commands()
		if len(cmds) != total || total < 90 { // fewer: duplicate templates, or refused ones
			t.Fatalf("%s: %d commands in the packs, %d in the library", lang, total, len(cmds))
		}
		for _, c := range cmds {
			if !c.Base || c.ID >= 0 {
				t.Fatalf("not a default: %+v", c)
			}
		}

		syn, _ := s.Synonyms()
		norm := map[string][]string{}
		for k, v := range syn {
			norm[search.Normalize(k)] = v
		}
		find := func(q, os, wantTpl string) {
			t.Helper()
			r := search.Rank(cmds, q, norm, os, 3)
			for _, x := range r {
				if strings.Contains(x.Command.Template, wantTpl) {
					return
				}
			}
			var got []string
			for _, x := range r {
				got = append(got, x.Command.Template)
			}
			t.Errorf("%s: %q (%s) top 3 lacks %q: %v", lang, q, os, wantTpl, got)
		}
		find("xem cổng đang mở", "windows", "netstat -ano")
		find("xem cong dang mo", "linux", "ss -tulpn")
		find("show open ports", "linux", "ss -tulpn")
		find("ổ đĩa đầy", "linux", "df -hT")
		find("thư mục nào chiếm dung lượng", "linux", "du -xh")
		find("khởi động lại dịch vụ", "linux", "systemctl restart")
		find("restart service", "windows", "Restart-Service")
		find("tìm tiến trình đang giữ cổng", "windows", "netstat -ano")
		find("dns", "windows", "nslookup")
		find("mở khóa tài khoản domain", "windows", "Unlock-ADAccount")
		find("máy in bị kẹt", "windows", "Spooler")
		find("đồng bộ giờ", "windows", "w32tm")
		find("map ổ mạng", "windows", "net use {drive}")
		find("chứng chỉ website hết hạn", "linux", "openssl s_client")
		find("sức khỏe ổ đĩa", "linux", "smartctl")
		find("mở cổng firewalld", "linux", "firewall-cmd --permanent")
		find("dịch vụ bị lỗi", "linux", "systemctl --failed")

		lookup := func(p string) (store.Explain, bool) { return s.Explain(p) }
		for line, want := range map[string]string{
			"netstat -ano | findstr :8080":   "cmd flag op cmd arg",
			"sudo ss -tulpn | grep :22":      "cmd cmd flag op cmd arg",
			"curl -sSIL https://x.example":   "cmd flag arg",
			"tar -czvf b.tar.gz /etc":        "cmd flag arg arg",
			"robocopy C:\\a D:\\b /MIR /R:2": "cmd arg arg flag flag",
			"gpresult /r":                    "cmd flag",
			"w32tm /query /status":           "cmd flag flag",
			"sudo firewall-cmd --list-all":   "cmd cmd flag",
			"sudo lsof -i :8080":             "cmd cmd flag",
			"sudo smartctl -H /dev/sda":      "cmd cmd flag arg",
		} {
			r := explain.Explain(line, lookup, explain.MessagesEN)
			var k []string
			for _, sg := range r.Segments {
				k = append(k, sg.Kind)
				if (sg.Kind == "flag" || sg.Kind == "cmd") && !sg.Known {
					t.Errorf("%s: %q not explained", line, sg.Title)
				}
			}
			if strings.Join(k, " ") != want {
				t.Errorf("%s: %s, want %s", line, strings.Join(k, " "), want)
			}
		}
		// case matters on Linux: -I is not -i
		r := explain.Explain("curl -I x", lookup, explain.MessagesEN)
		if !strings.Contains(r.Segments[1].Text, "header") || strings.HasPrefix(r.Segments[1].Text, "I:") {
			t.Errorf("curl -I: %q", r.Segments[1].Text)
		}
		s.Close()
	}
}

// Cisco and pfSense commands: found first on their device, explained with
// the device's own words (abbreviations included), never as Linux's.
func TestDevicePacks(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UseDefaults(All(), "vi"); err != nil {
		t.Fatal(err)
	}
	cmds, _ := s.Commands()
	syn, _ := s.Synonyms()
	top := func(q, os string) string {
		r := search.Rank(cmds, q, syn, os, 1)
		if len(r) == 0 {
			return ""
		}
		return r[0].Command.Template
	}
	if got := top("vlan", "cisco"); !strings.HasPrefix(got, "show vlan") && !strings.Contains(got, "vlan") {
		t.Errorf("cisco vlan: %q", got)
	}
	if got := top("bảng định tuyến", "cisco"); got != "show ip route" {
		t.Errorf("cisco route: %q", got)
	}
	if got := top("bảng định tuyến", "linux"); got != "ip route" {
		t.Errorf("linux route: %q", got)
	}
	if got := top("chặn địa chỉ", "pfsense"); !strings.HasPrefix(got, "easyrule block") {
		t.Errorf("pfsense block: %q", got)
	}
	if r := search.Rank(cmds, "", syn, "cisco", 1); r[0].Command.Platform != store.Cisco {
		t.Errorf("most used on a Cisco session: %+v", r[0].Command)
	}
	for _, r := range search.Rank(cmds, "", syn, "windows", 20) {
		if store.IsDevice(r.Command.Platform) {
			t.Errorf("device command among the most used on Windows: %+v", r.Command)
		}
	}

	cisco := func(p string) (store.Explain, bool) { return s.Explain(store.ExplainKey(store.Cisco, p)) }
	opts := explain.Options{Keywords: true, Programs: s.ExplainedOn(store.Cisco)}
	kinds := func(line string) string {
		r := explain.ExplainWith(line, cisco, explain.MessagesEN, opts)
		var k []string
		for _, sg := range r.Segments {
			if !sg.Known {
				k = append(k, "?"+sg.Title)
			} else {
				k = append(k, sg.Kind)
			}
		}
		return strings.Join(k, " ")
	}
	for line, want := range map[string]string{
		"sh ip int br":                         "cmd flag flag flag",
		"show running-config | include vlan":   "cmd flag op cmd arg",
		"conf t":                               "cmd flag",
		"no shut":                              "cmd cmd",
		"switchport access vlan 10":            "cmd flag flag",
		"switchport trunk allowed vlan 10,20":  "cmd flag flag flag",
		"ip address 192.168.1.1 255.255.255.0": "cmd flag arg arg",
		"reload in 10":                         "cmd flag",
	} {
		if got := kinds(line); got != want {
			t.Errorf("%s: %s, want %s", line, got, want)
		}
	}
	r := explain.ExplainWith("sh ip int br", cisco, explain.MessagesVI, opts)
	if r.Programs[0] != "show" || r.Segments[0].Text != "Xem trạng thái và cấu hình" {
		t.Errorf("%+v", r)
	}
	// Linux's ip is not Cisco's
	if e, _ := s.Explain("ip"); strings.Contains(e.Summary, "IP settings") {
		t.Error("Cisco explanation leaked into Linux")
	}

	pf := func(p string) (store.Explain, bool) {
		if e, ok := s.Explain(store.ExplainKey(store.PfSense, p)); ok {
			return e, true
		}
		return s.Explain(p)
	}
	r = explain.Explain("pfctl -vvsr | grep 443", pf, explain.MessagesEN)
	for _, sg := range r.Segments {
		if (sg.Kind == "cmd" || sg.Kind == "flag") && !sg.Known {
			t.Errorf("pfsense: %q not explained", sg.Title)
		}
	}
}

// Pack files are UTF-8 with a BOM, so Windows editors show Vietnamese
// right; the BOM must not reach the commands.
func TestPackFilesAreUTF8WithBOM(t *testing.T) {
	entries, _ := files.ReadDir(".")
	for _, e := range entries {
		b, _ := files.ReadFile(e.Name())
		if !bytes.HasPrefix(b, store.BOM) {
			t.Errorf("%s: no UTF-8 BOM", e.Name())
		}
		if !utf8.Valid(b) {
			t.Errorf("%s: not valid UTF-8", e.Name())
		}
	}
	for _, p := range All() {
		if strings.ContainsRune(p.Name, '\ufeff') || strings.ContainsRune(p.Commands[0].Template, '\ufeff') {
			t.Errorf("%s: BOM leaked into the pack", p.Name)
		}
	}
}

// Every starter command declares at least the risk Cmdex detects in it,
// so the pack files themselves say which commands are dangerous.
func TestPackRiskDeclared(t *testing.T) {
	for _, p := range All() {
		for _, c := range p.Commands {
			if d := store.DetectRisk(c.Template, c.Platform); c.Danger < d {
				t.Errorf("%s: %q declares %d, detected %d", p.Name, c.Template, c.Danger, d)
			}
		}
	}
}
