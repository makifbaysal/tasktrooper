package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const relationWalkLimit = 512

func (s *Service) SetBlockers(ctx context.Context, taskID uuid.UUID, blockers []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	if s.relations == nil {
		return nil, fmt.Errorf("relation store unavailable")
	}
	resolved, err := s.resolveRelationTargets(ctx, blockers, domain.TaskRelationBlocks)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(resolved))
	seen := make(map[uuid.UUID]bool, len(resolved))
	for _, rel := range resolved {
		if rel.TargetTaskID == taskID {
			return nil, fmt.Errorf("a task cannot be blocked by itself")
		}
		if seen[rel.TargetTaskID] {
			continue
		}
		seen[rel.TargetTaskID] = true
		ids = append(ids, rel.TargetTaskID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	for _, blockerID := range ids {
		if err := s.guardWorkOrderCycle(ctx, taskID, blockerID); err != nil {
			return nil, err
		}
	}
	return s.relations.AddBlockers(ctx, taskID, ids)
}

func (s *Service) resolveRelationTargets(ctx context.Context, inputs []domain.TaskRelationInput, relType domain.TaskRelationType) ([]domain.TaskRelationInput, error) {
	typed := make([]domain.TaskRelationInput, 0, len(inputs))
	for _, in := range inputs {
		in.RelationType = relType
		typed = append(typed, in)
	}
	return s.resolveRelations(ctx, typed)
}

func (s *Service) guardWorkOrderCycle(ctx context.Context, taskID, blockerID uuid.UUID) error {
	path, err := s.relationPath(ctx, taskID, blockerID, domain.TaskRelationBlocks)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return nil
	}
	return errors.New(workOrderCycleKey.Render(workOrderCycleInput{
		Task: s.taskLabelByID(ctx, taskID), Blocker: s.taskLabelByID(ctx, blockerID), Path: strings.Join(path, " → "),
	}))
}

func (s *Service) guardDeployOrderCycle(ctx context.Context, taskID, dependencyID uuid.UUID) error {
	path, err := s.relationPath(ctx, dependencyID, taskID, domain.TaskRelationDeployDependsOn)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return nil
	}
	return errors.New(deployOrderCycleKey.Render(deployOrderCycleInput{
		Dependency: s.taskLabelByID(ctx, dependencyID), Task: s.taskLabelByID(ctx, taskID), Path: strings.Join(path, " → "),
	}))
}

func (s *Service) relationPath(ctx context.Context, from, to uuid.UUID, relType domain.TaskRelationType) ([]string, error) {
	if s.relations == nil || from == uuid.Nil || to == uuid.Nil {
		return nil, nil
	}
	if from == to {
		return []string{s.taskLabelByID(ctx, from)}, nil
	}
	type step struct {
		id   uuid.UUID
		path []string
	}
	visited := map[uuid.UUID]bool{from: true}
	stack := []step{{id: from, path: []string{s.taskLabelByID(ctx, from)}}}
	for visits := 0; len(stack) > 0 && visits < relationWalkLimit; visits++ {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		rels, err := s.relations.ListBySource(ctx, cur.id)
		if err != nil {

			return nil, fmt.Errorf("relation graph could not be read: %w", err)
		}
		for _, rel := range rels {
			if rel.RelationType != relType {
				continue
			}
			label := domain.RelationLabel(rel.TargetKey, rel.TargetTitle, rel.TargetTaskID)
			if rel.TargetTaskID == to {
				return append(append([]string{}, cur.path...), label), nil
			}
			if visited[rel.TargetTaskID] {
				continue
			}
			visited[rel.TargetTaskID] = true
			stack = append(stack, step{id: rel.TargetTaskID, path: append(append([]string{}, cur.path...), label)})
		}
	}
	return nil, nil
}

func (s *Service) taskLabelByID(ctx context.Context, id uuid.UUID) string {
	if id == uuid.Nil {
		return "(unknown task)"
	}
	repositoryID, err := s.FindTaskRepositoryID(ctx, id)
	if err != nil {
		return id.String()
	}
	task, err := s.tasks.Get(ctx, repositoryID, id)
	if err != nil {
		return id.String()
	}
	return domain.RelationLabel(task.Key, task.Title, id)
}

func (s *Service) syncOrderNote(ctx context.Context, task domain.BoardTask) domain.BoardTask {
	if s.relations == nil {
		return task
	}
	deployAfter, workAfter, err := s.orderLabels(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("order note: reading relations failed")
		return task
	}
	note := renderOrderNote(deployAfter, workAfter)
	updatedText := domain.ApplyOrderNote(trimmedPtr(task.BeforeDeploy), note)
	if updatedText == trimmedPtr(task.BeforeDeploy) {
		return task
	}
	var before *string
	if updatedText != "" {
		before = &updatedText
	}
	stored, err := s.tasks.Update(ctx, withBeforeDeploy(task, before))
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("order note: writing before_deploy failed")
		return task
	}

	task.BeforeDeploy = stored.BeforeDeploy
	task.UpdatedAt = stored.UpdatedAt
	return task
}

func withBeforeDeploy(task domain.BoardTask, before *string) domain.BoardTask {
	task.BeforeDeploy = before
	return task
}

func (s *Service) orderLabels(ctx context.Context, taskID uuid.UUID) (deployAfter, workAfter []string, err error) {
	rels, err := s.relations.ListBySource(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDeployDependsOn {
			continue
		}
		deployAfter = append(deployAfter, domain.RelationLabel(rel.TargetKey, rel.TargetTitle, rel.TargetTaskID))
	}
	blockers, err := s.relations.ListBlockedBy(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	for _, rel := range blockers {
		workAfter = append(workAfter, domain.RelationLabel(rel.SourceKey, rel.SourceTitle, rel.SourceTaskID))
	}
	return deployAfter, workAfter, nil
}

func (s *Service) AnalysisReferences(ctx context.Context, taskID uuid.UUID) ([]domain.AnalysisReference, error) {
	if s.relations == nil || s.documents == nil {
		return nil, nil
	}
	rels, err := s.relations.ListBySource(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var out []domain.AnalysisReference
	seen := map[uuid.UUID]bool{}
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDerivedFrom || seen[rel.TargetTaskID] {
			continue
		}
		ref := domain.AnalysisReference{
			TaskID: rel.TargetTaskID,
			Key:    rel.TargetKey,
			Title:  rel.TargetTitle,
		}
		if s.tasks != nil {
			if target, terr := s.tasks.GetByID(ctx, rel.TargetTaskID); terr == nil {
				ref.TaskType, ref.Column, ref.RepositoryID = target.TaskType, target.Column, target.RepositoryID
			}
		}
		docs, derr := s.documents.ListByTask(ctx, rel.TargetTaskID)
		if derr != nil {
			log.Warn().Err(derr).Str("analysis_task_id", rel.TargetTaskID.String()).
				Msg("analysis reference: reading documents failed")
			continue
		}
		ref.Documents = docs
		if ref.IsDesign() {
			ref.Documents = s.chosenDesignDocs(ctx, ref.TaskID, docs)
		}
		seen[rel.TargetTaskID] = true
		out = append(out, ref)
	}
	designs, err := s.approvedDesignBlockers(ctx, taskID, seen)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("design reference: reading blockers failed")
	}
	return append(out, designs...), nil
}

// approvedDesignBlockers are the approved design tasks that block taskID: a
// screen designed before it was built hands its documents to the work it
// blocks without a derived_from relation, which an implementation task opened
// before the design (or in another repository) cannot be given afterwards.
func (s *Service) approvedDesignBlockers(ctx context.Context, taskID uuid.UUID, seen map[uuid.UUID]bool) ([]domain.AnalysisReference, error) {
	if s.tasks == nil {
		return nil, nil
	}
	blockers, err := s.relations.ListBlockedBy(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var out []domain.AnalysisReference
	for _, rel := range blockers {
		if seen[rel.SourceTaskID] {
			continue
		}
		source, err := s.tasks.GetByID(ctx, rel.SourceTaskID)
		if err != nil || source.TaskType != domain.TaskTypeDesign {
			continue
		}
		if source.Column != domain.TaskColumnDone && source.Column != domain.TaskColumnReleased {
			continue
		}
		docs, err := s.documents.ListByTask(ctx, source.ID)
		if err != nil {
			log.Warn().Err(err).Str("design_task_id", source.ID.String()).Msg("design reference: reading documents failed")
			continue
		}
		seen[source.ID] = true
		out = append(out, domain.AnalysisReference{
			TaskID: source.ID, Key: source.Key, Title: source.Title, Documents: s.chosenDesignDocs(ctx, source.ID, docs),
			TaskType: source.TaskType, Column: source.Column, RepositoryID: source.RepositoryID,
		})
	}
	return out, nil
}

// chosenDesignDocs keeps an approved design's documents minus the variants the
// human did not choose, so nobody builds a variant that lost.
func (s *Service) chosenDesignDocs(ctx context.Context, designTaskID uuid.UUID, docs []domain.TaskDocument) []domain.TaskDocument {
	if s.comments == nil {
		return docs
	}
	comments, err := s.comments.ListByTask(ctx, designTaskID)
	if err != nil {
		return docs
	}
	return domain.WithoutUnchosenVariants(docs, domain.ChosenVariants(comments))
}
