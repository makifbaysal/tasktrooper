package components

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type rustEcosystem struct{}

func (rustEcosystem) Name() string { return "rust" }

func (rustEcosystem) Roots(tree *inventory.Tree) []string {
	return manifestDirs(tree, "Cargo.toml")
}

var (
	cargoNameRe    = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	cargoDepLineRe = regexp.MustCompile(`(?m)^([A-Za-z0-9_-]+)\s*=`)
)

func (rustEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	path := inventory.Join(dir, "Cargo.toml")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	info := &ManifestInfo{Ecosystem: "rust", Path: path, Dependencies: map[string]string{}}
	section := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			section = trimmed
			continue
		}
		switch section {
		case "[package]":
			if m := cargoNameRe.FindStringSubmatch(trimmed); m != nil {
				info.Name = m[1]
			}
		case "[dependencies]", "[dev-dependencies]":
			if m := cargoDepLineRe.FindStringSubmatch(trimmed); m != nil {
				info.Dependencies[m[1]] = ""
			}
		}
	}
	if rt := strings.TrimSpace(tree.ReadString(inventory.Join(dir, "rust-toolchain.toml"))); rt != "" {
		if m := regexp.MustCompile(`channel\s*=\s*"([^"]+)"`).FindStringSubmatch(rt); m != nil {
			info.LanguageVersion = m[1]
		}
	} else if rt := strings.TrimSpace(tree.ReadString(inventory.Join(dir, "rust-toolchain"))); rt != "" {
		info.LanguageVersion = rt
	}
	hasLib := tree.Has(inventory.Join(dir, "src/lib.rs"))
	hasMain := tree.Has(inventory.Join(dir, "src/main.rs"))
	info.HasPackageMain = hasMain
	info.NoOwnSource = hasLib && !hasMain
	return info
}
