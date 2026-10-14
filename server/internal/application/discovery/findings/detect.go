package findings

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Result struct {
	Findings []domain.DetectedFinding
	// Partial: the read budget ran out before every file was read.
	Partial  bool
	Warnings []string
}

// The read budget keeps a scan of a huge monorepo bounded; a scan that runs
// out reports what it read and says so in a warning.
const (
	maxFilesRead  = 15000
	maxBytesRead  = 96 << 20
	maxFileBytes  = 512 << 10
	maxLineLength = 2000
)

type budget struct {
	files     int
	bytes     int64
	exhausted bool
}

func (b *budget) take(size int64) bool {
	if b.files <= 0 || b.bytes < size {
		b.exhausted = true
		return false
	}
	b.files--
	b.bytes -= size
	return true
}

// unit is one component, or the repository-wide remainder (path "") holding
// the files no component owns.
type unit struct {
	path  string
	comp  *domain.DetectedComponent
	files []fileInfo
}

func (u *unit) role() domain.ComponentRole {
	if u.comp == nil {
		return ""
	}
	return u.comp.Role
}

func Detect(tree *inventory.Tree, comps []domain.DetectedComponent) Result {
	units := assign(tree, comps)
	col := newCollector()
	b := &budget{files: maxFilesRead, bytes: maxBytesRead}
	for _, u := range units {
		newUnitScan(tree, u, col, b).run()
	}
	res := Result{Findings: col.result(), Partial: b.exhausted}
	if b.exhausted {
		res.Warnings = append(res.Warnings, "code health: read budget exhausted, findings cover part of the tree")
	}
	return res
}

func assign(tree *inventory.Tree, comps []domain.DetectedComponent) []*unit {
	sorted := make([]*unit, 0, len(comps)+1)
	for i := range comps {
		sorted = append(sorted, &unit{path: comps[i].Path, comp: &comps[i]})
	}
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i].path) > len(sorted[j].path) })
	rest := &unit{path: ""}
	for _, f := range tree.Files {
		owner := rest
		for _, u := range sorted {
			if contains(u.path, f) {
				owner = u
				break
			}
		}
		owner.files = append(owner.files, classify(f))
	}
	out := append(make([]*unit, 0, len(sorted)+1), sorted...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].path < out[j].path })
	if len(rest.files) > 0 {
		out = append(out, rest)
	}
	return out
}

func contains(componentPath, file string) bool {
	if componentPath == "." || componentPath == "" {
		return true
	}
	return file == componentPath || strings.HasPrefix(file, componentPath+"/")
}

type collector struct {
	order []string
	byKey map[string]*domain.DetectedFinding
}

func newCollector() *collector { return &collector{byKey: map[string]*domain.DetectedFinding{}} }

func findingKey(unitPath, rule string) string { return unitPath + "\x00" + rule }

// add records one occurrence. sev "" is the rule's own severity; a finding
// carries the worst severity any of its occurrences had.
func (c *collector) add(unitPath, rule string, sev domain.FindingSeverity, ev domain.SourceEvidence) {
	def, ok := catalog[rule]
	if !ok {
		return
	}
	if sev == "" {
		sev = def.Severity
	}
	key := findingKey(unitPath, rule)
	f, ok := c.byKey[key]
	if !ok {
		f = &domain.DetectedFinding{ComponentPath: unitPath, Rule: rule, Category: def.Category, Severity: sev}
		c.byKey[key] = f
		c.order = append(c.order, key)
	}
	if sev.Rank() < f.Severity.Rank() {
		f.Severity = sev
	}
	f.Occurrences++
	if len(f.Evidence) < domain.MaxFindingEvidence {
		f.Evidence = append(f.Evidence, ev)
	}
}

func (c *collector) count(unitPath, rule string) int {
	if f, ok := c.byKey[findingKey(unitPath, rule)]; ok {
		return f.Occurrences
	}
	return 0
}

func (c *collector) drop(unitPath, rule string) {
	key := findingKey(unitPath, rule)
	if _, ok := c.byKey[key]; !ok {
		return
	}
	delete(c.byKey, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *collector) result() []domain.DetectedFinding {
	out := make([]domain.DetectedFinding, 0, len(c.order))
	for _, k := range c.order {
		out = append(out, *c.byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ComponentPath != out[j].ComponentPath {
			return out[i].ComponentPath < out[j].ComponentPath
		}
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() < out[j].Severity.Rank()
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}

func snippet(line string) string {
	s := strings.TrimSpace(line)
	if len([]rune(s)) > 160 {
		s = string([]rune(s)[:157]) + "..."
	}
	return s
}

func splitLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}
