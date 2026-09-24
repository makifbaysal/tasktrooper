package components

import (
	"encoding/json"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type nodeEcosystem struct{}

func (nodeEcosystem) Name() string { return "node" }

func (nodeEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "package.json")
}

type packageJSON struct {
	Name            string            `json:"name"`
	Workspaces      json.RawMessage   `json:"workspaces"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         map[string]string `json:"engines"`
	Bin             json.RawMessage   `json:"bin"`
	Main            string            `json:"main"`
	Module          string            `json:"module"`
	Types           string            `json:"types"`
	Typings         string            `json:"typings"`
	Exports         json.RawMessage   `json:"exports"`
}

func (nodeEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "package.json")
	raw, err := tree.Read(path)
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}
	info := &ManifestInfo{
		Ecosystem:       "node",
		Path:            path,
		Name:            pkg.Name,
		Dependencies:    pkg.Dependencies,
		DevDependencies: pkg.DevDependencies,
		Scripts:         pkg.Scripts,
		Workspaces:      parseWorkspacesField(pkg.Workspaces),
		Bin:             len(pkg.Bin) > 0 && string(pkg.Bin) != "null",
	}
	if info.Dependencies == nil {
		info.Dependencies = map[string]string{}
	}
	if v := pkg.Engines["node"]; v != "" {
		info.LanguageVersion = trimVersion(v)
	}
	hasEntry := pkg.Main != "" || pkg.Module != "" || pkg.Types != "" || pkg.Typings != "" || len(pkg.Exports) > 0
	_, hasDev := info.Scripts["dev"]
	_, hasStart := info.Scripts["start"]
	_, hasServe := info.Scripts["serve"]
	info.NoOwnSource = hasEntry && !hasDev && !hasStart && !hasServe
	return info
}

func parseWorkspacesField(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var globs []string
	if json.Unmarshal(raw, &globs) == nil {
		return globs
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Packages
	}
	return nil
}
