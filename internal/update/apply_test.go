package update

import "testing"

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
