// Package links reads what each component connects to and where it ships:
// dependency manifests, example env files, docker-compose, ORM schemas,
// workspace package references and deploy markers, all reduced to
// domain.DetectedLink and domain.DeploySignal.
package links

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Result struct {
	Links         []domain.DetectedLink
	DeploySignals []domain.DeploySignal
	Warnings      []string
}

// extractor is one signal source; each writes into the shared collector
// rather than returning its own result, so later extractors (env vars) can
// find and extend what an earlier one (manifest dependencies) already found.
type extractor interface{ extract(ctx *scanCtx) }

// scanCtx is the tree and component list every extractor reads, plus the
// collector every extractor writes into.
type scanCtx struct {
	tree *inventory.Tree
	// components is sorted deepest-path-first so ownerOf's first match is
	// always the most specific one.
	components []domain.DetectedComponent
	byPath     map[string]domain.DetectedComponent
	collector  *collector
}

func Detect(tree *inventory.Tree, components []domain.DetectedComponent) Result {
	ctx := newScanCtx(tree, components)

	// Order matters: deps/schema/compose establish a component's primary
	// resource links (high confidence, from manifests) before env vars try to
	// fold themselves into an existing link rather than duplicating it.
	extractors := []extractor{
		depsExtractor{},
		schemaExtractor{},
		composeExtractor{},
		envExtractor{},
		workspaceExtractor{},
		deployExtractor{},
	}
	for _, e := range extractors {
		e.extract(ctx)
	}

	return ctx.collector.result()
}

func newScanCtx(tree *inventory.Tree, components []domain.DetectedComponent) *scanCtx {
	sorted := make([]domain.DetectedComponent, len(components))
	copy(sorted, components)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i].Path) > len(sorted[j].Path) })

	byPath := make(map[string]domain.DetectedComponent, len(components))
	for _, c := range components {
		byPath[c.Path] = c
	}

	return &scanCtx{tree: tree, components: sorted, byPath: byPath, collector: newCollector()}
}

// ownerOf resolves the deepest component whose Path contains file, matching
// domain.Component.Contains: "." owns everything, every other path owns
// itself and everything under it. A file no component contains has no owner.
func (c *scanCtx) ownerOf(file string) (string, bool) {
	for _, comp := range c.components {
		if componentContains(comp.Path, file) {
			return comp.Path, true
		}
	}
	return "", false
}

func componentContains(componentPath, file string) bool {
	if componentPath == "." || componentPath == "" {
		return true
	}
	return file == componentPath || strings.HasPrefix(file, componentPath+"/")
}

func (c *scanCtx) component(path string) (domain.DetectedComponent, bool) {
	comp, ok := c.byPath[path]
	return comp, ok
}

// rootComponent reports whether "." is itself a registered component, the
// first fallback a workflow step without a resolvable working directory gets.
func (c *scanCtx) rootComponent() (string, bool) {
	if _, ok := c.byPath["."]; ok {
		return ".", true
	}
	return "", false
}

// onlyComponent reports the sole component when there is exactly one, the
// last fallback for an unownable workflow step.
func (c *scanCtx) onlyComponent() (string, bool) {
	if len(c.byPath) == 1 {
		for p := range c.byPath {
			return p, true
		}
	}
	return "", false
}
