//go:build windows

package explain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeForHelp(t *testing.T) {
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32")
	for prog, want := range map[string]bool{
		"netstat":  true,  // console tool
		"ipconfig": true,  // console tool
		"notepad":  false, // window program: would just open
		"explorer": false, // not in System32, and a window program
		"control":  false, // window program
		"cmd":      false, // on the never-run list
		"diskpart": false, // on the never-run list
	} {
		if got := safeForHelp(prog, filepath.Join(sys, prog+".exe")); got != want {
			t.Errorf("%s: %v, want %v", prog, got, want)
		}
	}
}
