package links

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// workspaceExtractor links a component to another component it names as a
// dependency: an npm workspace package, or a Go module required by path
// (including a replace directive pointed at a relative in-repo directory).
type workspaceExtractor struct{}

func (workspaceExtractor) extract(ctx *scanCtx) {
	packageNameOwner := map[string]string{}
	for _, c := range ctx.components {
		if c.PackageName != "" {
			packageNameOwner[c.PackageName] = c.Path
		}
	}
	if len(packageNameOwner) == 0 {
		return
	}

	for _, file := range ctx.tree.Files {
		base := baseName(file)
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		switch base {
		case "package.json":
			for _, dep := range npmDeps(ctx.tree, file) {
				target, ok := packageNameOwner[dep.name]
				if !ok || target == owner {
					continue
				}
				addWorkspaceLink(ctx, owner, target, dep.name, dep.evidence)
			}
		case "go.mod":
			_, requires, replaces := goModDirectives(ctx.tree, file)
			for _, req := range requires {
				target, ok := packageNameOwner[req.name]
				if !ok || target == owner {
					continue
				}
				addWorkspaceLink(ctx, owner, target, req.name, req.evidence)
			}
			for modName, relDir := range replaces {
				resolved := joinRel(componentDirFromFile(file), relDir)
				for _, c := range ctx.components {
					if c.Path == resolved && c.Path != owner {
						addWorkspaceLink(ctx, owner, c.Path, modName, domain.SourceEvidence{Path: file})
					}
				}
			}
		}
	}
}

func addWorkspaceLink(ctx *scanCtx, owner, target, name string, evidence domain.SourceEvidence) {
	ctx.collector.addLink(rawSignal{
		componentPath: owner,
		signalKey:     "pkg:" + name,
		protocol:      domain.LinkPackage,
		target: domain.LinkTarget{
			Kind:          domain.LinkTargetComponent,
			ComponentPath: target,
			PackageName:   name,
		},
		evidence:   []domain.SourceEvidence{evidence},
		confidence: domain.ConfidenceHigh,
	})
}

func componentDirFromFile(file string) string {
	if idx := strings.LastIndexByte(file, '/'); idx >= 0 {
		return file[:idx]
	}
	return ""
}
