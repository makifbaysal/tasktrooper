package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (s *Service) SetTestCaseStore(store port.TaskTestCaseStore) {
	s.testCases = store
}

func (s *Service) ListTestCases(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error) {
	if s.testCases == nil {
		return nil, fmt.Errorf("test cases not enabled")
	}
	items, err := s.testCases.ListByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.TaskTestCase{}
	}
	return items, nil
}

func (s *Service) RecordTestCases(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	if s.testCases == nil {
		return nil, fmt.Errorf("test cases not enabled")
	}
	normalized, err := normalizeTestCases(items)
	if err != nil {
		return nil, err
	}
	if err := s.validateCriterionLinks(ctx, taskID, normalized); err != nil {
		return nil, err
	}
	return s.testCases.UpsertForTask(ctx, taskID, normalized)
}

func (s *Service) ReplaceTestCases(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.testCases == nil {
		return nil, fmt.Errorf("test cases not enabled")
	}
	normalized, err := normalizeTestCases(items)
	if err != nil {
		return nil, err
	}
	if err := s.validateCriterionLinks(ctx, taskID, normalized); err != nil {
		return nil, err
	}
	return s.testCases.ReplaceForTask(ctx, taskID, normalized)
}

func (s *Service) UpdateTestCase(ctx context.Context, repositoryID, taskID, testCaseID uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskTestCase{}, err
	}
	if s.testCases == nil {
		return domain.TaskTestCase{}, fmt.Errorf("test cases not enabled")
	}
	existing, err := s.testCases.Get(ctx, testCaseID)
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	if existing.TaskID != taskID {
		return domain.TaskTestCase{}, fmt.Errorf("test case %s does not belong to task %s", testCaseID, taskID)
	}

	normalized, err := mergeTestCase(existing, item).Normalize()
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	return s.testCases.Update(ctx, testCaseID, normalized)
}

func (s *Service) SetTestCaseResult(ctx context.Context, testCaseID uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	if s.testCases == nil {
		return domain.TaskTestCase{}, fmt.Errorf("test cases not enabled")
	}
	existing, err := s.testCases.Get(ctx, testCaseID)
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	normalized, err := mergeTestCase(existing, item).Normalize()
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	return s.testCases.Update(ctx, testCaseID, normalized)
}

func (s *Service) DeleteTestCase(ctx context.Context, repositoryID, taskID, testCaseID uuid.UUID) error {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return err
	}
	if s.testCases == nil {
		return fmt.Errorf("test cases not enabled")
	}
	existing, err := s.testCases.Get(ctx, testCaseID)
	if err != nil {
		return err
	}
	if existing.TaskID != taskID {
		return fmt.Errorf("test case %s does not belong to task %s", testCaseID, taskID)
	}
	return s.testCases.Delete(ctx, testCaseID)
}

func normalizeTestCases(items []domain.TaskTestCaseInput) ([]domain.TaskTestCaseInput, error) {
	out := make([]domain.TaskTestCaseInput, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		normalized, err := item.Normalize()
		if err != nil {
			return nil, err
		}

		key := strings.ToLower(normalized.Title)
		if _, dup := seen[key]; dup {
			return nil, errors.New(testCaseDuplicateTitleKey.Render(testCaseDuplicateTitleInput{Title: fmt.Sprintf("%q", normalized.Title)}))
		}
		seen[key] = struct{}{}
		if normalized.Position == 0 {
			normalized.Position = i + 1
		}
		out = append(out, normalized)
	}
	return out, nil
}

func (s *Service) validateCriterionLinks(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) error {
	if s.criteria == nil {
		return nil
	}
	var needed bool
	for _, item := range items {
		if item.CriterionID != nil {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	criteria, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil {
		return err
	}
	own := make(map[uuid.UUID]struct{}, len(criteria))
	for _, c := range criteria {
		own[c.ID] = struct{}{}
	}
	for _, item := range items {
		if item.CriterionID == nil {
			continue
		}
		if _, ok := own[*item.CriterionID]; !ok {
			return errors.New(testCaseCriterionNotOnTaskKey.Render(testCaseCriterionNotOnTaskInput{CriterionID: item.CriterionID.String()}))
		}
	}
	return nil
}

func mergeTestCase(existing domain.TaskTestCase, in domain.TaskTestCaseInput) domain.TaskTestCaseInput {
	out := domain.TaskTestCaseInput{
		CriterionID: existing.CriterionID,
		Title:       existing.Title,
		Category:    existing.Category,
		Status:      existing.Status,
		Expected:    existing.Expected,
		Actual:      existing.Actual,
		Evidence:    existing.Evidence,
		Notes:       existing.Notes,
		Position:    existing.Position,
	}
	if in.CriterionID != nil {
		out.CriterionID = in.CriterionID
	}
	if strings.TrimSpace(in.Title) != "" {
		out.Title = in.Title
	}
	if in.Category != "" {
		out.Category = in.Category
	}
	if in.Status != "" {
		out.Status = in.Status
	}
	if strings.TrimSpace(in.Expected) != "" {
		out.Expected = in.Expected
	}
	if strings.TrimSpace(in.Actual) != "" {
		out.Actual = in.Actual
	}
	if strings.TrimSpace(in.Evidence) != "" {
		out.Evidence = in.Evidence
	}
	if strings.TrimSpace(in.Notes) != "" {
		out.Notes = in.Notes
	}
	if in.Position != 0 {
		out.Position = in.Position
	}
	return out
}

// An agent's need_revision exit is gated too: a QA round that bounces the task
// without having run its cases sends the developer nothing to fix. A human
// dragging the card back is never held.
func (s *Service) testCaseGate(ctx context.Context, taskID uuid.UUID, taskType domain.TaskType, prev, target domain.TaskColumn, actor domain.TaskActor) error {
	if !s.requireCriteria || s.testCases == nil {
		return nil
	}
	wf, err := s.workflow(ctx, taskType)
	if err != nil {
		return fmt.Errorf("test case gate: workflow unavailable for %s (%w)", target, err)
	}
	if !wf.Has(prev, domain.BehaviourRequireTestCases) {
		return nil
	}
	revision := target == domain.TaskColumnNeedRevision && actor == domain.TaskActorAgent
	if !revision && !wf.Has(target, domain.BehaviourForwardExit) {
		return nil
	}
	items, err := s.testCases.ListByTask(ctx, taskID)
	if err != nil {
		return nil
	}
	if len(items) == 0 {
		return domain.RefuseMove(fmt.Errorf("%s", testCasesMissingKey.Render(testCasesMissingInput{Target: string(target)})))
	}
	var planned []string
	failed := false
	for _, c := range items {
		switch c.Status {
		case domain.TestCaseStatusPlanned:
			planned = append(planned, c.Title)
		case domain.TestCaseStatusFailed:
			failed = true
		}
	}
	if len(planned) > 0 {
		return domain.RefuseMove(fmt.Errorf("%s", testCasesPlannedKey.Render(testCasesPlannedInput{Target: string(target), Count: len(planned), Planned: planned})))
	}
	if revision && !failed && !s.hasRejectedCriterion(ctx, wf, prev, taskID) {
		return domain.RefuseMove(fmt.Errorf("%s", testCasesNoFailureKey.Render(testCasesMissingInput{Target: string(target)})))
	}
	return nil
}

func (s *Service) hasRejectedCriterion(ctx context.Context, wf domain.Workflow, stage domain.TaskColumn, taskID uuid.UUID) bool {
	channel, ok := wf.Param(stage, domain.BehaviourCriterionVerdict, "channel")
	if !ok || s.criteria == nil {
		return false
	}
	items, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil {
		return true
	}
	for _, c := range items {
		if c.Canceled {
			continue
		}
		if v := criterionCheckFor(c, domain.CriterionReviewRole(channel)); v != nil && !v.Approved {
			return true
		}
	}
	return false
}
