package components

import "strings"

var ignoredDirNames = map[string]bool{
	"testdata": true, "fixtures": true, "examples": true, "example": true,
	"samples": true, "__tests__": true, "e2e": true, "scripts": true,
	"docs": true, ".github": true,
}

// ignoredDir reports whether dir, or any ancestor of it, is a directory
// discovery must not treat as component evidence: a manifest sitting inside
// a fixture or example tree describes a test case, not a shippable unit.
func ignoredDir(dir string) bool {
	if dir == "" || dir == "." {
		return false
	}
	for _, part := range strings.Split(dir, "/") {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ".") || ignoredDirNames[part] {
			return true
		}
	}
	return false
}

func trimVersion(v string) string {
	return strings.TrimLeft(strings.TrimSpace(v), "^~>=< ")
}

func baseName(dir string) string {
	if dir == "" || dir == "." {
		return ""
	}
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

func underAppsServicesPackages(rel string) bool {
	first := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		first = rel[:i]
	}
	return first == "apps" || first == "services" || first == "packages"
}
