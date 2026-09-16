package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// AgentCatalog is the narrow slice of catalog.Service UpdateAnalizAssignment
// needs: look an agent up by name to check its tool policy, and widen that
// policy once the user has confirmed the grant.
type AgentCatalog interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	UpdateAgent(ctx context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error)
}

type Service struct {
	store  port.SettingsStore
	agents AgentCatalog
}

func NewService(store port.SettingsStore) *Service {
	return &Service{store: store}
}

// SetAgentCatalog wires the agent roster lookup UpdateAnalizAssignment needs.
// Nil (the pre-wiring behaviour) makes any assignment change to a
// non-system-architect agent fail rather than silently skip the tool check.
func (s *Service) SetAgentCatalog(agents AgentCatalog) {
	s.agents = agents
}

func (s *Service) Get(ctx context.Context) (domain.AppSettings, error) {
	return s.store.Get(ctx)
}

func (s *Service) Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	return s.store.Update(ctx, req)
}

// UpdateAnalizAssignment changes which agent an analiz task for the
// backend/frontend/mobile area is auto-assigned to. An area left as "" is
// untouched; "-" resets it to AgentSystemArchitect, which is always allowed
// (see architectToolPolicy) and therefore never triggers the tool check
// below. Any other value is looked up by name and checked against
// domain.RequiredAnalizTools: missing tools without ConfirmGrantTools refuses
// the whole request — nothing is saved — via MissingAnalizToolsError; with
// it, the missing tools are appended to the agent's policy before the
// setting is saved.
func (s *Service) UpdateAnalizAssignment(ctx context.Context, req domain.UpdateAnalizAssignmentRequest) (domain.AnalizAssignmentResult, error) {
	changes := map[string]string{
		"backend":  strings.TrimSpace(req.Backend),
		"frontend": strings.TrimSpace(req.Frontend),
		"mobile":   strings.TrimSpace(req.Mobile),
	}

	missingByArea := map[string][]string{}
	grantedByArea := map[string][]string{}
	for _, area := range []string{"backend", "frontend", "mobile"} {
		value := changes[area]
		if value == "" {
			continue
		}
		target := value
		if target == "-" {
			target = domain.AgentSystemArchitect
		}
		if target == domain.AgentSystemArchitect {
			continue
		}
		agent, found, err := s.findAgentByName(ctx, target)
		if err != nil {
			return domain.AnalizAssignmentResult{}, err
		}
		if !found {
			return domain.AnalizAssignmentResult{}, fmt.Errorf("unknown assignee %q for %s", target, area)
		}
		missing := domain.MissingAnalizTools(agent.ToolPolicy)
		if len(missing) == 0 {
			continue
		}
		if !req.ConfirmGrantTools {
			missingByArea[area] = missing
			continue
		}
		granted, err := s.grantTools(ctx, agent, missing)
		if err != nil {
			return domain.AnalizAssignmentResult{}, err
		}
		grantedByArea[area] = granted
	}

	if len(missingByArea) > 0 {
		return domain.AnalizAssignmentResult{}, &domain.MissingAnalizToolsError{Missing: missingByArea}
	}

	saved, err := s.store.UpdateAnalizAssignment(ctx, changes["backend"], changes["frontend"], changes["mobile"])
	if err != nil {
		return domain.AnalizAssignmentResult{}, err
	}
	result := domain.AnalizAssignmentResult{Saved: true, Settings: &saved}
	if len(grantedByArea) > 0 {
		result.GrantedTools = grantedByArea
	}
	return result, nil
}

func (s *Service) findAgentByName(ctx context.Context, name string) (domain.Agent, bool, error) {
	if s.agents == nil {
		return domain.Agent{}, false, errors.New("agent roster unavailable to check tool policy")
	}
	agents, err := s.agents.ListAgents(ctx)
	if err != nil {
		return domain.Agent{}, false, err
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, name) {
			return a, true, nil
		}
	}
	return domain.Agent{}, false, nil
}

func (s *Service) grantTools(ctx context.Context, agent domain.Agent, missing []string) ([]string, error) {
	policy := agent.ToolPolicy
	policy.AllowTools = append(append([]string(nil), policy.AllowTools...), missing...)
	_, err := s.agents.UpdateAgent(ctx, agent.ID, domain.UpdateAgentRequest{
		Name:                 agent.Name,
		Description:          agent.Description,
		SubagentType:         agent.SubagentType,
		SystemPrompt:         agent.SystemPrompt,
		ProviderType:         agent.ProviderType,
		Model:                agent.Model,
		ModelHeavy:           agent.ModelHeavy,
		MaxTurns:             agent.MaxTurns,
		Effort:               agent.Effort,
		ToolPolicy:           policy,
		Enabled:              agent.Enabled,
		SelfEvolutionEnabled: agent.SelfEvolutionEnabled,
	})
	if err != nil {
		return nil, err
	}
	return missing, nil
}
