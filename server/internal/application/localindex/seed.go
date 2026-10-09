package localindex

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const seededFromBase = "base"

// seedBranch prepares a branch seen for the first time so its pass embeds
// only what differs. The indexer seeds a new branch index from the
// repository's base index itself; this adds the two cases it does not cover.
//
// A base index built with another embedding model is dropped first: no
// search may read it, and the indexer would copy it only to embed every file
// again. And with no usable base (a task checkout is often the only clone of a
// repository on this computer) the most recently indexed branch of the same
// repository is copied instead: the pass then compares file hashes and embeds
// only the files that differ, so a sibling's rows are as good a start as the
// base's.
func (s *Service) seedBranch(ctx context.Context, o *opened, repoID uuid.UUID, branch, dir string) (string, error) {
	if _, err := o.store.GetIndexByProjectBranch(ctx, repoID, branch); err == nil {
		return "", nil
	}
	model, dims, err := s.deps.Embedder.ResolvedEmbedding(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", port.ErrLocalIndexEmbedder, err)
	}
	usable := func(idx domain.WorkspaceIndex) bool {
		return idx.Status == domain.IndexStatusCompleted &&
			!domain.EmbeddingProvenanceStale(idx.EmbeddingModel, idx.EmbeddingDims, model, dims)
	}

	if base, err := o.store.GetIndexByProject(ctx, repoID); err == nil {
		if usable(base) {
			return seededFromBase, nil
		}
		if base.Status == domain.IndexStatusCompleted {
			if err := o.store.DeleteIndex(ctx, base.ID); err != nil {
				return "", fmt.Errorf("drop the base index built with %s: %w", base.EmbeddingModel, err)
			}
			log.Info().Str("index_id", base.ID.String()).Str("built_with", base.EmbeddingModel).Str("now", model).
				Msg("local index: dropped a base index built with another embedding model")
		}
	}

	sibling, ok := newestBranchIndex(ctx, o.store, repoID, usable)
	if !ok {
		return "", nil
	}
	created, err := o.store.CreateProjectBranchIndex(ctx, repoID, branch, dir, "")
	if err != nil {
		return "", fmt.Errorf("create the branch index: %w", err)
	}
	if err := o.store.CopyIndexData(ctx, sibling.ID, created.ID); err != nil {
		_ = o.store.DeleteIndex(ctx, created.ID)
		log.Warn().Err(err).Str("branch", branch).Str("from", sibling.Branch).
			Msg("local index: seeding a branch index from another branch failed; full pass")
		return "", nil
	}
	return "branch:" + sibling.Branch, nil
}

func newestBranchIndex(ctx context.Context, store port.LocalIndexStore, repoID uuid.UUID, usable func(domain.WorkspaceIndex) bool) (domain.WorkspaceIndex, bool) {
	listed, err := store.ListBranchIndexes(ctx)
	if err != nil {
		return domain.WorkspaceIndex{}, false
	}
	var newest domain.WorkspaceIndex
	found := false
	for _, idx := range listed {
		if idx.ProjectID == nil || *idx.ProjectID != repoID || !usable(idx) {
			continue
		}
		if !found || !indexedAt(idx).Before(indexedAt(newest)) {
			newest, found = idx, true
		}
	}
	return newest, found
}

// pruneBranches drops the branch indexes of a repository that were indexed
// longest ago beyond KeepBranches, never the one just built. A branch keeps its
// row across incremental passes, so the row's age says nothing about use.
func (s *Service) pruneBranches(ctx context.Context, o *opened, repoID, keepID uuid.UUID) {
	keep := s.deps.Config.KeepBranches
	if keep <= 0 {
		keep = defaultKeepBranches
	}
	listed, err := o.store.ListBranchIndexes(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("local index: listing branch indexes to prune failed")
		return
	}
	var mine []domain.WorkspaceIndex
	for _, idx := range listed {
		if idx.ProjectID != nil && *idx.ProjectID == repoID {
			mine = append(mine, idx)
		}
	}
	slices.SortStableFunc(mine, func(a, b domain.WorkspaceIndex) int {
		return indexedAt(a).Compare(indexedAt(b))
	})
	for _, idx := range mine[:max(0, len(mine)-keep)] {
		if idx.ID == keepID {
			continue
		}
		if err := o.store.DeleteIndex(ctx, idx.ID); err != nil {
			log.Warn().Err(err).Str("branch", idx.Branch).Msg("local index: dropping an old branch index failed")
		}
	}
}

func indexedAt(idx domain.WorkspaceIndex) time.Time {
	if idx.IndexedAt == nil {
		return time.Time{}
	}
	return *idx.IndexedAt
}
