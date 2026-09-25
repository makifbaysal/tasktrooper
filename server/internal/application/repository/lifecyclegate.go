package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type StageEvidence interface {
	LatestVerdicts(ctx context.Context, taskID uuid.UUID) (map[string]string, error)
}

func (s *Service) SetSpanStore(spans StageEvidence) {
	s.spans = spans
}

func (s *Service) reviewChainGate(ctx context.Context, repo domain.Repository, task domain.BoardTask, prev, target domain.TaskColumn) error {
	if target != domain.TaskColumnDone && target != domain.TaskColumnReleased {
		return nil
	}

	if target == domain.TaskColumnReleased && prev == domain.TaskColumnDone {
		return nil
	}
	wf, err := s.workflow(ctx, task.TaskType)
	if err != nil {

		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review-chain gate could not read the task's workflow")
		return fmt.Errorf("%w: its workflow could not be read (%v) — retry the move", domain.ErrReviewChainIncomplete, err)
	}
	stages := wf.ReviewChain()
	if len(stages) == 0 {
		return nil
	}
	if s.spans == nil {
		return fmt.Errorf("%w: the column-span ledger is not available, so its review history cannot be read. "+
			"Fix the control plane's span store", domain.ErrReviewChainIncomplete)
	}
	verdicts, err := s.spans.LatestVerdicts(ctx, task.ID)
	if err != nil {

		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review-chain gate could not read span history")
		return fmt.Errorf("%w: its stage history could not be read (%v) — retry the move", domain.ErrReviewChainIncomplete, err)
	}

	var missing, rejected []string
	for _, stage := range stages {

		if !s.boardHasColumn(ctx, stage.Column) {
			continue
		}
		verdict, visited := verdicts[string(stage.Column)]
		switch {
		case !visited:
			missing = append(missing, fmt.Sprintf("%s (%s) — %s", stage.Label, stage.Column, stage.Remedy))
		case verdict == domain.ReviewVerdictReject:
			rejected = append(rejected, fmt.Sprintf("%s (%s) — %s", stage.Label, stage.Column, stage.Remedy))
		}
	}

	if len(rejected) > 0 {
		return fmt.Errorf("%w — cannot move %s to %s. Rejected at: %s",
			domain.ErrReviewStageRejected, taskLabel(task), target, strings.Join(rejected, "; "))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w — cannot move %s to %s. Missing: %s",
			domain.ErrReviewChainIncomplete, taskLabel(task), target, strings.Join(missing, "; "))
	}
	return nil
}

func (s *Service) CheckReviewChain(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	if s.repos == nil || s.tasks == nil {
		return fmt.Errorf("repository store unavailable")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return err
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return err
	}

	return s.reviewChainGate(ctx, repo, task, task.Column, domain.TaskColumnDone)
}

func (s *Service) boardHasColumn(ctx context.Context, col domain.TaskColumn) bool {
	if s.columns == nil {
		return domain.ValidTaskColumn(col)
	}
	return s.columns.ValidateColumn(ctx, string(col)) == nil
}

func taskLabel(task domain.BoardTask) string {
	if strings.TrimSpace(task.Key) != "" {
		return task.Key
	}
	return task.ID.String()
}
