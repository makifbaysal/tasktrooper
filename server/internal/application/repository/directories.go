package repository

import (
	"os"
	"sort"
	"strings"
)

// ListChildDirectories lists this directory's immediate subdirectories,
// skipping the ones no folder picker should ever offer (VCS internals,
// dependency caches, build output).
func ListChildDirectories(abs string) ([]string, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || skipDir(e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".venv", "target":
		return true
	}
	return strings.HasPrefix(name, ".")
}
