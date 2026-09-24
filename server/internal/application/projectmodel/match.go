package projectmodel

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// matchCandidate is one other repository's component considered as the
// target of an unresolved link.
type matchCandidate struct {
	Component      domain.Component
	RepositoryID   uuid.UUID
	RepositoryName string
	// SharesProject is true when the candidate's repository and the source
	// repository have at least one project id in common.
	SharesProject bool
}

type matchResult struct {
	Candidate  matchCandidate
	Confidence domain.Confidence
	Reason     string
}

// linkMatcher tries to resolve target against one of candidates; candidates
// already exclude the source repository's own components and dismissed ones.
type linkMatcher func(target domain.LinkTarget, candidates []matchCandidate) (matchResult, bool)

var linkMatchers = []linkMatcher{matchByPackage, matchByName}

// matchLink runs the strategies in order and returns the first hit.
func matchLink(target domain.LinkTarget, candidates []matchCandidate) (matchResult, bool) {
	for _, m := range linkMatchers {
		if res, ok := m(target, candidates); ok {
			return res, true
		}
	}
	return matchResult{}, false
}

func matchByPackage(target domain.LinkTarget, candidates []matchCandidate) (matchResult, bool) {
	pkg := strings.TrimSpace(target.PackageName)
	if pkg == "" {
		return matchResult{}, false
	}
	var pool []matchCandidate
	for _, c := range candidates {
		if c.Component.Stack.Get().PackageName == pkg {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		return matchResult{}, false
	}
	return matchResult{
		Candidate:  bestCandidate(pool),
		Confidence: domain.ConfidenceExact,
		Reason:     fmt.Sprintf("package %s", pkg),
	}, true
}

var serviceNameSuffixes = []string{"-svc", "-service", "-api", "-server", "-backend", "-app"}

func matchByName(target domain.LinkTarget, candidates []matchCandidate) (matchResult, bool) {
	hint := strings.TrimSpace(target.ServiceHint)
	if hint == "" {
		return matchResult{}, false
	}
	hintVariants := nameVariants(hint)
	var pool []matchCandidate
	for _, c := range candidates {
		if variantsIntersect(hintVariants, nameVariants(c.RepositoryName)) ||
			variantsIntersect(hintVariants, nameVariants(c.Component.Name.Get())) ||
			variantsIntersect(hintVariants, nameVariants(path.Base(c.Component.Path))) {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		return matchResult{}, false
	}
	best := bestCandidate(pool)
	reason := fmt.Sprintf("name %s ↔ %s/%s", hint, best.RepositoryName, best.Component.Path)
	if target.Port != 0 && target.Port == best.Component.Stack.Get().DevPort {
		reason += fmt.Sprintf(" and port %d", target.Port)
	}
	return matchResult{Candidate: best, Confidence: domain.ConfidenceMedium, Reason: reason}, true
}

// bestCandidate prefers a repository that shares a project with the source
// repository, then breaks ties by (repository name, path).
func bestCandidate(pool []matchCandidate) matchCandidate {
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].SharesProject != pool[j].SharesProject {
			return pool[i].SharesProject
		}
		if pool[i].RepositoryName != pool[j].RepositoryName {
			return pool[i].RepositoryName < pool[j].RepositoryName
		}
		return pool[i].Component.Path < pool[j].Component.Path
	})
	return pool[0]
}

// nameVariants normalizes s to lowercase [a-z0-9] and, when a known service
// suffix is present, adds the variant with that suffix stripped.
func nameVariants(s string) []string {
	lower := strings.ToLower(strings.TrimSpace(s))
	variants := []string{normalizeAlnum(lower)}
	for _, suf := range serviceNameSuffixes {
		if strings.HasSuffix(lower, suf) {
			variants = append(variants, normalizeAlnum(strings.TrimSuffix(lower, suf)))
		}
	}
	return variants
}

func normalizeAlnum(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func variantsIntersect(a, b []string) bool {
	for _, x := range a {
		if x == "" {
			continue
		}
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
