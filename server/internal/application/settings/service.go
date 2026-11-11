package settings

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// AgentCatalog is wired but currently unread: it survives from the
// analiz-assignment tool-grant flow this service used to run itself, now
// application/workflow.Service's own (SetRoleAssignmentsChecked /
// SetAgentRolesChecked). Kept so platform/runtime's wiring call
// (SetAgentCatalog) needs no matching removal there.
type AgentCatalog interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	UpdateAgent(ctx context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error)
}

// StageSwitch is the workflow engine's PM UAT on/off switch.
type StageSwitch interface {
	SetPMUATEnabled(enabled bool)
	StoredWorkflow(taskType domain.TaskType) (domain.Workflow, error)
}

// TaskMover lists every task and moves one; repository.Service.
type TaskMover interface {
	ListAllTasks(ctx context.Context) ([]domain.BoardTask, error)
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
}

type Service struct {
	store  port.SettingsStore
	agents AgentCatalog

	stages StageSwitch
	tasks  TaskMover

	// workflows/roles: see board.Dispatcher's own fields of the same name.
	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

func NewService(store port.SettingsStore) *Service {
	return &Service{store: store}
}

// SetAgentCatalog wires the agent roster lookup; see the AgentCatalog doc
// comment for why this stays even though nothing here reads it anymore.
func (s *Service) SetAgentCatalog(agents AgentCatalog) {
	s.agents = agents
}

func (s *Service) Get(ctx context.Context) (domain.AppSettings, error) {
	return s.store.Get(ctx)
}

func (s *Service) SetStageSwitch(sw StageSwitch) { s.stages = sw }
func (s *Service) SetTaskMover(t TaskMover)      { s.tasks = t }

func (s *Service) Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	out, err := s.store.Update(ctx, req)
	if err != nil {
		return out, err
	}
	if req.PMUATEnabled != nil {
		s.ApplyPMUAT(ctx, out.PMUATEnabled)
	}
	return out, nil
}

// ApplyStoredPMUAT pushes the saved PM UAT setting into the engine; call at
// boot after the workflow snapshot loads.
func (s *Service) ApplyStoredPMUAT(ctx context.Context) {
	out, err := s.store.Get(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("pm uat setting unreadable, step stays on")
		return
	}
	s.ApplyPMUAT(ctx, out.PMUATEnabled)
}

// ApplyPMUAT switches the step, and when it goes off moves every task still in
// pm_uat on to the stage's exit (human_uat in the stock workflow). Idempotent:
// the boot call finishes a sweep an earlier crash left half done.
func (s *Service) ApplyPMUAT(ctx context.Context, enabled bool) {
	if s.stages == nil {
		return
	}
	var stranded []domain.BoardTask
	if !enabled && s.tasks != nil {
		all, err := s.tasks.ListAllTasks(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("pm uat off: listing tasks failed, none moved")
		}
		for _, t := range all {
			if t.Column == domain.TaskColumnPMUAT {
				stranded = append(stranded, t)
			}
		}
	}
	exits := make(map[domain.TaskType]domain.TaskColumn, len(stranded))
	for _, t := range stranded {
		if _, ok := exits[t.TaskType]; ok {
			continue
		}
		exit := domain.TaskColumnHumanUAT
		if wf, err := s.stages.StoredWorkflow(t.TaskType); err == nil {
			if to, ok := wf.StageExit(domain.TaskColumnPMUAT); ok && to != domain.TaskColumnPMUAT {
				exit = to
			}
		}
		exits[t.TaskType] = exit
	}
	s.stages.SetPMUATEnabled(enabled)
	for _, t := range stranded {
		target := exits[t.TaskType]
		if _, err := s.tasks.UpdateTask(ctx, t.RepositoryID, t.ID, domain.UpdateBoardTaskRequest{
			Column:       &target,
			Actor:        domain.TaskActorSystem,
			SystemReason: domain.MoveReasonPMUATDisabled,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Str("target", string(target)).
				Msg("pm uat off: could not move task out of pm_uat")
		}
	}
}
