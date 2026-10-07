package workspace

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/taskkey"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Service manages the single implicit workspace: global board columns,
// board membership, column subscriptions and board settings.
type Service struct {
	board  port.BoardConfigStore
	stages WorkflowStageChecker
}

func NewService(board port.BoardConfigStore) *Service {
	return &Service{board: board}
}

// WorkflowStageChecker is the slice of application/workflow.Service
// UpdateColumns needs to refuse an orphaning column removal. Declared here
// rather than imported so this package does not depend on the workflow
// package (which would need to depend back on a board-columns lister, since
// its own stage validation reads the same column list).
type WorkflowStageChecker interface {
	ColumnHasBehaviourStages(ctx context.Context, slug string) (bool, error)
}

// SetWorkflowStageChecker wires the guard UpdateColumns uses to refuse
// removing a column slug a workflow stage with behaviours still references.
// Nil (the pre-wiring default) leaves UpdateColumns exactly as it behaved
// before workflow stages existed.
func (s *Service) SetWorkflowStageChecker(c WorkflowStageChecker) { s.stages = c }

func (s *Service) GetSettings(ctx context.Context) (domain.BoardSettings, error) {
	return s.board.GetSettings(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, req domain.UpdateBoardSettingsRequest) (domain.BoardSettings, error) {
	normalized := taskkey.NormalizeKeyPrefix(req.KeyPrefix)
	if err := taskkey.ValidateKeyPrefix(normalized); err != nil {
		return domain.BoardSettings{}, err
	}
	return s.board.UpdateSettings(ctx, normalized)
}

func (s *Service) GetConfig(ctx context.Context) (domain.WorkspaceConfig, error) {
	settings, err := s.board.GetSettings(ctx)
	if err != nil {
		return domain.WorkspaceConfig{}, err
	}
	columns, err := s.board.ListColumns(ctx)
	if err != nil {
		return domain.WorkspaceConfig{}, err
	}
	members, err := s.board.ListMembers(ctx)
	if err != nil {
		return domain.WorkspaceConfig{}, err
	}
	subs, err := s.board.ListSubscriptions(ctx)
	if err != nil {
		return domain.WorkspaceConfig{}, err
	}
	transitions, err := s.board.ListTransitions(ctx)
	if err != nil {
		return domain.WorkspaceConfig{}, err
	}
	if columns == nil {
		columns = []domain.BoardColumn{}
	}
	if members == nil {
		members = []domain.BoardMember{}
	}
	if subs == nil {
		subs = []domain.BoardSubscription{}
	}
	if transitions == nil {
		transitions = []domain.BoardTransition{}
	}
	return domain.WorkspaceConfig{
		Settings:      settings,
		Columns:       columns,
		Members:       members,
		Subscriptions: subs,
		Transitions:   transitions,

		DefaultTransitions: domain.DefaultBoardTransitions(),
	}, nil
}

func (s *Service) ListTransitions(ctx context.Context) ([]domain.BoardTransition, error) {
	out, err := s.board.ListTransitions(ctx)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []domain.BoardTransition{}
	}
	return out, nil
}

func (s *Service) SetTransitions(ctx context.Context, req domain.SetBoardTransitionsRequest) error {
	return s.board.SetTransitions(ctx, req.Transitions)
}

// ValidateTransition enforces the workflow: if the source column has any
// outgoing transitions defined, the target must be among them. A source with
// no defined transitions is unrestricted (permissive default).
func (s *Service) ValidateTransition(ctx context.Context, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	transitions, err := s.board.ListTransitions(ctx)
	if err != nil {
		return err
	}
	hasOutgoing := false
	for _, t := range transitions {
		if t.From == from {
			hasOutgoing = true
			if t.To == to {
				return nil
			}
		}
	}
	if hasOutgoing {
		return domain.RefuseMove(errors.New(prompt.Text(transitionNotAllowedKey)))
	}
	return nil
}

func (s *Service) ListColumns(ctx context.Context) ([]domain.BoardColumn, error) {
	return s.board.ListColumns(ctx)
}

func (s *Service) UpdateColumns(ctx context.Context, req domain.UpdateBoardColumnsRequest) error {
	if len(req.Columns) == 0 {
		return fmt.Errorf("columns are required")
	}
	if s.stages != nil {
		existing, err := s.board.ListColumns(ctx)
		if err != nil {
			return err
		}
		next := make(map[string]bool, len(req.Columns))
		for _, c := range req.Columns {
			next[c.Slug] = true
		}
		for _, c := range existing {
			if next[c.Slug] {
				continue
			}
			has, err := s.stages.ColumnHasBehaviourStages(ctx, c.Slug)
			if err != nil {
				return err
			}
			if has {
				return fmt.Errorf("%w: %s", domain.ErrColumnHasWorkflowStages, c.Slug)
			}
		}
	}
	return s.board.ReplaceColumns(ctx, req.Columns)
}

func (s *Service) ListMembers(ctx context.Context) ([]domain.BoardMember, error) {
	return s.board.ListMembers(ctx)
}

func (s *Service) SetMembers(ctx context.Context, req domain.SetBoardMembersRequest) error {
	return s.board.SetMembers(ctx, req.AgentIDs)
}

func (s *Service) ListSubscriptions(ctx context.Context) ([]domain.BoardSubscription, error) {
	return s.board.ListSubscriptions(ctx)
}

func (s *Service) SetSubscriptions(ctx context.Context, req domain.SetBoardSubscriptionsRequest) error {
	return s.board.SetSubscriptions(ctx, req.Subscriptions)
}

func (s *Service) ListAgentSubscriptions(ctx context.Context, agentID uuid.UUID) ([]string, error) {
	slugs, err := s.board.ListAgentSubscriptions(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if slugs == nil {
		slugs = []string{}
	}
	return slugs, nil
}

func (s *Service) SetAgentSubscriptions(ctx context.Context, agentID uuid.UUID, columnSlugs []string) error {
	return s.board.SetAgentSubscriptions(ctx, agentID, columnSlugs)
}

func (s *Service) ListAgentSubscriptionsDetailed(ctx context.Context, agentID uuid.UUID) ([]domain.AgentColumnSubscription, error) {
	return s.board.ListAgentSubscriptionsDetailed(ctx, agentID)
}

func (s *Service) SetAgentSubscriptionsDetailed(ctx context.Context, agentID uuid.UUID, subs []domain.AgentColumnSubscription) error {
	return s.board.SetAgentSubscriptionsDetailed(ctx, agentID, subs)
}

func (s *Service) ListAgentColumnInstructions(ctx context.Context, agentID uuid.UUID) ([]domain.AgentColumnInstruction, error) {
	return s.board.ListAgentColumnInstructions(ctx, agentID)
}

func (s *Service) SetAgentColumnInstructions(ctx context.Context, agentID uuid.UUID, instructions []domain.AgentColumnInstruction) error {
	for _, ins := range instructions {
		if err := s.board.SetAgentColumnInstruction(ctx, agentID, ins.ColumnSlug, ins.Instruction); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) IsMember(ctx context.Context, agentID uuid.UUID) (bool, error) {
	members, err := s.board.ListMembers(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range members {
		if m.AgentID == agentID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) ValidateColumn(ctx context.Context, slug string) error {
	ok, err := s.board.ValidateColumnSlug(ctx, slug)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("invalid column: %s", slug)
	}
	return nil
}

func (s *Service) ListActivity(ctx context.Context, events port.BoardEventStore, runs port.TaskAgentRunStore, limit int) ([]domain.ActivityItem, error) {
	if limit <= 0 {
		limit = 50
	}
	boardEvents, err := events.ListRecent(ctx, limit)
	if err != nil {
		return nil, err
	}
	agentRuns, err := runs.ListRecent(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.ActivityItem, 0, len(boardEvents)+len(agentRuns))
	for _, e := range boardEvents {
		items = append(items, domain.ActivityItem{
			ID:           e.ID,
			Kind:         "board_event",
			RepositoryID: e.RepositoryID,
			TaskID:       e.TaskID,
			EventType:    string(e.EventType),
			Payload:      e.Payload,
			CreatedAt:    e.CreatedAt,
		})
	}
	for _, r := range agentRuns {
		items = append(items, domain.ActivityItem{
			ID:        r.ID,
			Kind:      "agent_run",
			TaskID:    r.TaskID,
			AgentID:   &r.AgentID,
			Status:    r.Status,
			Summary:   r.Summary,
			CreatedAt: r.CreatedAt,
		})
	}
	sortActivityItems(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func sortActivityItems(items []domain.ActivityItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
}
