// Package packs holds the starter packs built into the program: common
// Windows and Linux commands with explanations, search synonyms and
// collection rules. They make the defaults database (store.UseDefaults),
// which every update rebuilds; users may change or delete everything they
// bring, and their changes live in their own database.
//
// Tool packs (tools/*.json) hold the commands of programs that are not
// part of the system, such as nmap or python. They are built in too, so
// nothing is downloaded, but join the defaults only once the user turns
// them on (Cmdex offers one when it finds its program on this computer).
package packs

import (
	"embed"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/thangkes/cmdex/internal/store"
)

//go:embed *.json tools
var files embed.FS

// All returns the starter packs in file-name order.
func All() []store.Pack { return read(".") }

// Tools returns the tool packs in file-name order. They are read once:
// callers must not change them.
func Tools() []store.Pack { return tools() }

var tools = sync.OnceValue(func() []store.Pack {
	out := read("tools")
	seen := map[string]bool{}
	for _, p := range out {
		if !ValidToolPack(p) || seen[p.Name] {
			panic("tool pack " + p.Name + ": needs a unique name, a title, an origin and the programs that bring it") // caught by the packs test
		}
		seen[p.Name] = true
	}
	return out
})

var reProgram = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// ValidToolPack reports whether p may be offered as a tool pack: a name,
// a title, where its content comes from, and program names that are plain
// file names (never a path).
func ValidToolPack(p store.Pack) bool {
	if !reProgram.MatchString(p.Name) || p.Title.In("en") == "" || strings.TrimSpace(p.Origin) == "" || len(p.Tools) == 0 {
		return false
	}
	for _, t := range p.Tools {
		if !reProgram.MatchString(t) {
			return false
		}
	}
	return true
}

func read(dir string) []store.Pack {
	entries, _ := files.ReadDir(dir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []store.Pack
	for _, n := range names {
		b, err := files.ReadFile(path.Join(dir, n))
		if err != nil {
			continue
		}
		p, err := store.ParsePack(b)
		if err != nil {
			panic("pack " + path.Join(dir, n) + ": " + err.Error()) // caught by the packs test before release
		}
		out = append(out, p)
	}
	return out
}
