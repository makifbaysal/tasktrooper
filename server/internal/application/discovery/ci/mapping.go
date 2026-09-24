package ci

import (
	"path"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type componentMatch struct {
	Component  domain.DetectedComponent
	Confidence domain.Confidence
	// FanOut marks the last-resort "every component" assignment, which only a
	// verification job may keep.
	FanOut bool
}

// mapJob resolves the component(s) a job verifies. Every explicit dir/path/
// filter candidate is resolved independently (a job can legitimately touch
// several components); only when none resolves does the fallback ladder run.
func mapJob(wf workflow, j job, components []domain.DetectedComponent, jc jobCommands) []componentMatch {
	dirs := map[string]bool{}
	addDir := func(d string) {
		if d != "" {
			dirs[cleanPath(d)] = true
		}
	}
	for _, d := range jc.DirHints {
		addDir(d)
	}
	addDir(j.WorkingDir)
	addDir(wf.WorkingDir)
	for _, s := range j.Steps {
		addDir(s.WorkingDir)
		if v := s.With["working-directory"]; v != "" {
			addDir(v)
		}
		if v := s.With["context"]; v != "" {
			addDir(v)
		}
		if v := s.With["cache-dependency-path"]; v != "" {
			for _, line := range strings.Fields(v) {
				addDir(path.Dir(cleanPath(line)))
			}
		}
	}
	for _, pf := range wf.PathFilters {
		if d, ok := dirFromPathFilter(pf); ok {
			addDir(d)
		}
	}

	names := map[string]bool{}
	for _, n := range jc.NameHints {
		if n != "" {
			names[n] = true
		}
	}

	resolved := map[string]componentMatch{}
	for _, d := range sortedKeys(dirs) {
		if c, ok := deepestForDir(components, d); ok {
			resolved[c.Path] = componentMatch{Component: c, Confidence: domain.ConfidenceHigh}
		}
	}
	for _, n := range sortedKeys(names) {
		if c, ok := findByPackageName(components, n); ok {
			resolved[c.Path] = componentMatch{Component: c, Confidence: domain.ConfidenceHigh}
		}
	}
	if len(resolved) > 0 {
		return sortedMatches(resolved)
	}

	if root, ok := findRoot(components); ok {
		return []componentMatch{{Component: root, Confidence: domain.ConfidenceMedium}}
	}
	if len(components) == 1 {
		return []componentMatch{{Component: components[0], Confidence: domain.ConfidenceMedium}}
	}
	if c, ok := matchByJobNameBase(components, j.Key, j.Name); ok {
		return []componentMatch{{Component: c, Confidence: domain.ConfidenceMedium}}
	}
	if len(components) == 0 {
		return nil
	}
	all := make([]componentMatch, 0, len(components))
	for _, c := range components {
		all = append(all, componentMatch{Component: c, Confidence: domain.ConfidenceMedium, FanOut: true})
	}
	sort.Slice(all, func(i, k int) bool { return all[i].Component.Path < all[k].Component.Path })
	return all
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedMatches(m map[string]componentMatch) []componentMatch {
	out := make([]componentMatch, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Component.Path < out[k].Component.Path })
	return out
}

// pathContains mirrors domain.Component.Contains for a DetectedComponent: the
// root component ("." ) contains everything, a non-root one only its subtree.
func pathContains(componentPath, dir string) bool {
	if componentPath == "." {
		return true
	}
	return dir == componentPath || strings.HasPrefix(dir, componentPath+"/")
}

// deepestForDir mirrors domain.OwningComponent: the deepest component whose
// path contains dir, root only winning when nothing deeper claims it.
func deepestForDir(components []domain.DetectedComponent, dir string) (domain.DetectedComponent, bool) {
	var best domain.DetectedComponent
	found := false
	for _, c := range components {
		if !pathContains(c.Path, dir) {
			continue
		}
		if !found || best.Path == "." || len(c.Path) > len(best.Path) {
			best, found = c, true
		}
	}
	return best, found
}

func findRoot(components []domain.DetectedComponent) (domain.DetectedComponent, bool) {
	for _, c := range components {
		if c.Path == "." {
			return c, true
		}
	}
	return domain.DetectedComponent{}, false
}

func findByPackageName(components []domain.DetectedComponent, name string) (domain.DetectedComponent, bool) {
	for _, c := range components {
		if c.PackageName != "" && c.PackageName == name {
			return c, true
		}
	}
	return domain.DetectedComponent{}, false
}

func matchByJobNameBase(components []domain.DetectedComponent, key, name string) (domain.DetectedComponent, bool) {
	hay := strings.ToLower(key + " " + name)
	var best domain.DetectedComponent
	found := false
	for _, c := range components {
		if c.Path == "." {
			continue
		}
		base := strings.ToLower(path.Base(c.Path))
		if base == "" || !strings.Contains(hay, base) {
			continue
		}
		if !found || len(c.Path) > len(best.Path) {
			best, found = c, true
		}
	}
	return best, found
}

// dirFromPathFilter reduces a glob path filter to the directory before its
// first wildcard segment: "services/worker/**" -> "services/worker".
func dirFromPathFilter(pf string) (string, bool) {
	pf = strings.TrimSpace(pf)
	if pf == "" || !strings.Contains(pf, "*") {
		return "", false
	}
	var kept []string
	for _, p := range strings.Split(pf, "/") {
		if strings.Contains(p, "*") {
			break
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return ".", true
	}
	return cleanPath(strings.Join(kept, "/")), true
}

// assignCommands splits one job's LocalCommands across the components it was
// mapped to. A single-component job keeps every command; a multi-component
// job gives each component only the commands scoped inside it, falling back
// to the root-dir ("." ) commands when it has none of its own.
func assignCommands(matches []componentMatch, commands []domain.LocalCommand) map[string][]domain.LocalCommand {
	out := map[string][]domain.LocalCommand{}
	if len(matches) == 1 {
		out[matches[0].Component.Path] = dedupeCommands(commands)
		return out
	}
	for _, m := range matches {
		var inside []domain.LocalCommand
		for _, cmd := range commands {
			if cmd.Dir != "." && pathContains(m.Component.Path, cmd.Dir) {
				inside = append(inside, cmd)
			}
		}
		if len(inside) == 0 {
			for _, cmd := range commands {
				if cmd.Dir == "." {
					inside = append(inside, cmd)
				}
			}
		}
		out[m.Component.Path] = dedupeCommands(inside)
	}
	return out
}

func dedupeCommands(cmds []domain.LocalCommand) []domain.LocalCommand {
	if len(cmds) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []domain.LocalCommand
	for _, c := range cmds {
		key := c.Dir + "\x00" + strings.Join(c.Argv, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}
