//go:build !windows

package app

import (
	"os"
	"strings"
	"syscall"

	"github.com/thangkes/cmdex/internal/update"
)

// install replaces the program file with the one from the downloaded
// archive and restarts into it.
func (a *App) install(archive string) error {
	exe, err := update.ReplaceBinary(archive)
	if err != nil {
		return err
	}
	a.shutdown(a.ctx)
	return syscall.Exec(exe, os.Args, os.Environ())
}

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
