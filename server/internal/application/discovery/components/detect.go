// Package components decides a working copy's shape and its components, and
// reads each component's role, stack and commands.
package components

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Result struct {
	Shape         domain.RepoShape
	ShapeEvidence []domain.SourceEvidence
	Components    []domain.DetectedComponent
	Languages     []domain.LanguageShare
	Warnings      []string
}

func Detect(tree *inventory.Tree) Result {
	built := buildComponents(tree)

	comps := make([]domain.DetectedComponent, 0, len(built.Dirs))
	for _, dir := range built.Dirs {
		info := built.Manifests[dir]
		role, roleConf, roleEv := classifyRole(tree, dir, info)
		cmds := buildCommands(tree, dir, info)

		comp := domain.DetectedComponent{
			Path:           dir,
			Name:           componentName(dir, info),
			Role:           role,
			RoleConfidence: roleConf,
			RoleEvidence:   roleEv,
			Stack:          buildStack(tree, dir, info),
			Commands:       cmds,
			DevPort:        detectDevPort(tree, dir, info, cmds),
		}
		if info != nil {
			comp.PackageName = info.Name
		}
		if role == domain.ComponentRoleMobile {
			comp.Mobile = buildMobileFacts(tree, dir, info)
		}
		comps = append(comps, comp)
	}

	return Result{
		Shape:         built.Shape,
		ShapeEvidence: built.ShapeEvidence,
		Components:    comps,
		Languages:     repoLanguages(tree),
		Warnings:      built.Warnings,
	}
}

func componentName(dir string, info *ManifestInfo) string {
	if dir == "." {
		if info != nil {
			return info.Name
		}
		return ""
	}
	return baseName(dir)
}
