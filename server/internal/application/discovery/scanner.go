// Package discovery is a pure scanner: it reads one git working copy and
// returns a domain.ScanResult, replacing the older application/repofacts and
// the detectors in application/repository (detect.go, appidentity.go,
// buildtargets.go).
package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/ci"
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/components"
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/links"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Scanner struct{}

func New() *Scanner { return &Scanner{} }

// Scan loads the tree, runs ScanTree, then fills Git; only a missing or
// non-directory root fails the scan — everything past that degrades to a
// warning so a scan always returns a usable result.
func (s *Scanner) Scan(ctx context.Context, root string, emit func(domain.ScanEvent)) (domain.ScanResult, error) {
	st, err := os.Stat(root)
	if err != nil {
		return domain.ScanResult{}, err
	}
	if !st.IsDir() {
		return domain.ScanResult{}, errors.New("working copy is not a directory: " + root)
	}

	tree, err := inventory.Load(ctx, root)
	if err != nil {
		return domain.ScanResult{}, err
	}

	result := ScanTree(ctx, tree, emit)

	g, warnings := gitFacts(ctx, root)
	result.Git = g
	result.Warnings = append(result.Warnings, warnings...)
	return result, nil
}

// ScanTree is the pure part: everything discovery can read off an in-memory
// or on-disk tree with no git or filesystem calls beyond the Tree itself,
// which is what makes it testable against fstest.MapFS fixtures.
func ScanTree(_ context.Context, tree *inventory.Tree, emit func(domain.ScanEvent)) domain.ScanResult {
	stage(emit, domain.ScanStageInventory, false, "")
	via := "walk"
	if tree.ViaGit {
		via = "git"
	}
	inventorySummary := fmt.Sprintf("%s files listed via %s", formatCount(len(tree.Files)), via)
	if tree.Truncated {
		inventorySummary += " (truncated)"
	}
	stage(emit, domain.ScanStageInventory, true, inventorySummary)

	stage(emit, domain.ScanStageShape, false, "")
	comps := components.Detect(tree)
	stage(emit, domain.ScanStageShape, true, shapeSummary(comps))

	stage(emit, domain.ScanStageComponents, false, "")
	stage(emit, domain.ScanStageComponents, true, componentsSummary(comps.Components))

	stage(emit, domain.ScanStageStack, false, "")
	stage(emit, domain.ScanStageStack, true, stackSummary(comps.Components))

	stage(emit, domain.ScanStageChecks, false, "")
	ciResult := ci.Detect(tree, comps.Components)
	stage(emit, domain.ScanStageChecks, true, checksSummary(ciResult.Checks))

	stage(emit, domain.ScanStageLinks, false, "")
	linksResult := links.Detect(tree, comps.Components)
	stage(emit, domain.ScanStageLinks, true, fmt.Sprintf("%d links", len(linksResult.Links)))

	stage(emit, domain.ScanStageDeploy, false, "")
	stage(emit, domain.ScanStageDeploy, true, fmt.Sprintf("%d deploy signals", len(linksResult.DeploySignals)))

	result := domain.ScanResult{
		Shape:         comps.Shape,
		ShapeEvidence: comps.ShapeEvidence,
		FileCount:     len(tree.Files),
		Truncated:     tree.Truncated,
		Languages:     comps.Languages,
		Components:    comps.Components,
		Checks:        ciResult.Checks,
		Links:         linksResult.Links,
		DeploySignals: linksResult.DeploySignals,
	}
	result.Warnings = append(result.Warnings, comps.Warnings...)
	result.Warnings = append(result.Warnings, ciResult.Warnings...)
	result.Warnings = append(result.Warnings, linksResult.Warnings...)
	return result
}

func stage(emit func(domain.ScanEvent), st domain.ScanStage, done bool, summary string) {
	if emit == nil {
		return
	}
	emit(domain.ScanEvent{Stage: st, Done: done, Summary: summary, At: time.Now().UTC()})
}

func shapeSummary(r components.Result) string {
	if r.Shape == domain.RepoShapeMonorepo {
		return fmt.Sprintf("Monorepo: %d components (%s)", len(r.Components), strings.Join(evidenceBaseNames(r.ShapeEvidence), ", "))
	}
	if len(r.Components) == 0 {
		return "Single component"
	}
	return fmt.Sprintf("Single component: %s", r.Components[0].Path)
}

func evidenceBaseNames(ev []domain.SourceEvidence) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range ev {
		b := path.Base(e.Path)
		if seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	return out
}

func componentsSummary(comps []domain.DetectedComponent) string {
	if len(comps) == 0 {
		return "no components"
	}
	parts := make([]string, 0, len(comps))
	for _, c := range comps {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Path, c.Role))
	}
	return strings.Join(parts, ", ")
}

func stackSummary(comps []domain.DetectedComponent) string {
	seen := map[string]bool{}
	var parts []string
	add := func(name, version string) {
		label := name
		if version != "" {
			label += " " + version
		}
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		parts = append(parts, label)
	}
	for _, c := range comps {
		for _, l := range c.Stack.Languages {
			add(l.Name, l.Version)
		}
		for _, f := range c.Stack.Frameworks {
			add(f.Name, f.Version)
		}
	}
	if len(parts) == 0 {
		return "no stack detected"
	}
	if len(parts) > 8 {
		parts = parts[:8]
	}
	return strings.Join(parts, ", ")
}

func checksSummary(checks []domain.DetectedCheck) string {
	workflows := map[string]bool{}
	for _, c := range checks {
		workflows[c.Workflow] = true
	}
	return fmt.Sprintf("%d checks from %d workflows", len(checks), len(workflows))
}

func formatCount(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	rem := len(s) % 3
	if rem == 0 {
		rem = 3
	}
	out := []byte(s[:rem])
	for i := rem; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}
