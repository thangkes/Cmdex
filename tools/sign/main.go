//go:build windows

// Command sign makes and uses the keys that sign Cmdex releases: the
// updater installs a release only when its SHA256SUMS.txt is signed by one
// of internal/update.TrustedKeys. Run it on the maintainer's computer only.
//
//	go run ./tools/sign gen                 release key: kept in %APPDATA%\Cmdex-release,
//	                                        protected by Windows (DPAPI) for this user only
//	go run ./tools/sign gen-backup FILE     backup key, protected by a passphrase you type;
//	                                        keep FILE offline (USB key, password manager)
//	go run ./tools/sign sign SUMS           writes SUMS.sig with the release key
//	go run ./tools/sign sign -backup FILE SUMS   …with the backup key (asks its passphrase)
//	go run ./tools/sign verify SUMS         checks SUMS.sig against the trusted keys
//	go run ./tools/sign pubkey              prints the release key's public key
//
// gen and gen-backup print the public key to put in internal/update/keys.go.
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/sys/windows"
	"golang.org/x/term"

	"github.com/thangkes/cmdex/internal/update"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sign gen | gen-backup FILE | sign [-backup FILE] SUMS | verify SUMS")
	}
	switch args[0] {
	case "gen":
		return gen()
	case "pubkey":
		key, err := loadRelease()
		if err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
		return nil
	case "gen-backup":
		if len(args) != 2 {
			return errors.New("usage: sign gen-backup FILE")
		}
		return genBackup(args[1])
	case "sign":
		if len(args) == 4 && args[1] == "-backup" {
			key, err := loadBackup(args[2])
			if err != nil {
				return err
			}
			return signFile(key, args[3])
		}
		if len(args) != 2 {
			return errors.New("usage: sign sign [-backup FILE] SUMS")
		}
		key, err := loadRelease()
		if err != nil {
			return err
		}
		return signFile(key, args[1])
	case "verify":
		if len(args) != 2 {
			return errors.New("usage: sign verify SUMS")
		}
		sums, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(args[1] + ".sig")
		if err != nil {
			return err
		}
		if !update.Verify(sums, sig) {
			return errors.New("signature does NOT match a trusted key")
		}
		fmt.Println("signature OK")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// releaseKeyPath is outside the repository, in the user's profile.
func releaseKeyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Cmdex-release", "release-signing.key"), nil
}

func gen() error {
	path, err := releaseKeyPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists already: a new key would make older releases' updaters refuse new versions", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	blob, err := protect(priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(blob)+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println("release key saved (DPAPI, this Windows user only):", path)
	fmt.Println("public key:", base64.StdEncoding.EncodeToString(pub))
	return nil
}

func loadRelease() (ed25519.PrivateKey, error) {
	path, err := releaseKeyPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("release key: %w (run: go run ./tools/sign gen)", err)
	}
	blob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, err
	}
	key, err := unprotect(blob)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("release key cannot be opened by this Windows user: %v", err)
	}
	return ed25519.PrivateKey(key), nil
}

// The backup key file: "cmdex-backup-key-v1", scrypt salt, AES-GCM nonce
// and the sealed private key, base64 on separate lines.
const backupHeader = "cmdex-backup-key-v1"

func genBackup(file string) error {
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s exists already", file)
	}
	pass, err := askPassphrase("Passphrase for the backup key: ")
	if err != nil {
		return err
	}
	again, err := askPassphrase("Same passphrase again: ")
	if err != nil {
		return err
	}
	if pass != again {
		return errors.New("the passphrases differ")
	}
	if len(pass) < 12 {
		return errors.New("use a passphrase of at least 12 characters")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	rand.Read(salt)
	rand.Read(nonce)
	aead, err := backupCipher(pass, salt)
	if err != nil {
		return err
	}
	sealed := aead.Seal(nil, nonce, priv, []byte(backupHeader))
	enc := base64.StdEncoding.EncodeToString
	out := strings.Join([]string{backupHeader, enc(salt), enc(nonce), enc(sealed), ""}, "\n")
	if err := os.WriteFile(file, []byte(out), 0o600); err != nil {
		return err
	}
	fmt.Println("backup key saved:", file)
	fmt.Println("Move it offline (USB key, password manager) and delete it from this computer.")
	fmt.Println("public key:", enc(pub))
	return nil
}

func loadBackup(file string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	l := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(l) != 4 || strings.TrimSpace(l[0]) != backupHeader {
		return nil, errors.New("not a Cmdex backup key file")
	}
	dec := func(s string) []byte { v, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(s)); return v }
	pass, err := askPassphrase("Passphrase of the backup key: ")
	if err != nil {
		return nil, err
	}
	aead, err := backupCipher(pass, dec(l[1]))
	if err != nil {
		return nil, err
	}
	key, err := aead.Open(nil, dec(l[2]), dec(l[3]), []byte(backupHeader))
	if err != nil {
		return nil, errors.New("wrong passphrase, or the file was changed")
	}
	return ed25519.PrivateKey(key), nil
}

func backupCipher(pass string, salt []byte) (cipher.AEAD, error) {
	k, err := scrypt.Key([]byte(pass), salt, 1<<17, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func askPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("passphrase must be typed in a terminal: %w", err)
	}
	return string(b), nil
}

func signFile(key ed25519.PrivateKey, sums string) error {
	b, err := os.ReadFile(sums)
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	if err := os.WriteFile(sums+".sig", []byte(sig+"\n"), 0o644); err != nil {
		return err
	}
	if !update.Verify(b, []byte(sig)) {
		return errors.New("signed, but the key is not among internal/update.TrustedKeys: releases signed with it would be refused")
	}
	fmt.Println("signed:", sums+".sig")
	return nil
}

// protect and unprotect use Windows DPAPI for the current user.
func protect(b []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte{}, unsafe.Slice(out.Data, out.Size)...), nil
}

func unprotect(b []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte{}, unsafe.Slice(out.Data, out.Size)...), nil
}
