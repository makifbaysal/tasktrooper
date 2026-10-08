package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) ListAgentTemplates(ctx context.Context) ([]domain.AgentTemplate, error) {
	if s.templates == nil {
		return nil, fmt.Errorf("agent templates not enabled")
	}
	templates, err := s.templates.List(ctx)
	if err != nil {
		return nil, err
	}
	if templates == nil {
		templates = []domain.AgentTemplate{}
	}
	return templates, nil
}

func (s *Service) CreateAgentFromTemplate(ctx context.Context, templateID uuid.UUID, override domain.CreateAgentRequest) (domain.Agent, error) {
	if s.templates == nil {
		return domain.Agent{}, fmt.Errorf("agent templates not enabled")
	}
	tpl, err := s.templates.Get(ctx, templateID)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("template not found: %w", err)
	}
	req := domain.CreateAgentRequest{
		Name:                 tpl.Name,
		Description:          tpl.Description,
		SubagentType:         tpl.SubagentType,
		SystemPrompt:         tpl.SystemPrompt,
		ProviderType:         tpl.ProviderType,
		Model:                tpl.Model,
		ToolPolicy:           tpl.ToolPolicy,
		Enabled:              true,
		SelfEvolutionEnabled: tpl.SelfEvolutionEnabled || override.SelfEvolutionEnabled,
	}
	if strings.TrimSpace(override.Name) != "" {
		req.Name = strings.TrimSpace(override.Name)
	} else {
		req.Name = s.uniqueAgentName(ctx, tpl.Name)
	}
	if strings.TrimSpace(override.Description) != "" {
		req.Description = override.Description
	}
	if strings.TrimSpace(override.SystemPrompt) != "" {
		req.SystemPrompt = override.SystemPrompt
	}
	if strings.TrimSpace(override.Model) != "" {
		req.Model = override.Model
	}
	if override.ProviderType != "" {
		req.ProviderType = override.ProviderType
	}
	if tpl.BuiltIn {
		s.fillTemplateAgentModels(&req)
	}
	agent, err := s.CreateAgent(ctx, req)
	if err != nil {
		return domain.Agent{}, err
	}
	if err := s.applySuggestedSubscriptions(ctx, agent, tpl.Subscriptions); err != nil {
		return domain.Agent{}, fmt.Errorf("apply suggested subscriptions for %s: %w", agent.Name, err)
	}
	if err := s.applySuggestedRoles(ctx, agent, tpl.Roles); err != nil {
		return domain.Agent{}, fmt.Errorf("apply suggested roles for %s: %w", agent.Name, err)
	}
	stackIDs, err := s.recreateTechStacks(ctx, agent.ID, tpl)
	if err != nil {
		return domain.Agent{}, err
	}
	for _, tplSkill := range tpl.Skills {
		skillReq := tplSkill.CreateRequest()
		if id, ok := stackIDs[stackKey(tplSkill.TechStack)]; ok {
			skillReq.TechStackID = &id
		}
		if err := s.seedSkill(ctx, agent.ID, skillReq); err != nil {
			return domain.Agent{}, fmt.Errorf("copy skill %s: %w", skillReq.Name, err)
		}
	}
	for _, ruleReq := range tpl.Rules {
		if _, err := s.CreateRuleForAgent(ctx, agent.ID, ruleReq); err != nil {
			return domain.Agent{}, fmt.Errorf("copy rule %s: %w", ruleReq.Name, err)
		}
	}
	if s.kpis != nil {
		for _, kpiReq := range tpl.KPIs {
			if _, err := s.kpis.CreateKPI(ctx, domain.AgentKPI{
				AgentID: agent.ID, MetricKey: kpiReq.MetricKey, Name: kpiReq.Name,
				Description: kpiReq.Description, Period: kpiReq.Period,
				TargetFull: kpiReq.TargetFull, TargetHalf: kpiReq.TargetHalf,
				Weight: kpiReq.Weight, Enabled: kpiReq.Enabled,
			}); err != nil {
				return domain.Agent{}, fmt.Errorf("copy kpi %s: %w", kpiReq.MetricKey, err)
			}
		}
	}
	return s.store.GetAgent(ctx, agent.ID)
}

func (s *Service) SaveAgentAsTemplate(ctx context.Context, agentID uuid.UUID) (domain.AgentTemplate, error) {
	if s.templates == nil {
		return domain.AgentTemplate{}, fmt.Errorf("agent templates not enabled")
	}
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, fmt.Errorf("agent not found: %w", err)
	}
	skills, err := s.store.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	stacks, err := s.store.ListTechStacksByAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	stackNames := make(map[uuid.UUID]string, len(stacks))
	for _, st := range stacks {
		stackNames[st.ID] = st.Name
	}
	rules, err := s.store.ListRulesByAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	tpl := domain.AgentTemplate{
		Name:                 agent.Name,
		Description:          agent.Description,
		SubagentType:         agent.SubagentType,
		SystemPrompt:         agent.SystemPrompt,
		ProviderType:         agent.ProviderType,
		Model:                agent.Model,
		ToolPolicy:           agent.ToolPolicy,
		SelfEvolutionEnabled: agent.SelfEvolutionEnabled,
		TechStacks:           make([]domain.CreateTechStackRequest, 0, len(stacks)),
		Skills:               make([]domain.TemplateSkill, 0, len(skills)),
		Rules:                make([]domain.CreateOrchestratorRuleRequest, 0, len(rules)),
		Roles:                s.agentRoleSuggestions(ctx, agentID),
		Subscriptions:        s.agentSubscriptionColumns(ctx, agentID),
	}
	for _, st := range stacks {
		tpl.TechStacks = append(tpl.TechStacks, domain.CreateTechStackRequest{
			Name: st.Name, Description: st.Description, Position: st.Position,
		})
	}
	for _, sk := range skills {
		tplSkill := domain.TemplateSkill{
			Name: sk.Name, Description: sk.Description, Category: sk.Category,
			Tags: sk.Tags, Content: sk.Content, Enabled: sk.Enabled,
		}
		if sk.TechStackID != nil {
			tplSkill.TechStack = stackNames[*sk.TechStackID]
		}
		tpl.Skills = append(tpl.Skills, tplSkill)
	}
	for _, r := range rules {
		tpl.Rules = append(tpl.Rules, domain.CreateOrchestratorRuleRequest{
			Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
		})
	}
	if s.kpis != nil {
		kpis, err := s.kpis.ListByAgent(ctx, agentID)
		if err != nil {
			return domain.AgentTemplate{}, err
		}
		tpl.KPIs = make([]domain.CreateKPIRequest, 0, len(kpis))
		for _, k := range kpis {
			tpl.KPIs = append(tpl.KPIs, domain.CreateKPIRequest{
				MetricKey: k.MetricKey, Name: k.Name, Description: k.Description,
				Period: k.Period, TargetFull: k.TargetFull, TargetHalf: k.TargetHalf,
				Weight: k.Weight, Enabled: k.Enabled,
			})
		}
	}
	return s.templates.UpsertByName(ctx, tpl)
}

// Each skill files under the stack it was saved in; a hand-written template that tags only its skills still groups them.
func (s *Service) recreateTechStacks(ctx context.Context, agentID uuid.UUID, tpl domain.AgentTemplate) (map[string]uuid.UUID, error) {
	wanted := make([]domain.CreateTechStackRequest, 0, len(tpl.TechStacks))
	seen := make(map[string]bool, len(tpl.TechStacks))
	add := func(req domain.CreateTechStackRequest) {
		key := stackKey(req.Name)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		wanted = append(wanted, req)
	}
	for _, st := range tpl.TechStacks {
		add(st)
	}
	for _, sk := range tpl.Skills {
		add(domain.CreateTechStackRequest{Name: strings.TrimSpace(sk.TechStack), Position: len(wanted)})
	}

	ids := make(map[string]uuid.UUID, len(wanted))
	for _, req := range wanted {
		created, err := s.CreateTechStackForAgent(ctx, agentID, req)
		if err != nil {
			return nil, fmt.Errorf("copy tech stack %s: %w", req.Name, err)
		}
		ids[stackKey(created.Name)] = created.ID
	}
	return ids, nil
}

func stackKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// When a create request names no model pair, fills the preferred CLI provider's defaults; an override that set either name alone is left alone, and the provider is only stamped when firstRunnableCLI says this host can execute it.
func (s *Service) fillTemplateAgentModels(req *domain.CreateAgentRequest) {
	if req.Model != "" || req.ModelHeavy != "" {
		return
	}
	if req.ProviderType == "" {
		p := s.firstRunnableCLI()
		if p == "" {
			return
		}
		req.ProviderType = p
	}
	req.Model, req.ModelHeavy = domain.ProviderDefaultModels(req.ProviderType)
}

// Most preferred host-executed CLI this host can run, in cliPreference order, so a built-in template never proposes something checkHostExecutor would refuse.
func (s *Service) firstRunnableCLI() domain.LLMProviderType {
	if s.hostExecutor == nil {
		return ""
	}
	for _, p := range cliPreference {
		if s.hostExecutor(p) {
			return p
		}
	}
	return ""
}

// Seeds hand-off subscriptions only into a genuinely open seat: the agent has none yet, and the column has no other subscriber —
// except a quorum review column, where every subscriber is one more required reviewer rather than a competitor for the card.
func (s *Service) applySuggestedSubscriptions(ctx context.Context, agent domain.Agent, suggested []domain.TaskColumn) error {
	if s.boardConfig == nil || len(suggested) == 0 {
		return nil
	}
	existing, err := s.boardConfig.ListAgentSubscriptions(ctx, agent.ID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	all, err := s.boardConfig.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	occupied := make(map[string]bool, len(all))
	for _, sub := range all {
		occupied[sub.ColumnSlug] = true
	}
	slugs := make([]string, 0, len(suggested))
	for _, col := range suggested {
		if occupied[string(col)] && !domain.QuorumReviewColumn(col) {
			continue
		}
		slugs = append(slugs, string(col))
	}
	if len(slugs) == 0 {
		return nil
	}
	return s.boardConfig.SetAgentSubscriptions(ctx, agent.ID, slugs)
}

// Assigns to suggested roles only into a vacancy: the role exists and no existing assignment already covers the suggested areas.
func (s *Service) applySuggestedRoles(ctx context.Context, agent domain.Agent, suggested []domain.TemplateRoleSuggestion) error {
	if s.roleAdmin == nil || len(suggested) == 0 {
		return nil
	}
	roles, err := s.roleAdmin.ListRoles(ctx)
	if err != nil {
		return err
	}
	byKey := make(map[string]domain.AgentRole, len(roles))
	for _, r := range roles {
		byKey[r.Key] = r
	}
	for _, sug := range suggested {
		role, ok := byKey[sug.Key]
		if !ok || !roleHasVacancyFor(role, sug.Areas) {
			continue
		}
		assignments := append(append([]domain.RoleAssignment{}, role.Assignments...), domain.RoleAssignment{
			AgentID: agent.ID, Areas: sug.Areas,
		})
		if err := s.roleAdmin.SetRoleAssignments(ctx, role.ID, assignments); err != nil {
			return err
		}
	}
	return nil
}

// A nil (any-area) request is vacant only when the role has no assignment at all.
func roleHasVacancyFor(role domain.AgentRole, areas []string) bool {
	if len(areas) == 0 {
		return len(role.Assignments) == 0
	}
	for _, a := range role.Assignments {
		if a.Areas == nil {
			return false
		}
		for _, want := range areas {
			for _, have := range a.Areas {
				if have == want {
					return false
				}
			}
		}
	}
	return true
}

func (s *Service) agentRoleSuggestions(ctx context.Context, agentID uuid.UUID) []domain.TemplateRoleSuggestion {
	if s.roleAdmin == nil {
		return nil
	}
	lister, ok := s.roleAdmin.(interface {
		ListAssignmentsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentRole, error)
	})
	if !ok {
		return nil
	}
	roles, err := lister.ListAssignmentsByAgent(ctx, agentID)
	if err != nil {
		return nil
	}
	out := make([]domain.TemplateRoleSuggestion, 0, len(roles))
	for _, r := range roles {
		var areas []string
		if len(r.Assignments) > 0 {
			areas = r.Assignments[0].Areas
		}
		out = append(out, domain.TemplateRoleSuggestion{Key: r.Key, Areas: areas})
	}
	return out
}

func (s *Service) agentSubscriptionColumns(ctx context.Context, agentID uuid.UUID) []domain.TaskColumn {
	if s.boardConfig == nil {
		return nil
	}
	slugs, err := s.boardConfig.ListAgentSubscriptions(ctx, agentID)
	if err != nil {
		return nil
	}
	out := make([]domain.TaskColumn, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, domain.TaskColumn(slug))
	}
	return out
}

func (s *Service) uniqueAgentName(ctx context.Context, base string) string {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return base
	}
	taken := make(map[string]bool, len(agents))
	for _, a := range agents {
		taken[a.Name] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
	return base + "-" + uuid.NewString()[:8]
}
