//go:build !windows

package app

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"

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

// consoleIn is the terminal's keyboard, when stdin is a terminal.
func consoleIn() (int, func(), error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return 0, nil, errors.New("not a terminal")
	}
	return fd, func() {}, nil
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
