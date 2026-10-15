package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFind(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	write := func(dir, name string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(a, "nmap"+ext, 0o755)
	write(b, "nmap"+ext, 0o755)
	write(b, "python3"+ext, 0o755)
	write(b, "notes"+ext, 0o644) // not executable (counts on Windows)
	os.Mkdir(filepath.Join(b, "java"+ext), 0o755)

	got := find([]string{"nmap", "python3", "java", "ruby", "../nmap", "notes"}, []string{"", "relative", a, b}, []string{ext})
	if got["nmap"] != filepath.Join(a, "nmap"+ext) {
		t.Errorf("nmap: want the first on PATH, got %q", got["nmap"])
	}
	if got["python3"] == "" {
		t.Error("python3 not found")
	}
	for _, n := range []string{"java", "ruby", "../nmap"} {
		if _, ok := got[n]; ok {
			t.Errorf("%s should not be found", n)
		}
	}
	if _, ok := got["notes"]; ok != (runtime.GOOS == "windows") {
		t.Errorf("notes: found=%v", ok)
	}
}
