//go:build windows

package explain

import (
	"testing"
	"unicode/utf8"
)

// Help of a localized Windows comes in the console code page; it must
// reach the explanation as UTF-8 letters, and UTF-8 must stay as it is.
func TestFromConsole(t *testing.T) {
	if got := string(fromConsole([]byte("ipconfig /all"))); got != "ipconfig /all" {
		t.Fatalf("%q", got)
	}
	if got := string(fromConsole([]byte("Xem cấu hình"))); got != "Xem cấu hình" {
		t.Fatalf("UTF-8 changed: %q", got)
	}
	got := fromConsole([]byte{'c', 'a', 'f', 0x82, ' ', 0xA0})
	if !utf8.Valid(got) || len([]rune(string(got))) != 6 || string(got) == "caf� �" {
		t.Fatalf("OEM text not decoded: %q", got)
	}
}
