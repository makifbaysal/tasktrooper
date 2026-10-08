package components

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

// Game engines keep their project file at the project root, but a Unity or
// Godot-C# root also carries a .csproj and a Unity root can carry
// package.json files, so these ecosystems are listed before dotnet and node:
// discoverManifests keeps the first ecosystem that claims a directory.

type unityEcosystem struct{}

func (unityEcosystem) Name() string { return "unity" }

func (unityEcosystem) Roots(tree *inventory.Tree) []string {
	dirs := map[string]bool{}
	for _, p := range tree.ByBase("ProjectVersion.txt") {
		settings := inventory.Dir(p)
		if baseName(settings) != "ProjectSettings" {
			continue
		}
		root := inventory.Dir(settings)
		if ignoredDir(root) {
			continue
		}
		dirs[root] = true
	}
	return sortedSet(dirs)
}

var (
	unityEditorVersionRe = regexp.MustCompile(`(?m)^m_EditorVersion:\s*(\S+)`)
	unityProductNameRe   = regexp.MustCompile(`(?m)^\s*productName:\s*(.+)$`)
)

func (unityEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "ProjectSettings/ProjectVersion.txt")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "unity", Path: path, Dependencies: map[string]string{}}
	if m := unityEditorVersionRe.FindStringSubmatch(body); m != nil {
		info.LanguageVersion = m[1]
	}
	if m := unityProductNameRe.FindStringSubmatch(tree.ReadString(inventory.Join(dir, "ProjectSettings/ProjectSettings.asset"))); m != nil {
		info.Name = strings.TrimSpace(m[1])
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if raw, err := tree.Read(inventory.Join(dir, "Packages/manifest.json")); err == nil && json.Unmarshal(raw, &manifest) == nil {
		for name, version := range manifest.Dependencies {
			info.Dependencies[name] = version
		}
	}
	return info
}

type godotEcosystem struct{}

func (godotEcosystem) Name() string { return "godot" }

func (godotEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "project.godot")
}

var (
	godotNameRe     = regexp.MustCompile(`(?m)^config/name\s*=\s*"([^"]*)"`)
	godotFeaturesRe = regexp.MustCompile(`(?m)^config/features\s*=\s*PackedStringArray\(\s*"(\d+\.\d+)"`)
)

func (godotEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "project.godot")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "godot", Path: path, Dependencies: map[string]string{}}
	if m := godotNameRe.FindStringSubmatch(body); m != nil {
		info.Name = m[1]
	}
	if m := godotFeaturesRe.FindStringSubmatch(body); m != nil {
		info.LanguageVersion = m[1]
	}
	for _, f := range tree.Under(dir) {
		if inventory.Dir(f) == dir && strings.HasSuffix(f, ".csproj") {
			info.Dependencies["Godot.NET.Sdk"] = ""
			break
		}
	}
	for _, addon := range []string{"gdUnit4", "gut"} {
		if tree.HasDir(inventory.Join(dir, "addons/"+addon)) {
			info.DevDependencies = map[string]string{addon: ""}
			break
		}
	}
	return info
}

type unrealEcosystem struct{}

func (unrealEcosystem) Name() string { return "unreal" }

func (unrealEcosystem) Roots(tree *inventory.Tree) []string {
	dirs := map[string]bool{}
	for _, f := range tree.WithSuffix(".uproject") {
		d := inventory.Dir(f)
		if ignoredDir(d) {
			continue
		}
		dirs[d] = true
	}
	return sortedSet(dirs)
}

func (unrealEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	var path string
	for _, f := range tree.Under(dir) {
		if inventory.Dir(f) == dir && strings.HasSuffix(f, ".uproject") {
			path = f
			break
		}
	}
	if path == "" {
		return nil
	}
	info := &ManifestInfo{
		Ecosystem:    "unreal",
		Path:         path,
		Name:         strings.TrimSuffix(baseName(path), ".uproject"),
		Dependencies: map[string]string{},
	}
	var project struct {
		EngineAssociation string `json:"EngineAssociation"`
		Plugins           []struct {
			Name    string `json:"Name"`
			Enabled bool   `json:"Enabled"`
		} `json:"Plugins"`
	}
	if raw, err := tree.Read(path); err == nil && json.Unmarshal(raw, &project) == nil {
		info.LanguageVersion = project.EngineAssociation
		for _, p := range project.Plugins {
			if p.Enabled {
				info.Dependencies[p.Name] = ""
			}
		}
	}
	return info
}

type dbtEcosystem struct{}

func (dbtEcosystem) Name() string { return "dbt" }

func (dbtEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "dbt_project.yml")
}

var dbtNameRe = regexp.MustCompile(`(?m)^name:\s*['"]?([A-Za-z0-9_]+)`)

func (dbtEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "dbt_project.yml")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "dbt", Path: path, Dependencies: map[string]string{}}
	if m := dbtNameRe.FindStringSubmatch(body); m != nil {
		info.Name = m[1]
	}
	return info
}
