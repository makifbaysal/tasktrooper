package components

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type swiftEcosystem struct{}

func (swiftEcosystem) Name() string { return "swift" }

func (swiftEcosystem) Roots(tree *inventory.Tree) []string {
	dirs := map[string]bool{}
	for _, d := range manifestDirs(tree, "Package.swift") {
		dirs[d] = true
	}
	for _, p := range tree.ByBase("project.pbxproj") {
		xcodeDir := inventory.Dir(p)
		if !strings.HasSuffix(xcodeDir, ".xcodeproj") {
			continue
		}
		parent := inventory.Dir(xcodeDir)
		if ignoredDir(parent) {
			continue
		}
		dirs[parent] = true
	}
	return sortedSet(dirs)
}

func (swiftEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	info := &ManifestInfo{Ecosystem: "swift", Dependencies: map[string]string{}}
	if tree.Has(inventory.Join(dir, "Package.swift")) {
		info.Path = inventory.Join(dir, "Package.swift")
		info.Name = baseName(dir)
	}
	if xp := xcodeprojIn(tree, dir); xp != "" {
		if info.Path == "" {
			info.Path = xp + "/project.pbxproj"
		}
		info.Name = strings.TrimSuffix(baseName(xp), ".xcodeproj")
		// An .xcodeproj marks a build target; a genuine app target vs. a
		// framework can only be told apart by parsing the pbxproj's
		// PRODUCT_TYPE, which the role classifier does not need — the mobile
		// rule only cares that there is no package.json claiming this as a
		// React Native root instead.
		info.Bin = true
	}
	if info.Path == "" {
		return nil
	}
	return info
}

func xcodeprojIn(tree *inventory.Tree, dir string) string {
	for _, p := range tree.ByBase("project.pbxproj") {
		xd := inventory.Dir(p)
		if strings.HasSuffix(xd, ".xcodeproj") && inventory.Dir(xd) == dir {
			return xd
		}
	}
	return ""
}
