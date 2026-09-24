package components

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type gradleEcosystem struct{}

func (gradleEcosystem) Name() string { return "gradle" }

func (gradleEcosystem) Roots(tree *inventory.Tree) []string {
	dirs := map[string]bool{}
	for _, base := range []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"} {
		for _, d := range manifestDirs(tree, base) {
			dirs[d] = true
		}
	}
	return sortedSet(dirs)
}

var androidAppPluginPattern = regexp.MustCompile(`com\.android\.application\b`)

// gradleJVMFrameworks are matched as raw substrings against the build file —
// a plugin id or a dependency coordinate both mention the framework by name,
// and a line-based reader has no reason to tell them apart.
var gradleJVMFrameworks = []string{
	"org.springframework.boot", "spring-boot", "io.ktor", "quarkus", "micronaut",
}

func (gradleEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	info := &ManifestInfo{Ecosystem: "gradle", Dependencies: map[string]string{}}
	for _, base := range []string{"build.gradle", "build.gradle.kts"} {
		p := inventory.Join(dir, base)
		body := tree.ReadString(p)
		if body == "" {
			continue
		}
		info.Path = p
		if androidAppPluginPattern.MatchString(body) {
			info.Dependencies["com.android.application"] = ""
		}
		for _, kw := range gradleJVMFrameworks {
			if strings.Contains(body, kw) {
				info.Dependencies[kw] = ""
			}
		}
		break
	}
	if info.Path == "" {
		for _, base := range []string{"settings.gradle", "settings.gradle.kts"} {
			p := inventory.Join(dir, base)
			if tree.Has(p) {
				info.Path = p
				break
			}
		}
	}
	if info.Path == "" {
		return nil
	}
	info.Name = baseName(dir)
	return info
}
