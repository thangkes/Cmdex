// Command launcher is cmdex.com, installed next to Cmdex.exe on Windows.
//
// Cmdex.exe is a GUI program: a terminal does not wait for it, so its
// prompt comes back at once and fights `cmdex --export … --password` for
// the keyboard. Typing `cmdex` finds cmdex.com first (PATHEXT lists .COM
// before .EXE). This console program starts Cmdex.exe from its own folder
// (never through PATH), shares its console and waits, so command-line use
// behaves like any terminal program. With no options it only opens the
// window and returns.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		fail(err)
	}
	if p, err := filepath.EvalSymlinks(self); err == nil {
		self = p
	}
	gui := filepath.Join(filepath.Dir(self), "Cmdex.exe")
	if _, err := os.Stat(gui); err != nil {
		fail(fmt.Errorf("Cmdex.exe is missing next to %s", self))
	}
	args := os.Args[1:]
	cmd := exec.Command(gui, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if !commandLine(args) {
		if err := cmd.Start(); err != nil {
			fail(err)
		}
		return // the window opens; the terminal is free again
	}
	signal.Ignore(os.Interrupt) // Ctrl+C reaches Cmdex.exe, which decides
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		fail(err)
	}
}

// commandLine reports whether Cmdex is asked to work in the terminal
// (--version, --export…) rather than to open its window.
func commandLine(args []string) bool {
	return len(args) > 0 && (strings.HasPrefix(args[0], "--") || args[0] == "-v")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "cmdex:", err)
	os.Exit(1)
}
