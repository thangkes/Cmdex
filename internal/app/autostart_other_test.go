//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopQuote(t *testing.T) {
	cases := map[string]string{
		"/home/an/.local/bin/cmdex": `"/home/an/.local/bin/cmdex"`,
		"/opt/my apps/cmdex":        `"/opt/my apps/cmdex"`,
		"/opt/a$b`c\"d\\e/cmdex":    "\"/opt/a\\$b\\`c\\\"d\\\\e/cmdex\"",
		"/opt/100%/cmdex":           `"/opt/100%%/cmdex"`,
	}
	for in, want := range cases {
		if got := desktopQuote(in); got != want {
			t.Errorf("desktopQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestAutostartFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if autostartOn() {
		t.Fatal("on before adding")
	}
	if err := autostartAdd("/opt/my apps/cmdex"); err != nil {
		t.Fatal(err)
	}
	p, _ := autostartFile()
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "Exec=\"/opt/my apps/cmdex\"\n") {
		t.Fatalf("entry: %q, %v", b, err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 || filepath.Base(p) != "cmdex.desktop" {
		t.Fatalf("file %s mode %v", p, fi.Mode())
	}
	if !autostartOn() {
		t.Fatal("off after adding")
	}
	if err := autostartRemove(); err != nil || autostartOn() {
		t.Fatalf("remove: %v", err)
	}
	if err := autostartRemove(); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}
