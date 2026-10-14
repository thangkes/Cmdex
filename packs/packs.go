// Package packs holds the starter packs built into the program: common
// Windows and Linux commands with explanations, search synonyms and
// collection rules. They make the defaults database (store.UseDefaults),
// which every update rebuilds; users may change or delete everything they
// bring, and their changes live in their own database.
package packs

import (
	"embed"
	"sort"

	"github.com/thangkes/cmdex/internal/store"
)

//go:embed *.json
var files embed.FS

// All returns the starter packs in file-name order.
func All() []store.Pack {
	entries, _ := files.ReadDir(".")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []store.Pack
	for _, n := range names {
		b, err := files.ReadFile(n)
		if err != nil {
			continue
		}
		p, err := store.ParsePack(b)
		if err != nil {
			panic("starter pack " + n + ": " + err.Error()) // caught by the packs test before release
		}
		out = append(out, p)
	}
	return out
}
