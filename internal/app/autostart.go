package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// AutostartState is what the settings show about starting Cmdex when the
// user signs in to Windows or Linux.
type AutostartState struct {
	Supported bool   `json:"supported"`
	On        bool   `json:"on"`
	Path      string `json:"path"` // the program that starts
}

// Autostart reports whether Cmdex starts at sign-in.
func (a *App) Autostart() AutostartState {
	exe, err := selfPath()
	if err != nil {
		return AutostartState{}
	}
	return AutostartState{Supported: true, On: autostartOn(), Path: exe}
}

// SetAutostart makes Cmdex start at sign-in, or stop doing so. It is off
// until the user turns it on, and only for this user: nothing needs
// administrator rights.
func (a *App) SetAutostart(on bool) (AutostartState, error) {
	var err error
	if !on {
		err = autostartRemove()
	} else {
		var exe string
		if exe, err = selfPath(); err == nil {
			err = autostartAdd(exe)
		}
	}
	return a.Autostart(), err // read after the change
}

// selfPath is the running Cmdex program. A development build (devserve, go
// run) is refused, so it never registers a temporary file.
func selfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(exe), ".exe"))
	if name != "cmdex" {
		return "", errors.New("starting at sign-in needs the installed Cmdex")
	}
	return exe, nil
}
