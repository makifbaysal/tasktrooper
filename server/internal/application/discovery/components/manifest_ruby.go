package components

import (
	"regexp"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type rubyEcosystem struct{}

func (rubyEcosystem) Name() string { return "ruby" }

func (rubyEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "Gemfile")
}

var gemRe = regexp.MustCompile(`(?m)^\s*gem\s+["']([^"']+)["']`)

func (rubyEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "Gemfile")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "ruby", Path: path, Name: baseName(dir), Dependencies: map[string]string{}}
	for _, m := range gemRe.FindAllStringSubmatch(body, -1) {
		info.Dependencies[m[1]] = ""
	}
	return info
}
