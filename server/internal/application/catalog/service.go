package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	store          port.CatalogStore
	llm            port.LLMClient
	embeddingModel string
	templates      port.AgentTemplateStore
	kpis           port.AgentKPIStore
	boardConfig    port.BoardConfigStore
	versions       port.CatalogVersionStore
	roleAdmin      RoleAdmin
	// nil means CLI-only.
	providers LLMProviders
	// Nil means no runner is attached — correct on every host without an agent CLI installed.
	hostExecutor HostExecutorProbe
}

type HostExecutorProbe func(domain.LLMProviderType) bool

var ErrNoHostRunner = errors.New("no host runner for this provider")

var ErrInvalidInput = errors.New("invalid input")

type invalidInputError struct{ msg string }

func (e invalidInputError) Error() string        { return e.msg }
func (e invalidInputError) Is(target error) bool { return target == ErrInvalidInput }

func invalidInput(format string, a ...any) error {
	return invalidInputError{msg: fmt.Sprintf(format, a...)}
}

// Constant false now that agents arrive through the external catalog sync; kept for the UI's stable "seeding" poll contract.
func (s *Service) SeedingInProgress() bool {
	return false
}

func NewService(store port.CatalogStore, llm port.LLMClient, embeddingModel string) *Service {
	return &Service{store: store, llm: llm, embeddingModel: embeddingModel}
}

func (s *Service) SetTemplateStore(store port.AgentTemplateStore) {
	s.templates = store
}

func (s *Service) SetKPIStore(store port.AgentKPIStore) {
	s.kpis = store
}

func (s *Service) SetBoardConfigStore(store port.BoardConfigStore) {
	s.boardConfig = store
}

type RoleAdmin interface {
	ListRoles(ctx context.Context) ([]domain.AgentRole, error)
	SetRoleAssignments(ctx context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error
}

func (s *Service) SetRoleAdmin(r RoleAdmin) {
	s.roleAdmin = r
}

// Runner existence is discovered at boot by probing the host, not read from configuration.
func (s *Service) SetHostExecutorProbe(p HostExecutorProbe) {
	s.hostExecutor = p
}

// Refuses to persist an agent whose engine does not exist here; uncaught, the same misconfiguration surfaced as many unrelated runtime failures.
func (s *Service) checkHostExecutor(providerType domain.LLMProviderType) error {
	if !domain.RequiresHostExecutor(providerType) {
		return nil
	}
	if s.hostExecutor != nil && s.hostExecutor(providerType) {
		return nil
	}
	label := string(providerType)
	if def, ok := domain.LLMProviderDefinitionFor(providerType); ok && def.Label != "" {
		label = def.Label
	}
	return fmt.Errorf("%w: this agent cannot run on %s here — this workspace has no runner attached for it; "+
		"start a local runner on a machine with that CLI installed, or pick another provider for this agent",
		ErrNoHostRunner, label)
}

// Refuses a provider declared but with no executor; runs before checkHostExecutor because "no runner attached" is fixable and "no executor was written" is not.
func (s *Service) checkProviderAvailable(providerType domain.LLMProviderType) error {
	if providerType == "" || !domain.ValidLLMProviderType(string(providerType)) {
		return nil
	}
	if domain.ProviderAvailable(providerType) {
		return nil
	}
	return domain.ErrUnavailableProvider(providerType)
}

func (s *Service) CreateSkillForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error) {
	if req.Name == "" {
		return domain.Skill{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.Skill{}, invalidInput("content is required")
	}
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return domain.Skill{}, fmt.Errorf("agent not found: %w", err)
	}
	stackID, err := s.resolveTechStack(ctx, agentID, req.TechStackID)
	if err != nil {
		return domain.Skill{}, err
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	emb, err := s.llm.Embed(ctx, req.Name+"\n"+req.Description+"\n"+req.Content, s.embeddingModel)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("embed skill: %w", err)
	}
	created, err := s.store.CreateSkill(ctx, domain.Skill{
		AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Embedding: emb, Enabled: req.Enabled, TechStackID: stackID,
	})
	if err != nil {
		return domain.Skill{}, err
	}
	s.recordSkillVersion(ctx, domain.CatalogVersionActionCreate, created)
	return created, nil
}

func (s *Service) GetSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID) (domain.Skill, error) {
	skill, err := s.store.GetSkill(ctx, skillID)
	if err != nil {
		return domain.Skill{}, err
	}
	if skill.AgentID != agentID {
		return domain.Skill{}, fmt.Errorf("skill not found for agent")
	}
	return skill, nil
}

func (s *Service) ListSkillsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.ListSkillsByAgent(ctx, agentID)
}

func (s *Service) UpdateSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID, req domain.UpdateSkillRequest) (domain.Skill, error) {
	if req.Name == "" {
		return domain.Skill{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.Skill{}, invalidInput("content is required")
	}
	current, err := s.GetSkillForAgent(ctx, agentID, skillID)
	if err != nil {
		return domain.Skill{}, err
	}
	stackID, err := s.resolveTechStack(ctx, agentID, req.TechStackID)
	if err != nil {
		return domain.Skill{}, err
	}
	emb, err := s.llm.Embed(ctx, req.Name+"\n"+req.Description+"\n"+req.Content, s.embeddingModel)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("embed skill: %w", err)
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	// catalog_sha survives an edit: it is the revision the edit was made on, which is how the catalog sync tells an edit from a stale copy.
	updated, err := s.store.UpdateSkill(ctx, domain.Skill{
		ID: skillID, AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Embedding: emb, Enabled: req.Enabled, TechStackID: stackID,
		CatalogSha: current.CatalogSha,
	})
	if err != nil {
		return domain.Skill{}, err
	}
	s.recordSkillVersion(ctx, updateAction(ctx), updated)
	return updated, nil
}

func (s *Service) DeleteSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID) error {
	existing, err := s.GetSkillForAgent(ctx, agentID, skillID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSkill(ctx, skillID); err != nil {
		return err
	}
	// The deleted content is the last write to history so the skill can be restored.
	s.recordSkillVersion(ctx, domain.CatalogVersionActionDelete, existing)
	return nil
}

func (s *Service) SearchSkills(ctx context.Context, query string, topK int) ([]domain.Skill, error) {
	emb, err := s.llm.Embed(ctx, query, s.embeddingModel)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return s.store.SearchSkills(ctx, emb, topK, nil)
}

// Any real stack id is valid to Postgres regardless of which agent owns it, so the reference passes the FK and fails everyone else.
func (s *Service) resolveTechStack(ctx context.Context, agentID uuid.UUID, stackID *uuid.UUID) (*uuid.UUID, error) {
	if stackID == nil || *stackID == uuid.Nil {
		return nil, nil
	}
	stack, err := s.store.GetTechStack(ctx, *stackID)
	if err != nil {
		return nil, invalidInput("tech stack not found")
	}
	if stack.AgentID != agentID {
		return nil, invalidInput("tech stack belongs to another agent")
	}
	return stackID, nil
}

func (s *Service) CreateTechStackForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateTechStackRequest) (domain.TechStack, error) {
	if req.Name == "" {
		return domain.TechStack{}, invalidInput("name is required")
	}
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return domain.TechStack{}, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.CreateTechStack(ctx, domain.TechStack{
		AgentID: agentID, Name: req.Name, Description: req.Description, Position: req.Position,
	})
}

func (s *Service) GetTechStackForAgent(ctx context.Context, agentID, stackID uuid.UUID) (domain.TechStack, error) {
	stack, err := s.store.GetTechStack(ctx, stackID)
	if err != nil {
		return domain.TechStack{}, err
	}
	if stack.AgentID != agentID {
		return domain.TechStack{}, fmt.Errorf("tech stack not found for agent")
	}
	return stack, nil
}

func (s *Service) ListTechStacksForAgent(ctx context.Context, agentID uuid.UUID) ([]domain.TechStack, error) {
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.ListTechStacksByAgent(ctx, agentID)
}

func (s *Service) UpdateTechStackForAgent(ctx context.Context, agentID, stackID uuid.UUID, req domain.UpdateTechStackRequest) (domain.TechStack, error) {
	existing, err := s.GetTechStackForAgent(ctx, agentID, stackID)
	if err != nil {
		return domain.TechStack{}, err
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Position != nil {
		existing.Position = *req.Position
	}
	if existing.Name == "" {
		return domain.TechStack{}, invalidInput("name is required")
	}
	return s.store.UpdateTechStack(ctx, existing)
}

// The FK nulls skills' tech_stack_id, filing them back as general skills.
func (s *Service) DeleteTechStackForAgent(ctx context.Context, agentID, stackID uuid.UUID) error {
	if _, err := s.GetTechStackForAgent(ctx, agentID, stackID); err != nil {
		return err
	}
	return s.store.DeleteTechStack(ctx, stackID)
}

func (s *Service) CreateAgent(ctx context.Context, req domain.CreateAgentRequest) (domain.Agent, error) {
	if req.Name == "" {
		return domain.Agent{}, invalidInput("name is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = "generalPurpose"
	}
	if !domain.ValidEffort(req.Effort) {
		return domain.Agent{}, invalidInput("effort must be one of %v, or empty for the executor's default", domain.EffortLevels)
	}
	if req.MaxTurns < 0 {
		return domain.Agent{}, invalidInput("max_turns cannot be negative; use 0 for the executor's default")
	}
	if err := s.checkProviderAvailable(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	if err := s.checkHostExecutor(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	return s.store.CreateAgent(ctx, domain.Agent{
		Name: req.Name, Description: req.Description, SubagentType: req.SubagentType,
		SystemPrompt: req.SystemPrompt, ProviderType: req.ProviderType, Model: req.Model, ModelHeavy: req.ModelHeavy, MaxTurns: req.MaxTurns, Effort: req.Effort, ToolPolicy: req.ToolPolicy, Enabled: req.Enabled,
		SelfEvolutionEnabled: req.SelfEvolutionEnabled,
		AutoPullAgentUpdates: true,
		KeepSkillsUpdated:    true,
	})
}

func (s *Service) GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error) {
	return s.store.GetAgent(ctx, id)
}

func (s *Service) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	return s.store.ListAgents(ctx)
}

func (s *Service) UpdateAgent(ctx context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error) {
	if req.Name == "" {
		return domain.Agent{}, invalidInput("name is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = "generalPurpose"
	}
	if !domain.ValidEffort(req.Effort) {
		return domain.Agent{}, invalidInput("effort must be one of %v, or empty for the executor's default", domain.EffortLevels)
	}
	if req.MaxTurns < 0 {
		return domain.Agent{}, invalidInput("max_turns cannot be negative; use 0 for the executor's default")
	}
	if err := s.checkProviderAvailable(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	if err := s.checkHostExecutor(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	existing, err := s.store.GetAgent(ctx, id)
	if err != nil {
		return domain.Agent{}, err
	}
	req = dropStaleModels(existing, req)
	autoPull := existing.AutoPullAgentUpdates
	if req.AutoPullAgentUpdates != nil {
		autoPull = *req.AutoPullAgentUpdates
	}
	keepUpdated := existing.KeepSkillsUpdated
	if req.KeepSkillsUpdated != nil {
		keepUpdated = *req.KeepSkillsUpdated
	}
	return s.store.UpdateAgent(ctx, domain.Agent{
		ID: id, Name: req.Name, Description: req.Description, SubagentType: req.SubagentType,
		SystemPrompt: req.SystemPrompt, ProviderType: req.ProviderType, Model: req.Model, ModelHeavy: req.ModelHeavy, MaxTurns: req.MaxTurns, Effort: req.Effort, ToolPolicy: req.ToolPolicy, Enabled: req.Enabled,
		SelfEvolutionEnabled: req.SelfEvolutionEnabled,
		AutoPullAgentUpdates: autoPull,
		KeepSkillsUpdated:    keepUpdated,
		CatalogSlug:          existing.CatalogSlug,
		CatalogEtag:          existing.CatalogEtag,
	})
}

// A model name only means something to its own provider, so a provider switch clears carried-over names; a name the caller actually changed is left alone.
func dropStaleModels(existing domain.Agent, req domain.UpdateAgentRequest) domain.UpdateAgentRequest {
	if existing.ProviderType == "" || req.ProviderType == "" || existing.ProviderType == req.ProviderType {
		return req
	}
	if req.Model == existing.Model {
		req.Model = ""
	}
	if req.ModelHeavy == existing.ModelHeavy {
		req.ModelHeavy = ""
	}
	return req
}

func (s *Service) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	return s.store.DeleteAgent(ctx, id)
}

func (s *Service) CreateRuleForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	if req.Name == "" {
		return domain.OrchestratorRule{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.OrchestratorRule{}, invalidInput("content is required")
	}
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return domain.OrchestratorRule{}, fmt.Errorf("agent not found: %w", err)
	}
	created, err := s.store.CreateRule(ctx, domain.OrchestratorRule{
		AgentID: agentID, Name: req.Name, Content: req.Content, Priority: req.Priority, Enabled: req.Enabled,
	})
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	s.recordRuleVersion(ctx, domain.CatalogVersionActionCreate, created)
	return created, nil
}

func (s *Service) GetRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID) (domain.OrchestratorRule, error) {
	rule, err := s.store.GetRule(ctx, ruleID)
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	if rule.AgentID != agentID {
		return domain.OrchestratorRule{}, fmt.Errorf("rule not found for agent")
	}
	return rule, nil
}

func (s *Service) ListRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error) {
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.ListRulesByAgent(ctx, agentID)
}

func (s *Service) UpdateRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID, req domain.UpdateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	if req.Name == "" {
		return domain.OrchestratorRule{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.OrchestratorRule{}, invalidInput("content is required")
	}
	if _, err := s.GetRuleForAgent(ctx, agentID, ruleID); err != nil {
		return domain.OrchestratorRule{}, err
	}
	updated, err := s.store.UpdateRule(ctx, domain.OrchestratorRule{
		ID: ruleID, AgentID: agentID, Name: req.Name, Content: req.Content, Priority: req.Priority, Enabled: req.Enabled,
	})
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	s.recordRuleVersion(ctx, updateAction(ctx), updated)
	return updated, nil
}

func (s *Service) DeleteRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID) error {
	existing, err := s.GetRuleForAgent(ctx, agentID, ruleID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteRule(ctx, ruleID); err != nil {
		return err
	}
	s.recordRuleVersion(ctx, domain.CatalogVersionActionDelete, existing)
	return nil
}

func (s *Service) GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error) {
	return s.store.GetPlanByRunID(ctx, runID)
}
