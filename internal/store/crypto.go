package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The user's database keeps its sensitive values encrypted (AES-256-GCM):
// connections (name, host, user, key path, serial settings), the user's
// commands (name, template, purpose, notes, tags, environments,
// parameters) and the collected log (templates and the lines as run).
// Lookups by value use a keyed hash (HMAC-SHA256) of the value instead.
// The defaults database holds nothing private and is not encrypted.
//
// The 32-byte database key lives in the system keychain, or, where there is
// none, is derived from a master password (Argon2id). Copying the database
// file elsewhere does not copy the key.

// encPrefix marks an encrypted value; values without it are plain (a
// database not encrypted yet).
const encPrefix = "enc1:"

// ErrLocked is returned while the database key is not given.
var ErrLocked = errors.New("the database is locked")

// ErrWrongKey is returned for a key or password that does not open the
// database.
var ErrWrongKey = errors.New("this key or password does not open the database")

// box encrypts and hashes values with keys derived from the database key.
type box struct {
	aead cipher.AEAD
	mac  []byte
}

func newBox(key []byte) (*box, error) {
	if len(key) != 32 {
		return nil, errors.New("the database key must be 32 bytes")
	}
	derive := func(label string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("cmdex " + label))
		return m.Sum(nil)
	}
	block, err := aes.NewCipher(derive("encryption"))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &box{aead: aead, mac: derive("lookup")}, nil
}

// seal encrypts v; field binds it to its place (a value moved to another
// column does not decrypt). Empty values stay empty.
func (b *box) seal(field, v string) string {
	if b == nil || v == "" || strings.HasPrefix(v, encPrefix) {
		return v
	}
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	return encPrefix + base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(v), []byte(field)))
}

// open decrypts v (plain values are returned as they are).
func (b *box) open(field, v string) (string, error) {
	if !strings.HasPrefix(v, encPrefix) {
		return v, nil
	}
	if b == nil {
		return "", ErrLocked
	}
	raw, err := base64.RawStdEncoding.DecodeString(v[len(encPrefix):])
	n := b.aead.NonceSize()
	if err != nil || len(raw) < n {
		return "", ErrWrongKey
	}
	out, err := b.aead.Open(nil, raw[:n], raw[n:], []byte(field))
	if err != nil {
		return "", ErrWrongKey
	}
	return string(out), nil
}

// lookup is the keyed hash used to find a value without storing it in
// clear ("" when the database is not encrypted).
func (b *box) lookup(field, v string) string {
	if b == nil {
		return ""
	}
	m := hmac.New(sha256.New, b.mac)
	m.Write([]byte(field + "\x00" + v))
	return hex.EncodeToString(m.Sum(nil))
}

// NewDatabaseKey returns a random database key.
func NewDatabaseKey() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}

// PasswordKey derives the database key from a master password and salt.
func PasswordKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
}
