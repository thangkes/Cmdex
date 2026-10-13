//go:build !windows

package app

import (
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/thangkes/cmdex/internal/update"
)

// installFile replaces the program file with the one from the downloaded
// archive.
func installFile(archive string, _ bool) error {
	_, err := update.ReplaceBinary(archive)
	return err
}

// restart starts the new program file in place of this process.
func (a *App) restart() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	a.shutdown(a.ctx)
	syscall.Exec(exe, os.Args, os.Environ())
}

func consoleOut() io.Writer { return os.Stdout }

// systemLang is "vi" when the locale is Vietnamese.
func systemLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "vi") {
				return "vi"
			}
			return "en"
		}
	}
	return "en"
}
