package components

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type goEcosystem struct{}

func (goEcosystem) Name() string { return "go" }

func (goEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "go.mod")
}

var (
	goModuleRe  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	goVersionRe = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)`)
)

func (goEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "go.mod")
	raw := tree.ReadString(path)
	if raw == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "go", Path: path, Dependencies: map[string]string{}}
	if m := goModuleRe.FindStringSubmatch(raw); m != nil {
		info.Name = m[1]
	}
	if m := goVersionRe.FindStringSubmatch(raw); m != nil {
		info.LanguageVersion = m[1]
	}
	inRequire := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inRequire = true
			continue
		case inRequire && trimmed == ")":
			inRequire = false
			continue
		case strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "("):
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "require "))
		case !inRequire:
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "// indirect"))
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && strings.HasPrefix(fields[1], "v") {
			info.Dependencies[fields[0]] = strings.TrimPrefix(fields[1], "v")
		}
	}
	return info
}
