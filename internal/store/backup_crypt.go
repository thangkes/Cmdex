package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// A backup may be sealed with a password the user chooses when exporting:
// the JSON backup is encrypted with AES-256-GCM under a key derived from
// the password with Argon2id. It opens on any computer, Windows or Linux,
// with that password. Without a password a backup stays plain JSON: the
// user decides.

const SealedBackupFormat = "cmdex-backup-sealed"

// MinBackupPassword is the shortest password a sealed backup takes.
const MinBackupPassword = 8

var (
	ErrBackupPassword      = errors.New("this backup is protected by a password")
	ErrWrongBackupPassword = errors.New("wrong password, or the file was changed")
	ErrShortBackupPassword = fmt.Errorf("the password needs at least %d characters", MinBackupPassword)
)

type sealedKDF struct {
	Name    string `json:"name"`
	Salt    string `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"` // KiB
	Threads uint8  `json:"threads"`
}

type sealedBackup struct {
	Format  string    `json:"format"`
	Version int       `json:"version"`
	Note    string    `json:"note"`
	KDF     sealedKDF `json:"kdf"`
	Cipher  string    `json:"cipher"`
	Nonce   string    `json:"nonce"`
	Data    string    `json:"data"`
}

// the header is bound to the data: changing the parameters breaks it
func (s sealedBackup) aad() []byte {
	return []byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%d\x00%d\x00%d\x00%s", s.Format, s.Version, s.KDF.Name, s.KDF.Salt, s.KDF.Time, s.KDF.Memory, s.KDF.Threads, s.Cipher))
}

func sealedKey(password string, k sealedKDF) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(k.Salt)
	// a file from elsewhere must not make Cmdex spend minutes or gigabytes
	if err != nil || k.Name != "argon2id" || len(salt) < 16 || k.Time < 1 || k.Time > 10 || k.Memory < 8*1024 || k.Memory > 1024*1024 || k.Threads < 1 || k.Threads > 16 {
		return nil, errors.New("this backup's protection is not one Cmdex reads")
	}
	return argon2.IDKey([]byte(password), salt, k.Time, k.Memory, k.Threads, 32), nil
}

// SealBackup encrypts a JSON backup with password.
func SealBackup(plain []byte, password string) ([]byte, error) {
	if utf8.RuneCountInString(password) < MinBackupPassword {
		return nil, ErrShortBackupPassword
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	s := sealedBackup{Format: SealedBackupFormat, Version: 1,
		Note:   "Encrypted Cmdex backup: import it in Cmdex (Library > Import) with its password.",
		KDF:    sealedKDF{Name: "argon2id", Salt: base64.StdEncoding.EncodeToString(salt), Time: 3, Memory: 64 * 1024, Threads: 4},
		Cipher: "aes-256-gcm"}
	key, err := sealedKey(password, s.KDF)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	s.Nonce = base64.StdEncoding.EncodeToString(nonce)
	s.Data = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, s.aad()))
	out, err := json.MarshalIndent(s, "", "  ")
	return append(out, '\n'), err
}

// IsSealedBackup reports whether data is a backup sealed with a password.
func IsSealedBackup(data []byte) bool {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), BOM)
	if !bytes.HasPrefix(data, []byte("{")) || !bytes.Contains(data[:min(len(data), 200)], []byte(SealedBackupFormat)) {
		return false
	}
	var s struct {
		Format string `json:"format"`
	}
	return json.Unmarshal(data, &s) == nil && s.Format == SealedBackupFormat
}

// OpenSealedBackup decrypts a sealed backup; the result is the JSON backup.
func OpenSealedBackup(data []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, ErrBackupPassword
	}
	var s sealedBackup
	if err := json.Unmarshal(bytes.TrimPrefix(bytes.TrimSpace(data), BOM), &s); err != nil || s.Format != SealedBackupFormat {
		return nil, errors.New("not a sealed Cmdex backup")
	}
	if s.Version != 1 || s.Cipher != "aes-256-gcm" {
		return nil, errors.New("this backup was sealed by a newer Cmdex")
	}
	key, err := sealedKey(password, s.KDF)
	if err != nil {
		return nil, err
	}
	nonce, err1 := base64.StdEncoding.DecodeString(s.Nonce)
	ct, err2 := base64.StdEncoding.DecodeString(s.Data)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if err1 != nil || err2 != nil || len(nonce) != gcm.NonceSize() {
		return nil, ErrWrongBackupPassword
	}
	plain, err := gcm.Open(nil, nonce, ct, s.aad())
	if err != nil {
		return nil, ErrWrongBackupPassword
	}
	return plain, nil
}
