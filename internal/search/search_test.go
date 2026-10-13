package search

import (
	"testing"

	"github.com/thangkes/cmdex/internal/store"
)

func lib() []store.Command {
	return []store.Command{
		{ID: 1, Name: "Tìm tiến trình đang giữ cổng", Platform: store.Windows, Template: "netstat -ano | findstr :{port}", Purpose: "Liệt kê kết nối và PID đang dùng một cổng", Tags: []string{"mạng", "cổng", "port"}, Uses: 3},
		{ID: 2, Name: "Xem cổng đang lắng nghe", Platform: store.Linux, Template: "ss -tulpn | grep :{port}", Purpose: "Hiện các cổng TCP/UDP đang mở", Tags: []string{"mạng", "cổng", "listen"}},
		{ID: 3, Name: "Ping máy", Platform: store.Windows, Template: "ping {host} -n {count}", Purpose: "Kiểm tra máy có phản hồi", Tags: []string{"ping", "kết nối"}, Uses: 20},
		{ID: 4, Name: "Xóa thư mục", Platform: store.Linux, Template: "rm -rf {path}", Purpose: "Xóa vĩnh viễn"},
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("Xem CỔNG đang mở — Đồng"); got != "xem cong dang mo — dong" {
		t.Fatalf("got %q", got)
	}
}

func TestRankWithAndWithoutDiacritics(t *testing.T) {
	for _, q := range []string{"xem cổng đang mở", "xem cong dang mo"} {
		r := Rank(lib(), q, nil, "", 0)
		if len(r) == 0 || (r[0].Command.ID != 2 && r[0].Command.ID != 1) {
			t.Fatalf("%q: %+v", q, r)
		}
		for _, x := range r {
			if x.Command.ID == 4 {
				t.Fatalf("%q matched the delete command", q)
			}
		}
		if r[0].Percent != 100 {
			t.Fatal("best match is not 100%")
		}
	}
}

func TestPlatformBoost(t *testing.T) {
	win := Rank(lib(), "cổng", nil, store.Windows, 0)
	lin := Rank(lib(), "cổng", nil, store.Linux, 0)
	if win[0].Command.Platform != store.Windows || lin[0].Command.Platform != store.Linux {
		t.Fatalf("boost: windows first=%d, linux first=%d", win[0].Command.ID, lin[0].Command.ID)
	}
}

func TestSynonyms(t *testing.T) {
	if r := Rank(lib(), "show open ports", nil, "", 0); len(r) != 0 {
		t.Fatalf("English matched without synonyms: %+v", r)
	}
	syn := map[string][]string{"ports": {"cổng"}, "open": {"đang mở"}}
	r := Rank(lib(), "show open ports", syn, "", 0)
	if len(r) == 0 || r[0].Command.ID == 3 || r[0].Command.ID == 4 {
		t.Fatalf("synonyms: %+v", r)
	}
}

func TestEmptyQueryListsMostUsed(t *testing.T) {
	r := Rank(lib(), "  ", nil, "", 2)
	if len(r) != 2 || r[0].Command.ID != 3 {
		t.Fatalf("%+v", r)
	}
}

func TestShortWordsMatchWholeWords(t *testing.T) {
	cmds := []store.Command{{ID: 1, Name: "pip install", Template: "pip install {pkg}"}, {ID: 2, Name: "Show IP", Template: "ipconfig", Tags: []string{"ip"}}}
	r := Rank(cmds, "ip", nil, "", 0)
	if len(r) != 1 || r[0].Command.ID != 2 {
		t.Fatalf("%+v", r)
	}
}
