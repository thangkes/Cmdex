//go:build windows

package app

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// Windows starts the programs listed under the user's Run key at sign-in.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartOn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Cmdex")
	return err == nil && v != ""
}

func autostartAdd(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("Cmdex", `"`+exe+`"`) // quoted: the path may hold spaces
}

func autostartRemove() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue("Cmdex"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
