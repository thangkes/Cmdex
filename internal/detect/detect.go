// Package detect finds which programs are installed, to offer the tool
// packs that go with them. It only looks at file names in the folders of
// PATH: it never starts a program, reads one, or asks a package manager.
package detect

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// Find returns, for each of names found in a folder of PATH, where it is
// (the first one, as the shell would run it). Names are plain program
// names without a path or extension: "nmap" finds nmap.exe on Windows.
func Find(names []string) map[string]string {
	return find(names, filepath.SplitList(os.Getenv("PATH")), exts())
}

func find(names, dirs, exts []string) map[string]string {
	out := map[string]string{}
	for _, name := range names {
		if !reName.MatchString(name) {
			continue
		}
		if _, done := out[name]; done {
			continue
		}
	dirs:
		for _, dir := range dirs {
			// a relative folder depends on where Cmdex was started from
			if dir == "" || !filepath.IsAbs(dir) {
				continue
			}
			for _, ext := range exts {
				p := filepath.Join(dir, name+ext)
				if isProgram(p) {
					out[name] = p
					break dirs
				}
			}
		}
	}
	return out
}

// exts are the endings a program file may have: PATHEXT on Windows.
func exts() []string {
	if runtime.GOOS != "windows" {
		return []string{""}
	}
	var out []string
	for _, e := range strings.Split(strings.ToLower(os.Getenv("PATHEXT")), ";") {
		if strings.HasPrefix(e, ".") && len(e) <= 5 {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		out = []string{".com", ".exe", ".bat", ".cmd"}
	}
	return out
}

// isProgram: a regular file (a link to one counts), executable on Linux.
// Windows' app execution aliases (WindowsApps\python.exe, which opens the
// Store when Python is missing) are not regular files and do not count.
func isProgram(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode()&0o111 != 0
}
