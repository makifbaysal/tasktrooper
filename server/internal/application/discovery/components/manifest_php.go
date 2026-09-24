package components

import (
	"encoding/json"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type phpEcosystem struct{}

func (phpEcosystem) Name() string { return "php" }

func (phpEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "composer.json")
}

func (phpEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "composer.json")
	raw, err := tree.Read(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Name    string            `json:"name"`
		Require map[string]string `json:"require"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "php", Path: path, Name: pkg.Name, Dependencies: map[string]string{}}
	for k, v := range pkg.Require {
		info.Dependencies[k] = v
	}
	return info
}
