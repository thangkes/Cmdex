package main

import (
	"strings"
	"testing"

	"github.com/thangkes/cmdex/internal/store"
)

// A page in the tldr format (written for this test).
const samplePage = "# demo\n\n" +
	"> Example scanner.\n> Second line of the summary.\n> More information: <https://example.org>.\n\n" +
	"- Scan a host with [v]erbose output:\n\n`demo -v {{host}}`\n\n" +
	"- Scan given ports and save to a file:\n\n`demo {{[-p|--ports]}} {{80}} {{host}} > {{path/to/out.txt}}`\n\n" +
	"- Pick a level:\n\n`demo -T{{1|2|3}} {{host}}`\n\n" +
	"- Copy files:\n\n`demo {{path/to/file1 path/to/file2 ...}} {{path/to/target}}`\n\n" +
	"- Print with awk:\n\n`demo | awk '{print}'`\n\n" +
	"- Log in:\n\n`demo --login {{username}} --password {{password}}`\n"

func TestBuild(t *testing.T) {
	pg := parsePage(samplePage)
	if pg.name != "demo" || pg.summary != "Example scanner. Second line of the summary." || len(pg.examples) != 6 {
		t.Fatalf("parse: %+v", pg)
	}
	p, warnings := build([]sourcePage{{platform: store.Both, page: pg}, {platform: store.Linux, page: pg}}, options{name: "demo", title: "Demo", tools: []string{"demo"}})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "awk") {
		t.Errorf("warnings: %q", warnings)
	}
	if len(p.Commands) != 5 { // the awk one is left out, the Linux page repeats the common one
		t.Fatalf("got %d commands", len(p.Commands))
	}
	want := []string{
		"demo -v {host}",
		"demo --ports {number} {host} > {out}",
		"demo -T{choice} {host}",
		"demo {file1} {target}",
		"demo --login {username} --password {password}",
	}
	for i, c := range p.Commands {
		if c.Template != want[i] {
			t.Errorf("%d: %q, want %q", i, c.Template, want[i])
		}
		if c.Platform != store.Both {
			t.Errorf("%q: platform %s", c.Template, c.Platform)
		}
		if _, ok := c.Name["vi"]; !ok {
			t.Errorf("%q: no vi slot", c.Template)
		}
	}
	if n := p.Commands[0].Name.In("vi"); n != "Scan a host with verbose output" {
		t.Errorf("name %q (an empty vi falls back to English)", n)
	}
	types := map[string]string{}
	for _, c := range p.Commands {
		for _, pp := range c.Params {
			types[pp.Key] = pp.Type
		}
	}
	for k, ty := range map[string]string{"host": "host", "number": "number", "out": "path", "choice": "text", "file1": "path", "password": "secret"} {
		if types[k] != ty {
			t.Errorf("param %s: type %q, want %q", k, types[k], ty)
		}
	}
	if d := p.Commands[2].Params[0]; d.Default != "1" || d.Required {
		t.Errorf("choice: %+v", d)
	}
	b, err := encode(p)
	if err != nil {
		t.Fatal(err)
	}
	back, err := store.ParsePack(b)
	if err != nil || len(back.Commands) != 5 || back.Origin == "" || back.Tools[0] != "demo" {
		t.Fatalf("round trip: %v %+v", err, back)
	}
}

// Running again keeps translations and extra commands, and does not touch
// the title of other commands through the shared category.
func TestMergeKeepsTranslations(t *testing.T) {
	pg := parsePage(samplePage)
	first, _ := build([]sourcePage{{platform: store.Both, page: pg}}, options{name: "demo", title: "Demo", tools: []string{"demo"}})
	first.Title["vi"] = "Demo VI"
	first.Commands[0].Name["vi"] = "Quét máy, in chi tiết"
	first.Commands[0].Category = store.Text{"en": "Demo", "vi": "Nhóm VI"}
	first.Commands[0].Danger = 1
	first.Commands[1].Params[0].Description["vi"] = "số cổng"
	first.Commands = append(first.Commands, store.PackCommand{Name: store.Text{"en": "Extra", "vi": "Thêm"}, Platform: store.Both, Template: "demo --extra"})

	again, _ := build([]sourcePage{{platform: store.Both, page: pg}}, options{name: "demo", title: "Demo", tools: []string{"demo"}})
	warnings := merge(&again, first)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "--extra") {
		t.Errorf("warnings: %q", warnings)
	}
	c := again.Commands[0]
	if c.Name.In("vi") != "Quét máy, in chi tiết" || c.Danger != 1 || c.Category.In("vi") != "Nhóm VI" {
		t.Errorf("first command: %+v", c)
	}
	if again.Commands[2].Category.In("vi") == "Nhóm VI" {
		t.Error("a command's category changed the others")
	}
	if again.Title.In("vi") != "Demo VI" || again.Commands[1].Params[0].Description.In("vi") != "số cổng" {
		t.Error("title or parameter translation lost")
	}
	if len(again.Commands) != 6 {
		t.Errorf("got %d commands", len(again.Commands))
	}
	missing, total := untranslated(again)
	if missing == 0 || missing >= total {
		t.Errorf("untranslated %d of %d", missing, total)
	}
}
