//go:build !windows

package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Linux desktops (GNOME, KDE, Xfce…) start the entries in
// ~/.config/autostart at sign-in (the XDG autostart specification).
func autostartFile() (string, error) {
	dir, err := os.UserConfigDir() // $XDG_CONFIG_HOME or ~/.config
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "cmdex.desktop"), nil
}

func autostartOn() bool {
	p, err := autostartFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func autostartAdd(exe string) error {
	p, err := autostartFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=Cmdex\nExec=" + desktopQuote(exe) +
		"\nIcon=cmdex\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(p, []byte(entry), 0o600)
}

func autostartRemove() error {
	p, err := autostartFile()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// desktopQuote quotes a path for an Exec line: inside double quotes `"`,
// backtick, `$` and `\` are escaped, and `%` is doubled (field codes).
func desktopQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + strings.ReplaceAll(r.Replace(s), "%", "%%") + `"`
}
