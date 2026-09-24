package components

import (
	"regexp"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type mavenEcosystem struct{}

func (mavenEcosystem) Name() string { return "maven" }

func (mavenEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "pom.xml")
}

var (
	mavenArtifactRe = regexp.MustCompile(`<artifactId>([^<]+)</artifactId>`)
	mavenDepRe      = regexp.MustCompile(`<dependency>\s*<groupId>([^<]+)</groupId>\s*<artifactId>([^<]+)</artifactId>`)
)

func (mavenEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "pom.xml")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "maven", Path: path, Dependencies: map[string]string{}}
	if m := mavenArtifactRe.FindStringSubmatch(body); m != nil {
		info.Name = m[1]
	}
	for _, m := range mavenDepRe.FindAllStringSubmatch(body, -1) {
		info.Dependencies[m[1]+":"+m[2]] = ""
	}
	return info
}
