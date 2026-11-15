package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const qaRoleKey = "qa"

type roleByKey interface {
	RoleByKey(ctx context.Context, key string) (domain.AgentRole, error)
}

// setAutomationTests switches the repository's automation suite on or off;
// the off→on edge opens the backfill task that writes the tests the
// repository is still missing.
func (s *Service) setAutomationTests(ctx context.Context, before domain.Repository, enabled bool) (domain.Repository, error) {
	repo, err := s.repos.UpdateAutomationTests(ctx, before.ID, enabled)
	if err != nil {
		return domain.Repository{}, err
	}
	if !enabled || before.AutomationTests {
		return repo, nil
	}
	task, created, err := s.CreateAutomationBackfillTask(ctx, repo.ID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("automation backfill task could not be opened")
		return repo, nil
	}
	if created {
		repo.AutomationTaskID = task.ID.String()
	}
	return repo, nil
}

// CreateAutomationBackfillTask opens the one task that builds the
// repository's qa-automation/ project and covers what already ships; while it
// is still open on the board it is returned instead of a second one.
func (s *Service) CreateAutomationBackfillTask(ctx context.Context, repositoryID uuid.UUID) (domain.BoardTask, bool, error) {
	s.ciSetupMu.Lock()
	defer s.ciSetupMu.Unlock()

	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if open, ok, err := s.openTaskByID(ctx, repo.ID, repo.AutomationTaskID); err != nil || ok {
		return open, false, err
	}
	task, err := s.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:              prompt.Text(automationBackfillTitleKey),
		Description:        automationBackfillBriefKey.Render(automationBackfillBriefInput{Kind: repo.Kind}),
		Priority:           domain.TaskPriorityMedium,
		Column:             domain.TaskColumnTodo,
		CreatedBy:          "system",
		AssigneeAgentID:    s.qaAgent(ctx, repo),
		AcceptanceCriteria: automationBackfillCriteria(),
	})
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if err := s.repos.SetAutomationTaskID(ctx, repositoryID, task.ID.String()); err != nil {
		return domain.BoardTask{}, false, err
	}
	return task, true, nil
}

func automationBackfillCriteria() []domain.AcceptanceCriterionInput {
	lines := strings.Split(strings.TrimSpace(prompt.Text(automationBackfillCriteriaKey)), "\n")
	out := make([]domain.AcceptanceCriterionInput, 0, len(lines))
	for i, line := range lines {
		if text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-")); text != "" {
			out = append(out, domain.AcceptanceCriterionInput{Text: text, Position: i})
		}
	}
	return out
}

// qaAgent is the agent holding the qa role for the repository's area, falling
// back to the system task assignee when no one holds it.
func (s *Service) qaAgent(ctx context.Context, repo domain.Repository) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	area := domain.RepoArea(repo.Kind, repo.SubProjects)
	if byKey, ok := s.roles.(roleByKey); ok {
		if role, err := byKey.RoleByKey(ctx, qaRoleKey); err == nil {
			if agent, err := s.roles.AgentForRole(ctx, role.ID, area); err == nil && agent != nil {
				return agent
			}
		}
	}
	if agent, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, area); err == nil {
		return agent
	}
	return nil
}

// openTaskByID: a deleted task, or one that reached done/released, no longer
// holds the slot.
func (s *Service) openTaskByID(ctx context.Context, repositoryID uuid.UUID, rawID string) (domain.BoardTask, bool, error) {
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return domain.BoardTask{}, false, nil
	}
	task, err := s.tasks.Get(ctx, repositoryID, id)
	if errors.Is(err, domain.ErrBoardTaskNotFound) || errors.Is(err, port.ErrNotFound) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if task.Column == domain.TaskColumnDone || task.Column == domain.TaskColumnReleased {
		return domain.BoardTask{}, false, nil
	}
	return task, true, nil
}

// automationGate holds QA's own forward move on a repository with automation
// tests on: the board runs the suite after the run and makes the move itself
// once it is green. A person moving the card is never held.
func (s *Service) automationGate(ctx context.Context, repo domain.Repository, taskType domain.TaskType, prev, target domain.TaskColumn, actor domain.TaskActor) error {
	if !repo.AutomationTests || actor != domain.TaskActorAgent || prev == target {
		return nil
	}
	wf, err := s.workflow(ctx, taskType)
	if err != nil {
		return nil
	}
	if !wf.Has(prev, domain.BehaviourRequireExecutionEvidence) || !wf.Has(target, domain.BehaviourForwardExit) {
		return nil
	}
	return domain.RefuseMove(errors.New(automationMoveHeldKey.Render(testCasesMissingInput{Target: string(target)})))
}
