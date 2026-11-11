// Package workflow is roles, task types and per-type workflow stages as
// data. Service is the single implementation of both admin CRUD (backed by
// port.RoleStore/port.WorkflowStore) and the two read interfaces the engine's
// hot path consults (port.WorkflowReader, port.RoleResolver) — both reads are
// answered from an in-memory snapshot loaded at boot and reloaded after every
// write, so a board move never queries the database to ask "does this stage
// have build_verify".
package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ErrSnapshotEmpty is returned by every WorkflowReader/RoleResolver method
// when no snapshot has ever loaded successfully — boot ran before the first
// Reload, or every Reload attempt has failed. Gates built on Workflow/Has/Param
// must fail CLOSED on this, never silently answer as if a type or stage were
// simply absent.
var ErrSnapshotEmpty = errors.New("workflow snapshot not loaded")

// BoardColumnLister is the slice of port.BoardConfigStore validation needs to
// check a behaviour's column param against the board's actual columns.
// Declared narrow (rather than depending on port.BoardConfigStore directly)
// so a caller with only a column slice at hand — a test, workflowtest — can
// satisfy it trivially.
type BoardColumnLister interface {
	ListColumns(ctx context.Context) ([]domain.BoardColumn, error)
}

type snapshot struct {
	workflows   map[domain.TaskType]domain.Workflow
	defaultType domain.TaskType
	defectType  domain.TaskType
	roles       map[uuid.UUID]domain.AgentRole
	purposes    map[domain.RolePurposeKey]*uuid.UUID
}

// Service implements port.RoleStore, port.WorkflowStore, port.WorkflowReader
// and port.RoleResolver.
type Service struct {
	roles     port.RoleStore
	workflows port.WorkflowStore
	board     BoardColumnLister
	// agentCatalog is nil until SetAgentCatalog wires it; see that setter's
	// doc for what stays refused without it.
	agentCatalog AgentCatalog

	mu   sync.RWMutex
	snap *snapshot
}

func NewService(roles port.RoleStore, workflows port.WorkflowStore) *Service {
	return &Service{roles: roles, workflows: workflows}
}

// SetBoardColumnLister wires the column source stage validation checks a
// column param against. Nil (the pre-wiring default, and every workflowtest
// use) skips that one check — a param naming an unknown column is caught
// later, at dispatch, rather than at save time.
func (s *Service) SetBoardColumnLister(b BoardColumnLister) { s.board = b }

// Reload rebuilds the in-memory snapshot from the stores. Call it once at
// boot and after every write this service makes; on error the PREVIOUS
// snapshot (if any) is left in place — a failed reload must not turn a
// working cache into an empty, fail-closed one over a transient read error.
func (s *Service) Reload(ctx context.Context) error {
	workflows, err := s.workflows.LoadAll(ctx)
	if err != nil {
		return fmt.Errorf("reload workflows: %w", err)
	}
	roleList, err := s.roles.List(ctx)
	if err != nil {
		return fmt.Errorf("reload roles: %w", err)
	}
	purposeList, err := s.roles.ListPurposes(ctx)
	if err != nil {
		return fmt.Errorf("reload role purposes: %w", err)
	}

	next := &snapshot{
		workflows: make(map[domain.TaskType]domain.Workflow, len(workflows)),
		roles:     make(map[uuid.UUID]domain.AgentRole, len(roleList)),
		purposes:  make(map[domain.RolePurposeKey]*uuid.UUID, len(purposeList)),
	}
	for _, wf := range workflows {
		next.workflows[wf.Type.Key] = wf
		if wf.Type.IsDefault {
			next.defaultType = wf.Type.Key
		}
		if wf.Type.IsDefect {
			next.defectType = wf.Type.Key
		}
	}
	for _, r := range roleList {
		next.roles[r.ID] = r
	}
	for _, p := range purposeList {
		roleID := p.RoleID
		next.purposes[p.Purpose] = roleID
	}

	s.mu.Lock()
	s.snap = next
	s.mu.Unlock()
	return nil
}

func (s *Service) getSnapshot() (*snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snap == nil {
		return nil, ErrSnapshotEmpty
	}
	return s.snap, nil
}

// ---- port.WorkflowReader ----

// Workflow returns taskType's workflow, or the default type's workflow when
// taskType is unknown to this snapshot — a task whose type was since deleted,
// or created before its type existed, still has somewhere to route.
func (s *Service) Workflow(ctx context.Context, taskType domain.TaskType) (domain.Workflow, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return domain.Workflow{}, err
	}
	if wf, ok := snap.workflows[taskType]; ok {
		return wf, nil
	}
	if snap.defaultType == "" {
		return domain.Workflow{}, fmt.Errorf("workflow: unknown task type %q and no default type configured", taskType)
	}
	wf, ok := snap.workflows[snap.defaultType]
	if !ok {
		return domain.Workflow{}, fmt.Errorf("workflow: default task type %q not loaded", snap.defaultType)
	}
	return wf, nil
}

func (s *Service) DefaultTaskType(ctx context.Context) (domain.TaskType, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return "", err
	}
	if snap.defaultType == "" {
		return "", fmt.Errorf("workflow: no default task type configured")
	}
	return snap.defaultType, nil
}

func (s *Service) DefectTaskType(ctx context.Context) (domain.TaskType, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return "", err
	}
	if snap.defectType == "" {
		return "", fmt.Errorf("workflow: no defect task type configured")
	}
	return snap.defectType, nil
}

func (s *Service) TaskTypeExists(ctx context.Context, taskType domain.TaskType) (bool, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return false, err
	}
	_, ok := snap.workflows[taskType]
	return ok, nil
}

func (s *Service) KeyPrefix(ctx context.Context, taskType domain.TaskType) (string, error) {
	wf, err := s.Workflow(ctx, taskType)
	if err != nil {
		return "", err
	}
	return wf.Type.KeyPrefix, nil
}
