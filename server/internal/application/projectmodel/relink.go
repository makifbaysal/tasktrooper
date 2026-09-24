package projectmodel

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Relink re-resolves every scan-detected link that is still open (see
// linkOpenForRematch) and carries a TargetHost, against environments bound
// after the link was created — an environment a human confirms today can
// resolve a link a scan left dangling months ago. It is safe to call
// repeatedly: a link with no open question or no TargetHost is left alone,
// and re-running against the same environments reproduces the same result.
func (s *Service) Relink(ctx context.Context) error {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return fmt.Errorf("relink: list repositories: %w", err)
	}
	if len(repos) == 0 {
		return nil
	}
	repoIDs := make([]uuid.UUID, len(repos))
	repoByID := make(map[uuid.UUID]domain.Repository, len(repos))
	for i, r := range repos {
		repoIDs[i] = r.ID
		repoByID[r.ID] = r
	}

	links, err := s.store.ListLinksForRepositories(ctx, repoIDs)
	if err != nil {
		return fmt.Errorf("relink: list links: %w", err)
	}

	var open []domain.ComponentLink
	for _, l := range links {
		if linkOpenForRematch(l) && l.TargetHost != "" {
			open = append(open, l)
		}
	}
	if len(open) == 0 {
		return nil
	}

	allComponents, err := s.store.ListAllComponents(ctx)
	if err != nil {
		return fmt.Errorf("relink: list components: %w", err)
	}
	componentByID := make(map[uuid.UUID]domain.Component, len(allComponents))
	for _, c := range allComponents {
		componentByID[c.ID] = c
	}

	environments, err := s.listAllEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("relink: list environments: %w", err)
	}

	for _, l := range open {
		from, ok := componentByID[l.FromComponentID]
		if !ok {
			continue
		}
		updated, changed := relinkByEnvironment(l, from, environments, componentByID, repoByID)
		if !changed && !l.Resolved() {
			updated, changed = relinkByLocalDevPort(l, from, allComponents, repoByID)
		}
		if !changed {
			continue
		}
		if _, err := s.store.SaveLink(ctx, updated); err != nil {
			return fmt.Errorf("relink: save link %s: %w", l.ID, err)
		}
	}
	return nil
}

// relinkByEnvironment confirms l when its TargetHost matches exactly one
// confirmed environment of another repository's component: the environment's
// URL host, or (when present) its HealthURL host. Several matches are left
// alone — an ambiguous host is the human's call, not the platform's.
func relinkByEnvironment(l domain.ComponentLink, from domain.Component, environments []domain.ComponentEnvironment, componentByID map[uuid.UUID]domain.Component, repoByID map[uuid.UUID]domain.Repository) (domain.ComponentLink, bool) {
	target := normalizeHost(l.TargetHost)
	if target == "" {
		return l, false
	}

	var matches []domain.ComponentEnvironment
	for _, e := range environments {
		if e.Status != domain.LinkConfirmed {
			continue
		}
		comp, ok := componentByID[e.ComponentID]
		if !ok || comp.RepositoryID == from.RepositoryID {
			continue
		}
		if normalizeHost(hostOf(e.URL)) == target || (e.HealthURL != "" && normalizeHost(hostOf(e.HealthURL)) == target) {
			matches = append(matches, e)
		}
	}
	if len(matches) != 1 {
		return l, false
	}

	env := matches[0]
	comp := componentByID[env.ComponentID]
	repo := repoByID[comp.RepositoryID]

	out := l
	out.ToComponentID = &comp.ID
	out.ToResourceID = nil
	out.Status = domain.LinkConfirmed
	out.AutoConfirmed = true
	out.Confidence = domain.ConfidenceExact
	out.Reason = fmt.Sprintf("URL %s is %s/%s's %s address", l.TargetHost, repo.Name, comp.Path, env.Environment)
	out.Hint = ""
	return out, true
}

// relinkByLocalDevPort is the local-dev fallback: a link pointing at
// localhost/127.0.0.1 on a port that matches exactly one OTHER repository's
// component dev port, within the SAME project, is worth a suggestion — never
// more, since two unrelated repositories can share a dev port by coincidence.
func relinkByLocalDevPort(l domain.ComponentLink, from domain.Component, allComponents []domain.Component, repoByID map[uuid.UUID]domain.Repository) (domain.ComponentLink, bool) {
	host := normalizeHost(l.TargetHost)
	if (host != "localhost" && host != "127.0.0.1") || l.TargetPort == 0 {
		return l, false
	}

	fromProjects := toSet(repoByID[from.RepositoryID].ProjectIDs)
	if len(fromProjects) == 0 {
		return l, false
	}

	var matches []domain.Component
	for _, c := range allComponents {
		if c.RepositoryID == from.RepositoryID || c.Status != domain.ComponentStatusActive {
			continue
		}
		if c.Stack.Get().DevPort != l.TargetPort {
			continue
		}
		repo, ok := repoByID[c.RepositoryID]
		if !ok || !sharesProject(fromProjects, repo.ProjectIDs) {
			continue
		}
		matches = append(matches, c)
	}
	if len(matches) != 1 {
		return l, false
	}

	comp := matches[0]
	repo := repoByID[comp.RepositoryID]

	out := l
	out.ToComponentID = &comp.ID
	out.ToResourceID = nil
	out.Status = domain.LinkSuggested
	out.AutoConfirmed = false
	out.Confidence = domain.ConfidenceMedium
	out.Reason = fmt.Sprintf("localhost:%d is %s/%s's dev port", l.TargetPort, repo.Name, comp.Path)
	out.Hint = ""
	return out, true
}

func sharesProject(a map[uuid.UUID]bool, ids []uuid.UUID) bool {
	for _, id := range ids {
		if a[id] {
			return true
		}
	}
	return false
}

// hostOf reads the hostname out of a URL that may or may not carry a scheme
// ("https://billing.internal:8080" or bare "billing.internal:8080").
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Host == "" {
		u, err = url.Parse("//" + raw)
		if err != nil {
			return ""
		}
	}
	return u.Hostname()
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}
