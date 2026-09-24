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

type ReleaseEvidence struct {
	Deployed bool
}

func (s *Service) releaseEvidence(ctx context.Context, repositoryID, taskID uuid.UUID) (ReleaseEvidence, error) {
	if s.pipelineStore == nil {
		return ReleaseEvidence{}, fmt.Errorf("pipeline ledger unavailable")
	}
	runs, err := s.pipelineStore.ListByTask(ctx, taskID)
	if err != nil {
		return ReleaseEvidence{}, err
	}
	prodMapped := s.deployCategoryMapped(ctx, repositoryID, domain.PipelineCategoryProdDeploy)
	var out ReleaseEvidence
	for _, run := range runs {
		if run.Trigger != domain.PipelineTriggerProdDeploy && run.Trigger != domain.PipelineTriggerPreProdDeploy {
			continue
		}
		if run.Status == domain.PipelineStatusSuccess {
			if run.Trigger == domain.PipelineTriggerProdDeploy || !prodMapped {
				out.Deployed = true
				return out, nil
			}
			continue
		}
	}
	return out, nil
}

func (s *Service) taskIsLive(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) (bool, error) {
	if task.Column == domain.TaskColumnReleased {
		return true, nil
	}
	evidence, err := s.releaseEvidence(ctx, repositoryID, task.ID)
	if err != nil {
		return false, err
	}
	return evidence.Deployed, nil
}

func (s *Service) deployDependencyGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	if s.relations == nil {
		return nil
	}
	rels, err := s.relations.ListBySource(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("deploy-dependency gate could not read relations")
		return fmt.Errorf("%w: its deploy dependencies could not be read (%v) — retry the release",
			domain.ErrDeployDependencyNotReleased, err)
	}

	var blocking []string
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDeployDependsOn {
			continue
		}
		label := rel.TargetKey
		if strings.TrimSpace(label) == "" {
			label = rel.TargetTaskID.String()
		}
		target, terr := s.tasks.Get(ctx, repositoryID, rel.TargetTaskID)
		if terr != nil {

			blocking = append(blocking, label+" (cannot be read from this repository)")
			continue
		}
		live, lerr := s.taskIsLive(ctx, repositoryID, target)
		if lerr != nil {
			blocking = append(blocking, label+" (its deploy history could not be read)")
			continue
		}
		if !live {
			blocking = append(blocking, label)
		}
	}
	if len(blocking) == 0 {
		return nil
	}

	err = fmt.Errorf("%w — %s must deploy after: %s",
		domain.ErrDeployDependencyNotReleased, taskLabel(task), strings.Join(blocking, ", "))
	if s.comments != nil {
		_, _ = s.comments.Create(ctx, domain.TaskComment{
			TaskID:     task.ID,
			AuthorType: "system",
			Content: "Release blocked: this task declares a deploy dependency that is not live in production yet — " +
				strings.Join(blocking, ", ") + ".\n\n" +
				"Release those first (or drop the dependency if the ordering no longer applies), then release this task.",
		})
	}
	log.Warn().Str("task_id", task.ID.String()).Str("repository_id", repositoryID.String()).
		Strs("blocking", blocking).Msg("release blocked: deploy dependency not released")
	return err
}

func (s *Service) deployCategoryMapped(ctx context.Context, repositoryID uuid.UUID, category string) bool {
	if s.pipelineJobs == nil {
		return false
	}
	mappings, err := s.pipelineJobs.ListByRepository(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("lifecycle gate: pipeline mapping lookup failed")
		return true
	}
	for _, m := range mappings {
		if m.Category == category && m.TargetKind == domain.PipelineTargetWorkflow && strings.TrimSpace(m.TargetRef) != "" {
			return true
		}
	}
	return false
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
