package board

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// RepoAreaFunc resolves a repository's role area (domain.RepoArea); "" when
// the repository cannot be read.
type RepoAreaFunc func(ctx context.Context, repositoryID uuid.UUID) string

type repositoryGetter interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

func RepoAreaFromStore(repos repositoryGetter) RepoAreaFunc {
	return func(ctx context.Context, repositoryID uuid.UUID) string {
		repo, err := repos.Get(ctx, repositoryID)
		if err != nil {
			return ""
		}
		return domain.RepoArea(repo.Kind, repo.SubProjects)
	}
}

// subscribersForTaskArea narrows an unassigned task's column subscribers to
// the ones whose role areas cover the task's repository, those first: every developer
// watches todo and need_revision, and a chat on a frontend card must not land
// on whichever of them the store lists first. Area-less subscribers are kept;
// with nobody covering the area, the lookup walks AreaFallbacks and then
// RoleAreas' order, so it still settles on one area. A quorum review column is
// never narrowed: there every subscriber is a required reviewer.
func subscribersForTaskArea(ctx context.Context, roles port.RoleResolver, repoArea RepoAreaFunc, task domain.BoardTask, ids []uuid.UUID) []uuid.UUID {
	if roles == nil || len(ids) < 2 || domain.QuorumReviewColumn(task.Column) {
		return ids
	}
	area := ""
	if repoArea != nil {
		area = repoArea(ctx, task.RepositoryID)
	}
	areasOf := make(map[uuid.UUID][]string, len(ids))
	for _, id := range ids {
		areasOf[id] = roles.AgentAreas(ctx, id)
	}
	wants := append(append([]string{area}, domain.AreaFallbacks(area)...), domain.RoleAreas()...)
	for _, want := range wants {
		if want == "" {
			continue
		}
		var covering, arealess []uuid.UUID
		for _, id := range ids {
			switch areas := areasOf[id]; {
			case len(areas) == 0:
				arealess = append(arealess, id)
			case slices.Contains(areas, want):
				covering = append(covering, id)
			}
		}
		if len(covering) > 0 {
			return append(covering, arealess...)
		}
	}
	return ids
}
