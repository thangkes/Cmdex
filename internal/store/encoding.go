package store

import (
	"bytes"
	"errors"
	"unicode/utf16"
	"unicode/utf8"
)

// BOM is the UTF-8 byte order mark. Cmdex writes it at the start of the
// text files it exports, so Windows programs that guess the encoding (old
// Notepad, Excel, PowerShell 5's Get-Content) show Vietnamese correctly.
var BOM = []byte{0xEF, 0xBB, 0xBF}

// ErrNotUTF8 is returned for a text file in a legacy encoding.
var ErrNotUTF8 = errors.New("the file is not UTF-8 or UTF-16 text: save it as UTF-8 and try again")

// WithBOM returns text with a UTF-8 BOM in front.
func WithBOM(text []byte) []byte {
	return append(append([]byte{}, BOM...), bytes.TrimPrefix(text, BOM)...)
}

// DecodeText returns a text file as UTF-8 without BOM. It reads UTF-8 with
// or without BOM, and UTF-16 with BOM (Notepad's "Unicode", PowerShell 5's
// ">" redirection). A file in a legacy encoding is refused rather than
// read as broken letters.
func DecodeText(data []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(data, BOM):
		data = data[len(BOM):]
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return fromUTF16(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return fromUTF16(data[2:], true)
	}
	if !utf8.Valid(data) {
		return nil, ErrNotUTF8
	}
	return data, nil
}

func fromUTF16(b []byte, bigEndian bool) ([]byte, error) {
	if len(b)%2 != 0 {
		return nil, ErrNotUTF8
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return []byte(string(utf16.Decode(u))), nil
}
