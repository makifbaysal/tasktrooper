package components

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// maxComponents bounds a pathological monorepo: past this, the components
// tab stops being a map and starts being a directory listing.
const maxComponents = 50

type buildResult struct {
	Shape         domain.RepoShape
	ShapeEvidence []domain.SourceEvidence
	Dirs          []string
	Manifests     map[string]*ManifestInfo
	Warnings      []string
}

// buildComponents runs shape detection end to end: find every ecosystem
// root, fold in workspace declarations for evidence, absorb what a
// cross-platform or Gradle root owns, drop a workspace-only root, then cap
// and classify.
func buildComponents(tree *inventory.Tree) buildResult {
	manifests := discoverManifests(tree)
	decls := discoverWorkspaces(tree, manifests)

	absorbMobile(manifests)
	absorbGradle(manifests)
	dropWorkspaceOnlyRoots(tree, manifests)

	var warnings []string
	dirs := sortedDirs(manifests)
	if len(dirs) > maxComponents {
		dirs = largestDirs(tree, dirs, maxComponents)
		kept := map[string]bool{}
		for _, d := range dirs {
			kept[d] = true
		}
		for d := range manifests {
			if !kept[d] {
				delete(manifests, d)
			}
		}
		warnings = append(warnings, "capped components at 50; kept the largest by file count")
	}

	res := buildResult{Dirs: dirs, Manifests: manifests, Warnings: warnings}
	if len(dirs) >= 2 {
		res.Shape = domain.RepoShapeMonorepo
		res.ShapeEvidence = shapeEvidence(decls, manifests, dirs)
		return res
	}

	res.Shape = domain.RepoShapeSingle
	if len(dirs) == 0 {
		res.Dirs = []string{"."}
	}
	return res
}

func shapeEvidence(decls []workspaceDecl, manifests map[string]*ManifestInfo, dirs []string) []domain.SourceEvidence {
	seen := map[string]bool{}
	var out []domain.SourceEvidence
	for _, d := range decls {
		matched := false
		for dir := range d.Dirs {
			if manifests[dir] != nil {
				matched = true
				break
			}
		}
		if !matched || seen[d.Evidence.Path] {
			continue
		}
		seen[d.Evidence.Path] = true
		out = append(out, d.Evidence)
	}
	if len(out) == 0 {
		for _, dir := range dirs {
			if m := manifests[dir]; m != nil && m.Path != "" {
				out = append(out, domain.SourceEvidence{Path: m.Path})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func largestDirs(tree *inventory.Tree, dirs []string, n int) []string {
	type kv struct {
		dir   string
		files int
	}
	ranked := make([]kv, 0, len(dirs))
	for _, d := range dirs {
		ranked = append(ranked, kv{d, len(tree.Under(d))})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].files != ranked[j].files {
			return ranked[i].files > ranked[j].files
		}
		return ranked[i].dir < ranked[j].dir
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]string, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, r.dir)
	}
	sort.Strings(out)
	return out
}

// absorbMobile removes the ios/ and android/ subdirectories a cross-platform
// mobile root owns: those subtrees are the native shell the framework
// generated, not independent components.
func absorbMobile(manifests map[string]*ManifestInfo) {
	var absorbers []string
	for dir, info := range manifests {
		if isCrossPlatformMobileRoot(info) {
			absorbers = append(absorbers, dir)
		}
	}
	for _, dir := range absorbers {
		for _, sub := range []string{"ios", "android"} {
			removeUnder(manifests, inventory.Join(dir, sub))
		}
	}
}

func isCrossPlatformMobileRoot(info *ManifestInfo) bool {
	if info.Ecosystem == "dart" {
		_, ok := info.hasDep("flutter")
		return ok
	}
	if info.Ecosystem == "node" {
		_, _, ok := info.anyDep("react-native", "expo")
		return ok
	}
	return false
}

func removeUnder(manifests map[string]*ManifestInfo, target string) {
	delete(manifests, target)
	prefix := target + "/"
	for dir := range manifests {
		if strings.HasPrefix(dir, prefix) {
			delete(manifests, dir)
		}
	}
}

// absorbGradle removes the modules a Gradle root's settings.gradle owns,
// unless a module sits under apps/, services/ or packages/ — the convention
// this repository (and the spec) uses to mark a module as independently
// deployable rather than part of the same Gradle build.
func absorbGradle(manifests map[string]*ManifestInfo) {
	var gradleRoots []string
	for dir, info := range manifests {
		if info.Ecosystem != "gradle" {
			continue
		}
		if strings.HasSuffix(info.Path, "settings.gradle") || strings.HasSuffix(info.Path, "settings.gradle.kts") {
			gradleRoots = append(gradleRoots, dir)
		}
	}
	for _, root := range gradleRoots {
		for dir, info := range manifests {
			if dir == root || info.Ecosystem != "gradle" || !isDescendant(root, dir) {
				continue
			}
			if underAppsServicesPackages(relativeTo(root, dir)) {
				continue
			}
			delete(manifests, dir)
		}
	}
}

func isDescendant(root, dir string) bool {
	if root == "." {
		return dir != "."
	}
	return strings.HasPrefix(dir, root+"/")
}

func relativeTo(root, dir string) string {
	if root == "." {
		return dir
	}
	return strings.TrimPrefix(dir, root+"/")
}

// dropWorkspaceOnlyRoots removes a root package.json that only declares
// workspaces: no src/, app/, pages/ or index.* of its own means there is no
// app here, just the monorepo's orchestration manifest.
func dropWorkspaceOnlyRoots(tree *inventory.Tree, manifests map[string]*ManifestInfo) {
	for dir, info := range manifests {
		if info.Ecosystem != "node" || len(info.Workspaces) == 0 {
			continue
		}
		if rootHasOwnSource(tree, dir) {
			continue
		}
		delete(manifests, dir)
	}
}

func rootHasOwnSource(tree *inventory.Tree, dir string) bool {
	for _, d := range []string{"src", "app", "pages"} {
		if tree.HasDir(inventory.Join(dir, d)) {
			return true
		}
	}
	for _, base := range []string{"index.js", "index.ts", "index.jsx", "index.tsx", "index.mjs", "index.cjs"} {
		if tree.Has(inventory.Join(dir, base)) {
			return true
		}
	}
	return false
}
