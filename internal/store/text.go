package store

import (
	"errors"
	"fmt"
)

// ErrHiddenChars is returned for a command line that holds characters which
// make it run differently from how it looks: line breaks and other control
// characters (a second command after a hidden newline), and the invisible
// or direction-changing Unicode marks used to disguise code ("Trojan
// Source"). A command from an untrusted pack or backup could otherwise show
// one thing and run another.
var ErrHiddenChars = errors.New("the command contains hidden or control characters")

// CheckCommandText reports whether s is safe to show and run as one line.
func CheckCommandText(s string) error {
	for i, r := range s {
		switch {
		case r == '\t':
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0: // C0, DEL, C1 controls (incl. \n, \r, ESC)
			return fmt.Errorf("%w (U+%04X at %d)", ErrHiddenChars, r, i)
		case r >= 0x200b && r <= 0x200f, // zero-width space/joiners, LRM, RLM
			r >= 0x202a && r <= 0x202e,            // bidi embeddings and overrides
			r >= 0x2066 && r <= 0x2069,            // bidi isolates
			r == 0x061c, r == 0x2060, r == 0xfeff, // Arabic letter mark, word joiner, BOM
			r >= 0xfff9 && r <= 0xfffb: // interlinear annotations
			return fmt.Errorf("%w (U+%04X at %d)", ErrHiddenChars, r, i)
		}
	}
	return nil
}
