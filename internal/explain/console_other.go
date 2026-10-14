//go:build !windows

package explain

// fromConsole: man pages are read as UTF-8 already.
func fromConsole(b []byte) []byte { return b }
