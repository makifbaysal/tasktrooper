package settings

import (
	"context"

	"github.com/google/uuid"
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

type Service struct {
	store  port.SettingsStore
	agents AgentCatalog

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

func (s *Service) Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	return s.store.Update(ctx, req)
}
