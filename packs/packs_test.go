package packs

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/thangkes/cmdex/internal/explain"
	"github.com/thangkes/cmdex/internal/search"
	"github.com/thangkes/cmdex/internal/store"
)

// The starter packs must parse, import cleanly in both languages, have no
// duplicate templates, describe every parameter, and make the examples in
// the README work.
func TestStarterPacks(t *testing.T) {
	all := All()
	if len(all) != 3 {
		t.Fatalf("want 3 packs, got %d", len(all))
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
			r, err := s.ImportPack(p, lang)
			if err != nil {
				t.Fatalf("%s: %v", p.Name, err)
			}
			if r.Skipped != 0 {
				t.Errorf("%s: %d duplicate templates", p.Name, r.Skipped)
			}
			total += r.Added
		}
		cmds, _ := s.Commands()
		if len(cmds) != total || total < 90 {
			t.Fatalf("%s: %d commands imported, %d in library", lang, total, len(cmds))
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

		lookup := func(p string) (store.Explain, bool) { return s.Explain(p) }
		for line, want := range map[string]string{
			"netstat -ano | findstr :8080":   "cmd flag op cmd arg",
			"sudo ss -tulpn | grep :22":      "cmd cmd flag op cmd arg",
			"curl -sSIL https://x.example":   "cmd flag arg",
			"tar -czvf b.tar.gz /etc":        "cmd flag arg arg",
			"robocopy C:\\a D:\\b /MIR /R:2": "cmd arg arg flag flag",
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
