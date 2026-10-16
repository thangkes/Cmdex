package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestSealedBackup(t *testing.T) {
	plain := []byte(`{"format":"cmdex-backup","version":2,"notes":[{"body":"witness-secret Mật khẩu","created":1}]}`)
	sealed, err := SealBackup(plain, "đúng mật khẩu 1")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("witness-secret")) || bytes.Contains(sealed, []byte("Mật khẩu")) {
		t.Fatal("the sealed file shows the backup")
	}
	if !IsSealedBackup(sealed) || IsSealedBackup(plain) || IsBackup(sealed) {
		t.Fatal("format detection")
	}
	got, err := OpenSealedBackup(sealed, "đúng mật khẩu 1")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %v %q", err, got)
	}
	if _, err := OpenSealedBackup(sealed, "wrong password"); !errors.Is(err, ErrWrongBackupPassword) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := OpenSealedBackup(sealed, ""); !errors.Is(err, ErrBackupPassword) {
		t.Errorf("no password: %v", err)
	}
	// two seals of the same backup differ (fresh salt and nonce)
	again, _ := SealBackup(plain, "đúng mật khẩu 1")
	if bytes.Equal(again, sealed) {
		t.Error("sealing twice gave the same file")
	}
	// a changed header or body is refused, not misread
	var s map[string]any
	json.Unmarshal(sealed, &s)
	for _, change := range []func(m map[string]any){
		func(m map[string]any) { m["kdf"].(map[string]any)["time"] = 4 },
		func(m map[string]any) {
			d := m["data"].(string)
			c := "A"
			if d[10] == 'A' {
				c = "B"
			}
			m["data"] = d[:10] + c + d[11:]
		},
	} {
		var c map[string]any
		json.Unmarshal(sealed, &c)
		change(c)
		b, _ := json.Marshal(c)
		if _, err := OpenSealedBackup(b, "đúng mật khẩu 1"); err == nil {
			t.Error("a changed file opened")
		}
	}
	// a file asking for absurd work is refused before any is done
	var c map[string]any
	json.Unmarshal(sealed, &c)
	c["kdf"].(map[string]any)["memory"] = 64 * 1024 * 1024
	b, _ := json.Marshal(c)
	if _, err := OpenSealedBackup(b, "đúng mật khẩu 1"); err == nil || errors.Is(err, ErrWrongBackupPassword) {
		t.Errorf("huge memory: %v", err)
	}
	if _, err := SealBackup(plain, "short"); !errors.Is(err, ErrShortBackupPassword) {
		t.Errorf("short password: %v", err)
	}
}
