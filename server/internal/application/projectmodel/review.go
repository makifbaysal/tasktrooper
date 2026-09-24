package projectmodel

import (
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// reviewItems is every medium-confidence value a human hasn't yet looked at:
// a component whose effective role came from a medium-confidence detection
// with no override, a link still waiting to be confirmed, and an environment
// binding MatchScan could not resolve on its own.
func reviewItems(components []domain.Component, links []domain.ComponentLink, environments []domain.ComponentEnvironment) []domain.ReviewItem {
	items := make([]domain.ReviewItem, 0, len(components)+len(links)+len(environments))
	for _, c := range components {
		if c.Status != domain.ComponentStatusActive {
			continue
		}
		if c.Role.Overridden() || c.Role.Confidence != domain.ConfidenceMedium {
			continue
		}
		items = append(items, domain.ReviewItem{
			Kind:         domain.ReviewRole,
			EntityID:     c.ID,
			RepositoryID: c.RepositoryID,
			ComponentID:  c.ID,
			Confidence:   c.Role.Confidence,
		})
	}
	for _, l := range links {
		if l.Status != domain.LinkSuggested {
			continue
		}
		items = append(items, domain.ReviewItem{
			Kind:         domain.ReviewLink,
			EntityID:     l.ID,
			RepositoryID: l.RepositoryID,
			ComponentID:  l.FromComponentID,
			Confidence:   l.Confidence,
		})
	}
	for _, e := range environments {
		if e.Status != domain.LinkSuggested {
			continue
		}
		items = append(items, domain.ReviewItem{
			Kind:         domain.ReviewEnvironment,
			EntityID:     e.ID,
			RepositoryID: e.RepositoryID,
			ComponentID:  e.ComponentID,
			Confidence:   e.Confidence,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].EntityID.String() < items[j].EntityID.String()
	})
	return items
}
