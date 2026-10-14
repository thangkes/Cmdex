//go:build windows

package explain

import (
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

// fromConsole turns the output of a Windows console tool into UTF-8. Tools
// writing to a pipe use the console (OEM) code page, so on a localized
// Windows their help would show as broken letters. Output that is already
// UTF-8 (or plain ASCII) is left as it is.
func fromConsole(b []byte) []byte {
	if len(b) == 0 || utf8.Valid(b) {
		return b
	}
	const cpOEM = 1 // CP_OEMCP
	n, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return []byte(string([]rune(string(b)))) // invalid bytes become U+FFFD
	}
	w := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), &w[0], n); err != nil {
		return []byte(string([]rune(string(b))))
	}
	return []byte(windows.UTF16ToString(w))
}
