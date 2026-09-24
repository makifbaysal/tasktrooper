package components

import (
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

// ManifestInfo is what one ecosystem's Read extracts from its manifest file;
// role, stack and command detection all read from this rather than
// re-parsing the manifest themselves.
type ManifestInfo struct {
	Ecosystem       string
	Path            string
	Dir             string
	Name            string
	LanguageVersion string
	Dependencies    map[string]string
	DevDependencies map[string]string
	Scripts         map[string]string
	Workspaces      []string
	// Bin marks a package that ships an executable (npm "bin", or an Xcode
	// app target) — the cli/mobile role signal a manifest alone cannot give.
	Bin bool
	// HasPackageMain and NoOwnSource are ecosystem-specific "is this runnable
	// or a library" signals: HasPackageMain for Go/Rust's package main /
	// src/main.rs, NoOwnSource for a package.json with no runnable script but
	// a published entry point.
	HasPackageMain bool
	NoOwnSource    bool
}

func (m *ManifestInfo) hasDep(name string) (string, bool) {
	if m == nil {
		return "", false
	}
	if v, ok := m.Dependencies[name]; ok {
		return v, true
	}
	if v, ok := m.DevDependencies[name]; ok {
		return v, true
	}
	return "", false
}

func (m *ManifestInfo) anyDep(names ...string) (name, version string, ok bool) {
	for _, n := range names {
		if v, found := m.hasDep(n); found {
			return n, v, true
		}
	}
	return "", "", false
}

// hasDepPrefix reports whether any dependency name starts with prefix, for a
// scoped family like "@radix-ui/*".
func (m *ManifestInfo) hasDepPrefix(prefix string) (string, bool) {
	if m == nil {
		return "", false
	}
	for name := range m.Dependencies {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return name, true
		}
	}
	for name := range m.DevDependencies {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return name, true
		}
	}
	return "", false
}

// ecosystem is the one piece of code that has to change to teach discovery
// about a new manifest format: where its roots live in the tree, and how to
// read one into a ManifestInfo.
type ecosystem interface {
	Name() string
	Roots(tree *inventory.Tree) []string
	Read(tree *inventory.Tree, dir string) *ManifestInfo
}

func ecosystems() []ecosystem {
	return []ecosystem{
		nodeEcosystem{}, goEcosystem{}, pythonEcosystem{}, rustEcosystem{}, dartEcosystem{},
		swiftEcosystem{}, gradleEcosystem{}, mavenEcosystem{}, phpEcosystem{}, rubyEcosystem{},
		dotnetEcosystem{}, denoEcosystem{},
	}
}

// discoverManifests finds every ecosystem root in the tree, keyed by
// component directory. A directory matching more than one ecosystem (rare —
// e.g. a go.mod and a package.json living side by side, which the caller
// treats as one component, not two) keeps the first ecosystem in
// ecosystems() order.
func discoverManifests(tree *inventory.Tree) map[string]*ManifestInfo {
	out := map[string]*ManifestInfo{}
	for _, eco := range ecosystems() {
		for _, dir := range eco.Roots(tree) {
			if ignoredDir(dir) {
				continue
			}
			if _, exists := out[dir]; exists {
				continue
			}
			if info := eco.Read(tree, dir); info != nil {
				info.Dir = dir
				out[dir] = info
			}
		}
	}
	return out
}

func sortedDirs(m map[string]*ManifestInfo) []string {
	out := make([]string, 0, len(m))
	for d := range m {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func manifestDirs(tree *inventory.Tree, basename string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range tree.ByBase(basename) {
		d := inventory.Dir(p)
		if ignoredDir(d) || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
