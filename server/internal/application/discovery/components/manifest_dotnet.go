package components

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type dotnetEcosystem struct{}

func (dotnetEcosystem) Name() string { return "dotnet" }

func (dotnetEcosystem) Roots(tree *inventory.Tree) []string {
	dirs := map[string]bool{}
	for _, f := range tree.WithSuffix(".csproj") {
		d := inventory.Dir(f)
		if ignoredDir(d) {
			continue
		}
		dirs[d] = true
	}
	return sortedSet(dirs)
}

var packageRefRe = regexp.MustCompile(`<PackageReference\s+Include="([^"]+)"`)

func (dotnetEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	var path, body string
	for _, f := range tree.Under(dir) {
		if inventory.Dir(f) == dir && strings.HasSuffix(f, ".csproj") {
			path, body = f, tree.ReadString(f)
			break
		}
	}
	if path == "" {
		return nil
	}
	info := &ManifestInfo{
		Ecosystem:    "dotnet",
		Path:         path,
		Name:         strings.TrimSuffix(baseName(path), ".csproj"),
		Dependencies: map[string]string{},
	}
	for _, m := range packageRefRe.FindAllStringSubmatch(body, -1) {
		info.Dependencies[m[1]] = ""
	}
	return info
}
