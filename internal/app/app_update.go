package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/thangkes/cmdex/internal/update"
)

// updateLoop looks for a newer release a minute after start and then every
// six hours. Finding one only offers it; nothing is installed until the
// user clicks.
func (a *App) updateLoop() {
	time.Sleep(time.Minute)
	for {
		if a.store.Setting("update.check", "1") == "1" {
			if r, _ := a.CheckUpdate(); r != nil {
				a.emit("update:available", r)
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

// CheckUpdate returns a newer release, or nil.
func (a *App) CheckUpdate() (*update.Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := update.Check(ctx, http.DefaultClient, Version, update.Asset)
	if err != nil || r == nil {
		return nil, err
	}
	a.mu.Lock()
	a.release = r
	a.mu.Unlock()
	return r, nil
}

// ApplyUpdate downloads the release found by CheckUpdate (checksum-verified)
// and installs it: the Windows setup runs and restarts Cmdex; on Linux the
// program file is replaced and Cmdex restarts.
func (a *App) ApplyUpdate() error {
	a.mu.Lock()
	r, busy := a.release, a.updating
	if r != nil && !busy {
		a.updating = true
	}
	a.mu.Unlock()
	if r == nil {
		return errors.New("no update found")
	}
	if busy {
		return errors.New("already updating")
	}
	defer func() {
		a.mu.Lock()
		a.updating = false
		a.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "cmdex-update-")
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, r.AssetName)
	if err := update.Download(ctx, http.DefaultClient, r, dest); err != nil {
		os.RemoveAll(dir)
		return err
	}
	return a.install(dest)
}
