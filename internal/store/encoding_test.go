package store

import (
	"bytes"
	"errors"
	"testing"
	"unicode/utf16"
)

func TestDecodeText(t *testing.T) {
	want := `{"name": "Xem cổng đang mở"}`
	le := []byte{0xFF, 0xFE}
	be := []byte{0xFE, 0xFF}
	for _, u := range utf16.Encode([]rune(want)) {
		le = append(le, byte(u), byte(u>>8))
		be = append(be, byte(u>>8), byte(u))
	}
	for name, in := range map[string][]byte{
		"utf-8":     []byte(want),
		"utf-8 BOM": WithBOM([]byte(want)),
		"utf-16 LE": le,
		"utf-16 BE": be,
	} {
		got, err := DecodeText(in)
		if err != nil || string(got) != want {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	// "cổng" in Windows-1258: not UTF-8, refused rather than garbled
	if _, err := DecodeText([]byte{'c', 0xF4, 0xD2, 'n', 'g'}); !errors.Is(err, ErrNotUTF8) {
		t.Fatalf("legacy encoding accepted: %v", err)
	}
	if b := WithBOM(WithBOM([]byte("x"))); !bytes.Equal(b, append(append([]byte{}, BOM...), 'x')) {
		t.Fatalf("BOM twice: %q", b)
	}
	// parsers take files with a BOM too
	if _, err := ParsePack(WithBOM([]byte(packJSON))); err != nil {
		t.Fatal(err)
	}
	bk := WithBOM([]byte(`{"format": "cmdex-backup", "version": 2}`))
	if !IsBackup(bk) {
		t.Fatal("backup with a BOM not recognized")
	}
	if _, err := ParseBackup(bk); err != nil {
		t.Fatal(err)
	}
}
