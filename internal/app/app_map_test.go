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

// The map opens again on the folder last shown, kept encrypted; several
// files go to the trash at once, a folder's contents with it.
func TestMapLastFolderAndTrashMany(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "c.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetUpKey(store.KeyKeychain, "test", nil, store.NewDatabaseKey()); err != nil {
		t.Fatal(err)
	}
	a := &App{store: s}
	if a.MapLastFolder() != "" {
		t.Fatal("a folder before any scan")
	}
	base := t.TempDir()
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", base)
	}
	root := filepath.Join(base, "witness-work-folder")
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	for _, f := range []string{"a.txt", "b.txt", filepath.Join("sub", "c.txt")} {
		os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644)
	}
	if _, err := a.MapScan(root); err != nil {
		t.Fatal(err)
	}
	if got := a.MapLastFolder(); got != filepath.Clean(root) {
		t.Errorf("last folder %q", got)
	}
	if raw := s.Setting("map.root", ""); strings.Contains(raw, "witness-work-folder") {
		t.Error("the folder is stored in clear")
	}
	if runtime.GOOS == "windows" && os.Getenv("CMDEX_TEST_RECYCLE") != "1" {
		return // the rest moves files to the real Recycle Bin
	}
	res := a.MapTrashMany([]string{filepath.Join(root, "sub"), filepath.Join(root, "sub", "c.txt"), filepath.Join(root, "a.txt"), root, "relative.txt"})
	errs := map[string]string{}
	for _, r := range res {
		errs[filepath.Base(r.Path)] = r.Error
	}
	if errs["sub"] != "" || errs["c.txt"] != "" || errs["a.txt"] != "" || errs[filepath.Base(root)] == "" || len(res) != 4 {
		t.Errorf("results %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err == nil {
		t.Error("a.txt still there")
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Error("b.txt was not chosen but is gone")
	}
}

// A backup sealed with a password: unreadable without it, described and
// imported with it, refused with a wrong one; a plain one still works.
func TestSealedBackupRoundTrip(t *testing.T) {
	open := func() *App {
		s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		if err := s.SetUpKey(store.KeyKeychain, "test", nil, store.NewDatabaseKey()); err != nil {
			t.Fatal(err)
		}
		return &App{store: s}
	}
	a := open()
	if _, err := a.store.AddNote("witness-note ghi chú bí mật", ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sealed, plain := filepath.Join(dir, "sealed.json"), filepath.Join(dir, "plain.json")
	if err := a.writeBackup(sealed, "mật khẩu đủ dài"); err != nil {
		t.Fatal(err)
	}
	if err := a.writeBackup(plain, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sealed)
	if strings.Contains(string(raw), "witness-note") {
		t.Fatal("the sealed backup shows a note")
	}
	if raw, _ := os.ReadFile(plain); !strings.Contains(string(raw), "witness-note") {
		t.Fatal("the plain backup should hold the note in clear")
	}
	if err := a.writeBackup(filepath.Join(dir, "x.json"), "short"); err == nil {
		t.Error("a short password was accepted")
	}

	b := open()
	pv, err := b.InspectImport(sealed, "")
	if err != nil || !pv.Locked || !pv.Sealed || pv.Kind != "backup" {
		t.Fatalf("locked preview: %v %+v", err, pv)
	}
	if _, err := b.ImportFrom(sealed, false, ""); err == nil {
		t.Error("imported a sealed backup without its password")
	}
	if _, err := b.ImportFrom(sealed, false, "wrong password!"); err == nil {
		t.Error("imported with a wrong password")
	}
	if pv, err = b.InspectImport(sealed, "mật khẩu đủ dài"); err != nil || pv.Locked || pv.Notes != 1 {
		t.Fatalf("opened preview: %v %+v", err, pv)
	}
	r, err := b.ImportFrom(sealed, false, "mật khẩu đủ dài")
	if err != nil || r.Notes != 1 {
		t.Fatalf("import: %v %+v", err, r)
	}
	if ns, _ := b.store.Notes("witness-note"); len(ns) != 1 {
		t.Error("the note did not arrive")
	}
	if pv, err := b.InspectImport(plain, ""); err != nil || pv.Locked || pv.Sealed {
		t.Errorf("plain preview: %v %+v", err, pv)
	}
}
