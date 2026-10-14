package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"

	"github.com/thangkes/cmdex/internal/store"
)

// The database key (see internal/store/crypto.go) is kept in the system
// keychain under "db-key-<id>". Without a keychain (some Linux setups) the
// user sets a master password instead, asked at each start.

// Lock states of the database, as the page sees them (Info.Locked).
const (
	lockOpen     = ""         // unlocked
	lockCreate   = "create"   // no keychain: a master password must be chosen
	lockPassword = "password" // the master password must be typed
	lockLost     = "lost"     // the key is not in the keychain (another computer or user, keychain reset)
)

func keyAccount(id string) string { return "db-key-" + id }

// openKey opens the database with its key, encrypting it the first time.
func (a *App) openKey() string {
	mode, id := a.store.KeyState()
	switch mode {
	case store.KeyNone:
		idb := make([]byte, 8)
		rand.Read(idb)
		id := hex.EncodeToString(idb)
		key := store.NewDatabaseKey()
		enc := base64.StdEncoding.EncodeToString(key)
		if keyring.Set(keyringService, keyAccount(id), enc) != nil {
			return lockCreate
		}
		if got, err := keyring.Get(keyringService, keyAccount(id)); err != nil || got != enc {
			keyring.Delete(keyringService, keyAccount(id))
			return lockCreate // a keychain that does not give the key back cannot be trusted with it
		}
		if err := a.store.SetUpKey(store.KeyKeychain, id, nil, key); err != nil {
			a.dbErr = err.Error()
		}
		return lockOpen
	case store.KeyKeychain:
		v, err := keyring.Get(keyringService, keyAccount(id))
		if err != nil {
			return lockLost
		}
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil || a.store.Unlock(key) != nil {
			return lockLost
		}
		return lockOpen
	case store.KeyPassword:
		return lockPassword
	}
	return lockLost
}

// openKeyCLI opens the database for the command line: from the keychain, or
// with the master password typed in the terminal.
func (a *App) openKeyCLI(out io.Writer) error {
	switch a.locked = a.openKey(); a.locked {
	case lockOpen:
		return nil
	case lockPassword:
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("the database is protected by a master password: run this in a terminal")
		}
		fmt.Fprint(out, "Cmdex master password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return err
		}
		return a.unlock(string(pw))
	case lockCreate:
		return errors.New("open Cmdex once to choose the master password of this library")
	}
	return errors.New("the key of this library is not in this computer's keychain: open Cmdex to choose what to do")
}

// UnlockDatabase sets the master password (state "create") or opens the
// database with it (state "password"), then starts Cmdex.
func (a *App) UnlockDatabase(password string) error {
	if err := a.unlock(password); err != nil {
		return err
	}
	a.ready()
	return nil
}

// unlock does UnlockDatabase's work without starting Cmdex (command line).
func (a *App) unlock(password string) error {
	a.mu.Lock()
	state := a.locked
	a.mu.Unlock()
	switch state {
	case lockCreate:
		if len([]rune(password)) < 10 {
			return errors.New("the master password needs at least 10 characters")
		}
		salt := make([]byte, 16)
		rand.Read(salt)
		if err := a.store.SetUpKey(store.KeyPassword, "", salt, store.PasswordKey(password, salt)); err != nil {
			return err
		}
	case lockPassword:
		if err := a.store.Unlock(store.PasswordKey(password, a.store.PasswordSalt())); err != nil {
			if errors.Is(err, store.ErrWrongKey) {
				return errors.New("wrong master password")
			}
			return err
		}
	default:
		return errors.New("the database is not waiting for a password")
	}
	a.mu.Lock()
	a.locked = lockOpen
	a.mu.Unlock()
	return nil
}

// StartNewLibrary sets a database that cannot be opened (key lost, master
// password forgotten) aside, renamed and kept, and starts with a new one.
func (a *App) StartNewLibrary() error {
	a.mu.Lock()
	state := a.locked
	a.mu.Unlock()
	if state != lockLost && state != lockPassword {
		return errors.New("the database is open: nothing to set aside")
	}
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	a.store.Close()
	aside := fmt.Sprintf("%s.locked-%s", path, time.Now().Format("20060102-150405"))
	for _, ext := range []string{"", "-wal", "-shm", ".bak-v1"} {
		if _, err := os.Stat(path + ext); err == nil {
			if err := os.Rename(path+ext, aside+ext); err != nil {
				return err
			}
		}
	}
	if a.store, err = store.Open(path); err != nil {
		return err
	}
	a.mu.Lock()
	a.locked = a.openKey()
	locked := a.locked
	a.mu.Unlock()
	if locked == lockOpen {
		a.ready()
	}
	return nil
}
