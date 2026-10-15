// Package fsmap reads a folder for the folder map and acts on its files at
// the user's request: open, show in the file manager, rename, new folder,
// move to the trash.
//
// Reading looks at names, sizes and dates only, never at file contents, and
// stops at Depth layers below the folder in the centre: the map opens a
// deeper folder by making it the centre. Links (symlinks, junctions) are
// listed as files and never followed, so a loop cannot trap the scan.
//
// Every change is limited to the folder last scanned (Inside), never touches
// that folder itself or the system's own folders (protected), and deletes
// only by moving to the trash.
package fsmap

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Depth is how many layers below the centre are read: with the centre, the
// map shows four.
const Depth = 3

const (
	perDir     = 60   // entries shown per folder; the others are summed in More
	maxEntries = 2000 // entries read per scan, then deeper folders stay closed
)

// Node is a file or folder on the map.
type Node struct {
	Name     string  `json:"name"`
	Dir      bool    `json:"dir"`
	Link     bool    `json:"link,omitempty"` // a symlink or junction: shown, not followed
	Size     int64   `json:"size"`           // folders: the files read below it
	Mod      int64   `json:"mod"`            // last change, Unix seconds
	Read     bool    `json:"read"`           // folders: their entries were read
	Partial  bool    `json:"partial"`        // folders: something below was not read
	Denied   bool    `json:"denied,omitempty"`
	Files    int     `json:"files"` // folders: files directly inside
	Dirs     int     `json:"dirs"`  // folders: folders directly inside
	Kids     []*Node `json:"kids,omitempty"`
	More     int     `json:"more,omitempty"` // entries not shown
	MoreSize int64   `json:"moreSize,omitempty"`
}

// Map is a scanned folder.
type Map struct {
	Root   string `json:"root"`
	Parent string `json:"parent"` // "" at the top of a drive or of /
	Sep    string `json:"sep"`
	Tree   *Node  `json:"tree"`
}

// Scan reads root and Depth layers below it, breadth first, so a large
// folder shows its upper layers before the limit closes deeper ones.
func Scan(ctx context.Context, root string) (*Map, error) {
	root, err := clean(root)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, errors.New("not a folder")
	}
	m := &Map{Root: root, Sep: string(filepath.Separator), Tree: &Node{Name: filepath.Base(root), Dir: true, Mod: fi.ModTime().Unix()}}
	if p := filepath.Dir(root); p != root {
		m.Parent = p
	}
	if m.Tree.Name == "." || m.Tree.Name == string(filepath.Separator) || m.Tree.Name == "" {
		m.Tree.Name = root
	}
	type job struct {
		n     *Node
		path  string
		depth int
	}
	queue := []job{{m.Tree, root, 0}}
	read := 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		j := queue[0]
		queue = queue[1:]
		if j.depth >= Depth || read >= maxEntries {
			continue // stays closed: opened when it becomes the centre
		}
		entries, err := os.ReadDir(j.path)
		j.n.Read = true
		if err != nil {
			j.n.Denied = true
			continue
		}
		read += len(entries)
		var kids []*Node
		for _, e := range entries {
			k := &Node{Name: e.Name()}
			if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				k.Link = true // never followed
			} else {
				k.Dir = e.IsDir()
			}
			if info, err := e.Info(); err == nil {
				k.Mod = info.ModTime().Unix()
				if info.Mode().IsRegular() {
					k.Size = info.Size()
				}
			}
			if k.Dir {
				j.n.Dirs++
			} else {
				j.n.Files++
			}
			kids = append(kids, k)
		}
		// folders first, then the largest files
		sort.SliceStable(kids, func(a, b int) bool {
			if kids[a].Dir != kids[b].Dir {
				return kids[a].Dir
			}
			if !kids[a].Dir && kids[a].Size != kids[b].Size {
				return kids[a].Size > kids[b].Size
			}
			return strings.ToLower(kids[a].Name) < strings.ToLower(kids[b].Name)
		})
		if len(kids) > perDir {
			for _, k := range kids[perDir:] {
				j.n.More++
				j.n.MoreSize += k.Size
			}
			kids = kids[:perDir]
		}
		j.n.Kids = kids
		for _, k := range kids {
			if k.Dir {
				queue = append(queue, job{k, filepath.Join(j.path, k.Name), j.depth + 1})
			}
		}
	}
	total(m.Tree)
	return m, nil
}

// total adds up what was read below each folder.
func total(n *Node) (int64, int64, bool) {
	if !n.Dir {
		return n.Size, n.Mod, false
	}
	size, mod, partial := n.MoreSize, n.Mod, !n.Read || n.Denied
	for _, k := range n.Kids {
		s, m, p := total(k)
		size += s
		if m > mod {
			mod = m
		}
		partial = partial || p
	}
	if n.More > 0 {
		partial = partial || hasDirs(n)
	}
	n.Size, n.Mod, n.Partial = size, mod, partial
	return size, mod, partial
}

func hasDirs(n *Node) bool {
	shown := 0
	for _, k := range n.Kids {
		if k.Dir {
			shown++
		}
	}
	return n.Dirs > shown
}

// clean makes path absolute and clean, refusing relative ones: the map never
// depends on the folder Cmdex was started in.
func clean(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("the path must be absolute")
	}
	return filepath.Clean(path), nil
}
