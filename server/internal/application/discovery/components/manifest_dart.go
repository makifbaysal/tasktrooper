package components

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"gopkg.in/yaml.v3"
)

type dartEcosystem struct{}

func (dartEcosystem) Name() string { return "dart" }

func (dartEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "pubspec.yaml")
}

type pubspec struct {
	Name         string               `yaml:"name"`
	Dependencies map[string]yaml.Node `yaml:"dependencies"`
	Environment  map[string]string    `yaml:"environment"`
}

func (dartEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "pubspec.yaml")
	raw, err := tree.Read(path)
	if err != nil {
		return nil
	}
	var spec pubspec
	if yaml.Unmarshal(raw, &spec) != nil {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "dart", Path: path, Name: spec.Name, Dependencies: map[string]string{}}
	for name := range spec.Dependencies {
		info.Dependencies[name] = ""
	}
	if sdk := spec.Environment["sdk"]; sdk != "" {
		info.LanguageVersion = trimVersion(sdk)
	}
	return info
}
