package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thangkes/cmdex/internal/store"
)

func TestMapSuggest(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &App{store: s}
	oldPacks := toolPacks
	defer func() { toolPacks = oldPacks }()
	toolPacks = func() []store.Pack {
		return []store.Pack{{Name: "python", Title: store.Text{"en": "Python"}, Tools: []string{"python"}, Origin: "test", Commands: []store.PackCommand{
			{Name: store.Text{"en": "Run a Python file"}, Platform: store.Both, Template: "python {file}"},
			{Name: store.Text{"en": "Install from a file"}, Platform: store.Both, Template: "pip install --requirement {requirements}", Danger: 1},
		}}}
	}
	if err := s.UseDefaults(a.activePacks(), "en"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	py := filepath.Join(root, "it's here", "train.py")
	os.MkdirAll(filepath.Dir(py), 0o755)
	os.WriteFile(py, []byte("print(1)"), 0o644)
	req := filepath.Join(root, "requirements.txt")
	os.WriteFile(req, []byte("x"), 0o644)

	if got, _ := a.MapSuggest(py); len(got) != 0 {
		t.Fatalf("suggested before any scan (outside the map): %+v", got)
	}
	if _, err := a.MapScan(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.MapSuggest(py); len(got) != 1 || !strings.Contains(got[0].Line, "train.py") {
		t.Fatalf("pack off: %+v", got) // only the checksum command of the starter packs
	}
	if err := a.SetToolPack("python", true); err != nil {
		t.Fatal(err)
	}
	got, _ := a.MapSuggest(py)
	if len(got) < 2 || got[0].Name != "Run a Python file" {
		t.Fatalf("%+v", got)
	}
	want := "python '" + strings.ReplaceAll(py, "'", "''") + "'"
	if runtime.GOOS != "windows" {
		want = "python '" + strings.ReplaceAll(py, "'", `'\''`) + "'"
	}
	if got[0].Line != want {
		t.Errorf("line %q, want %q", got[0].Line, want)
	}
	if r, _ := a.MapSuggest(req); len(r) == 0 || r[0].Risk != store.RiskWarning {
		t.Errorf("requirements: %+v", r)
	}
	if a.MapRunnable(filepath.Join(os.TempDir(), "x.exe")) {
		t.Error("runnable outside the map")
	}
	if _, err := a.MapRename(filepath.Join(filepath.Dir(root), "elsewhere"), "x"); err == nil {
		t.Error("renamed outside the map")
	}
}
