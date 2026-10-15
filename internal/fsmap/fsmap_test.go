package fsmap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(n *Node, name string) *Node {
	for _, k := range n.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

// Four layers with the centre: deeper folders are listed but not read.
func TestScanDepth(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.txt"), 10)
	write(t, filepath.Join(root, "l1", "b.py"), 20)
	write(t, filepath.Join(root, "l1", "l2", "c.jar"), 30)
	write(t, filepath.Join(root, "l1", "l2", "l3", "d.md"), 40)
	write(t, filepath.Join(root, "l1", "l2", "l3", "l4", "e.csv"), 50)
	m, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Root != filepath.Clean(root) || m.Parent != filepath.Dir(filepath.Clean(root)) {
		t.Errorf("root %q parent %q", m.Root, m.Parent)
	}
	l1 := find(m.Tree, "l1")
	l2 := find(l1, "l2")
	l3 := find(l2, "l3")
	if l1 == nil || l2 == nil || l3 == nil {
		t.Fatal("missing layers")
	}
	// centre, l1, l2, l3: four layers; l3 is shown but not read
	if !l2.Read || l3.Read || l3.Kids != nil {
		t.Errorf("l2 read=%v, l3 read=%v kids=%v", l2.Read, l3.Read, l3.Kids)
	}
	if m.Tree.Size != 60 || !m.Tree.Partial || !l3.Partial || l1.Size != 50 {
		t.Errorf("sizes: root %d partial %v, l1 %d", m.Tree.Size, m.Tree.Partial, l1.Size)
	}
	if m.Tree.Files != 1 || m.Tree.Dirs != 1 {
		t.Errorf("root counts %d files %d dirs", m.Tree.Files, m.Tree.Dirs)
	}
	// opening l3 as the centre reads what was closed
	m3, err := Scan(context.Background(), filepath.Join(root, "l1", "l2", "l3"))
	if err != nil || find(m3.Tree, "d.md") == nil || find(find(m3.Tree, "l4"), "e.csv") == nil {
		t.Fatalf("l3 as centre: %v", err)
	}
	if _, err := Scan(context.Background(), "relative"); err == nil {
		t.Error("a relative path was scanned")
	}
}

// Large folders show the first perDir entries (folders first, then the
// largest files) and sum the rest.
func TestScanLimit(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "zz-dir"), 0o755)
	for i := 0; i < perDir+15; i++ {
		write(t, filepath.Join(root, fmt.Sprintf("f%03d.txt", i)), i+1)
	}
	m, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tree.Kids) != perDir || m.Tree.More != 16 || !m.Tree.Kids[0].Dir {
		t.Fatalf("%d shown, %d more, first %q", len(m.Tree.Kids), m.Tree.More, m.Tree.Kids[0].Name)
	}
	if m.Tree.Kids[1].Size < m.Tree.Kids[2].Size {
		t.Error("files are not largest first")
	}
	var sum int64
	for i := 0; i < perDir+15; i++ {
		sum += int64(i + 1)
	}
	if m.Tree.Size != sum {
		t.Errorf("size %d, want %d (hidden entries included)", m.Tree.Size, sum)
	}
}

// Links are listed, never followed: a loop cannot trap the scan.
func TestScanLinksNotFollowed(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sub", "x.txt"), 1)
	if err := os.Symlink(root, filepath.Join(root, "sub", "loop")); err != nil {
		t.Skip("symlinks not allowed here:", err)
	}
	m, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	loop := find(find(m.Tree, "sub"), "loop")
	if loop == nil || !loop.Link || loop.Dir || loop.Kids != nil {
		t.Fatalf("loop: %+v", loop)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"notes.txt", "Thư mục mới", "a b", ".gitignore", "x.tar.gz"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", ".", "..", "a/b", `a\b`, "a:b", "a*", "a?", `a"`, "a<b", "a|b", "trail.", "trail ", " lead", "CON", "nul.txt", "COM1", "lpt9.log", "a\x00b", "a\u202eb", string(make([]byte, 256))} {
		if ValidName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestInside(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	cases := map[string]bool{
		root:                           true,
		filepath.Join(root, "a", "b"):  true,
		root + "-other":                false,
		filepath.Dir(root):             false,
		filepath.Join(root, "..", "x"): false, // clean first: target() does
		filepath.Clean(filepath.Join(root, "a", "..", "b")): true,
	}
	for p, want := range cases {
		if got := Inside(root, filepath.Clean(p)); got != want {
			t.Errorf("Inside(%q) = %v", p, got)
		}
	}
	if runtime.GOOS == "windows" && !Inside(`C:\Data\Root`, `c:\data\root\x`) {
		t.Error("case should not matter on Windows")
	}
}

// Changes stay inside the scanned folder, never touch the folder itself,
// and refuse taken names.
func TestRenameMkdirLimits(t *testing.T) {
	base := t.TempDir()
	if runtime.GOOS != "windows" {
		// on Linux only the home folder, /tmp and drives may change
		home := filepath.Join(base, "home")
		os.MkdirAll(home, 0o755)
		t.Setenv("HOME", home)
		base = home
	}
	root := filepath.Join(base, "proj")
	write(t, filepath.Join(root, "a.txt"), 1)
	write(t, filepath.Join(root, "b.txt"), 1)
	write(t, filepath.Join(base, "outside.txt"), 1)

	to, err := Rename(root, filepath.Join(root, "a.txt"), "c.txt")
	if err != nil || filepath.Base(to) != "c.txt" {
		t.Fatalf("rename: %v %q", err, to)
	}
	if _, err := Rename(root, filepath.Join(root, "c.txt"), "b.txt"); !errors.Is(err, ErrExists) {
		t.Errorf("rename onto b.txt: %v", err)
	}
	if _, err := Rename(root, filepath.Join(base, "outside.txt"), "x.txt"); !errors.Is(err, ErrOutside) {
		t.Errorf("rename outside: %v", err)
	}
	if _, err := Rename(root, filepath.Join(root, "..", "outside.txt"), "x.txt"); !errors.Is(err, ErrOutside) {
		t.Errorf("rename via ..: %v", err)
	}
	if _, err := Rename(root, root, "x"); !errors.Is(err, ErrProtected) {
		t.Errorf("rename the root: %v", err)
	}
	if _, err := Rename(root, filepath.Join(root, "b.txt"), "../b.txt"); err == nil {
		t.Error("a name with a separator was accepted")
	}
	if _, err := Rename("", filepath.Join(root, "b.txt"), "x.txt"); !errors.Is(err, ErrOutside) {
		t.Errorf("rename before any scan: %v", err)
	}
	d, err := Mkdir(root, root, "Thư mục mới")
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
		t.Fatal("folder not made")
	}
	if _, err := Mkdir(root, root, "Thư mục mới"); !errors.Is(err, ErrExists) {
		t.Errorf("mkdir twice: %v", err)
	}
	if _, err := Mkdir(root, base, "x"); !errors.Is(err, ErrOutside) {
		t.Errorf("mkdir outside: %v", err)
	}
	if err := Trash(root, root); !errors.Is(err, ErrProtected) {
		t.Errorf("trash the root: %v", err)
	}
	if err := Trash(root, filepath.Join(base, "outside.txt")); !errors.Is(err, ErrOutside) {
		t.Errorf("trash outside: %v", err)
	}
	if err := Open(root, filepath.Join(root, "tool.bat"), false); err == nil {
		t.Error("opened a missing file")
	}
	write(t, filepath.Join(root, "tool.bat"), 1)
	if err := Open(root, filepath.Join(root, "tool.bat"), false); !errors.Is(err, ErrRunnable) {
		t.Errorf("open a runnable file without confirming: %v", err)
	}
}

func TestProtected(t *testing.T) {
	if runtime.GOOS == "windows" {
		sys := os.Getenv("SystemRoot")
		for _, p := range []string{`C:\`, sys, filepath.Join(sys, "System32", "drivers"), filepath.Join(os.Getenv("ProgramFiles"), "App"), `C:\$Recycle.Bin\x`, `C:\System Volume Information`} {
			if !protected(filepath.Clean(p)) {
				t.Errorf("%q not protected", p)
			}
		}
		if protected(filepath.Join(os.Getenv("USERPROFILE"), "Desktop", "x")) {
			t.Error("the desktop is protected")
		}
		return
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/", "/etc/passwd", "/usr/bin", home} {
		if !protected(p) {
			t.Errorf("%q not protected", p)
		}
	}
	for _, p := range []string{filepath.Join(home, "x"), "/tmp/x", "/media/usb/x"} {
		if protected(p) {
			t.Errorf("%q protected", p)
		}
	}
}

// Linux: the freedesktop trash gets the file and an info file with where it
// came from.
func TestTrashFreedesktop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses the Recycle Bin (TestTrashRecycleBin)")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	root := filepath.Join(home, "proj")
	write(t, filepath.Join(root, "old file.txt"), 5)
	write(t, filepath.Join(root, "dir", "x.txt"), 5)
	for _, name := range []string{"old file.txt", "dir"} {
		if err := Trash(root, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			t.Fatalf("%s still there", name)
		}
	}
	trash := filepath.Join(home, ".local", "share", "Trash")
	if _, err := os.Stat(filepath.Join(trash, "files", "dir", "x.txt")); err != nil {
		t.Error("dir not in the trash")
	}
	info, err := os.ReadFile(filepath.Join(trash, "info", "old file.txt.trashinfo"))
	if err != nil || !contains(string(info), "Path="+filepath.ToSlash(root)+"/old%20file.txt") || !contains(string(info), "DeletionDate=") {
		t.Errorf("info: %v %q", err, info)
	}
	// the same name again gets a new one
	write(t, filepath.Join(root, "old file.txt"), 5)
	if err := Trash(root, filepath.Join(root, "old file.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trash, "files", "old file.txt.2")); err != nil {
		t.Error("second copy not renamed")
	}
}

// Windows: really moves a file to the Recycle Bin, so it runs only when
// asked (CMDEX_TEST_RECYCLE=1); it leaves a small test file there.
func TestTrashRecycleBin(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("CMDEX_TEST_RECYCLE") != "1" {
		t.Skip("set CMDEX_TEST_RECYCLE=1 on Windows")
	}
	root := t.TempDir()
	p := filepath.Join(root, "cmdex-recycle-test.txt")
	write(t, p, 3)
	if err := Trash(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); err == nil {
		t.Fatal("still there")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
