package main

import "testing"

func TestCommandLine(t *testing.T) {
	cases := map[string]bool{
		"":                     false,
		"--version":            true,
		"-v":                   true,
		"--export x.json":      true,
		"--import x.json":      true,
		"--update":             true,
		"C:\\work\\notes.json": false,
	}
	for in, want := range cases {
		var args []string
		if in != "" {
			args = []string{in}
		}
		if got := commandLine(args); got != want {
			t.Errorf("commandLine(%q) = %v, want %v", in, got, want)
		}
	}
}
