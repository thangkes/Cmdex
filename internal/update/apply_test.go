package update

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestAssetFor(t *testing.T) {
	win, lin := AssetFor("windows", "amd64"), AssetFor("linux", "amd64")
	cases := []struct {
		name     string
		win, lin bool
	}{
		{"Cmdex-Windows-Setup-0.1.0.exe", true, false},
		{"Cmdex-Linux-amd64-0.1.0.tar.gz", false, true},
		{"Cmdex-Linux-arm64-0.1.0.tar.gz", false, false},
		{"SHA256SUMS.txt", false, false},
	}
	for _, c := range cases {
		if win(c.name) != c.win || lin(c.name) != c.lin {
			t.Errorf("%s", c.name)
		}
	}
}

func TestReplaceBinaryAt(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cmdex")
	os.WriteFile(exe, []byte("old"), 0o755)
	archive := filepath.Join(dir, "a.tar.gz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"README": "x", "cmdex": "new program"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()
	if err := ReplaceBinaryAt(archive, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new program" {
		t.Fatalf("%q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}
