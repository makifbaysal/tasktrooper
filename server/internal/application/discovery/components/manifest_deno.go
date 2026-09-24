package components

import (
	"encoding/json"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type denoEcosystem struct{}

func (denoEcosystem) Name() string { return "deno" }

func (denoEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "deno.json")
}

func (denoEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "deno.json")
	raw, err := tree.Read(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Name  string            `json:"name"`
		Tasks map[string]string `json:"tasks"`
	}
	_ = json.Unmarshal(raw, &cfg)
	return &ManifestInfo{Ecosystem: "deno", Path: path, Name: cfg.Name, Scripts: cfg.Tasks, Dependencies: map[string]string{}}
}
