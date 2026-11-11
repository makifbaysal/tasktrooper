package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/reviewhistory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const reviewDiffTimeout = 20 * time.Second

// numstatGit is the optional half of port.GitClient that counts a diff's
// lines per file; reached with a type-assert for the reason patchIDGit is.
type numstatGit interface {
	TaskNumstat(ctx context.Context, workspacePath string) ([]port.GitFileStat, error)
}

// SetReviewHistory wires the review history in. The service reads the
// checkout through this repository service, so the closures are set here.
func (s *Service) SetReviewHistory(h *reviewhistory.Service) {
	if h != nil {
		h.HeadSHA = func(ctx context.Context, taskID uuid.UUID) string { return s.currentTaskSHA(ctx, taskID) }
		h.Diff = s.reviewDiff
	}
	s.reviewHistory = h
}

// reviewDiff is the pull request's changed files with line counts, and the
// stat-plus-patch text a review run is handed; both empty when this host has
// no checkout of the task.
func (s *Service) reviewDiff(ctx context.Context, taskID uuid.UUID) ([]reviewhistory.DiffFile, string) {
	if s.git == nil {
		return nil, ""
	}
	path := s.taskWorkspacePath(taskID)
	if path == "" || !s.git.HasGit(path) {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(ctx, reviewDiffTimeout)
	defer cancel()
	patch, err := s.git.TaskDiff(ctx, path)
	if err != nil {
		return nil, ""
	}
	var files []reviewhistory.DiffFile
	if ng, ok := s.git.(numstatGit); ok {
		stats, err := ng.TaskNumstat(ctx, path)
		if err != nil {
			return nil, ""
		}
		for _, st := range stats {
			files = append(files, reviewhistory.DiffFile{Path: st.Path, Added: st.Added, Deleted: st.Deleted, Binary: st.Binary})
		}
	}
	return files, patch
}

// ReviewHistory is GET .../review-history.
func (s *Service) ReviewHistory(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.ReviewHistory, error) {
	return s.reviewHistory.History(ctx, repositoryID, taskID)
}

// RecordReviewReport is record_review_findings' write path.
func (s *Service) RecordReviewReport(ctx context.Context, repositoryID, taskID, agentID uuid.UUID, report domain.ReviewReportInput) (int, error) {
	return s.reviewHistory.RecordReport(ctx, repositoryID, taskID, agentID, report)
}

// MarkReviewFlagReviewed is POST .../review-flags/:flagId/reviewed.
func (s *Service) MarkReviewFlagReviewed(ctx context.Context, repositoryID, taskID uuid.UUID, flagKey, by string) (domain.ReviewFlag, error) {
	return s.reviewHistory.MarkReviewed(ctx, repositoryID, taskID, flagKey, by)
}

// OpenReviewFlags is how many flags on the task nobody has looked at yet.
func (s *Service) OpenReviewFlags(ctx context.Context, repositoryID, taskID uuid.UUID) int {
	return s.reviewHistory.OpenFlagCount(ctx, repositoryID, taskID)
}
