package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAgentFromTemplate_QAOwnsItsFourColumns(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	qaTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "qa-agent", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{
			domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
			domain.TaskColumnDone, domain.TaskColumnReleased,
		},
	})
	require.NoError(t, err)
	agent, err := svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{
			string(domain.TaskColumnReadyForQA), string(domain.TaskColumnInQA),
			string(domain.TaskColumnDone), string(domain.TaskColumnReleased),
		},
		board.subs[agent.ID])
}

func TestCreateAgentFromTemplate_ArchitectAndPMGetTheirColumn(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	architectTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "system-architect", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnCodeReview},
	})
	require.NoError(t, err)
	architect, err := svc.CreateAgentFromTemplate(ctx, architectTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{string(domain.TaskColumnCodeReview)}, board.subs[architect.ID])

	pmTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "product-manager", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnPMUAT},
	})
	require.NoError(t, err)
	pm, err := svc.CreateAgentFromTemplate(ctx, pmTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{string(domain.TaskColumnPMUAT)}, board.subs[pm.ID])
}

func TestCreateAgentFromTemplate_SecondAgentFindsColumnsAlreadyOccupied(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	qaTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "qa-agent", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{
			domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
			domain.TaskColumnDone, domain.TaskColumnReleased,
		},
	})
	require.NoError(t, err)
	first, err := svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.NotEmpty(t, board.subs[first.ID])

	second, err := svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{Name: "qa-agent-2"})
	require.NoError(t, err)

	assert.Empty(t, board.subs[second.ID], "the columns are already claimed by the first agent")
}

func TestCreateAgentFromTemplate_EveryDeveloperAndTheDesignerWatchTheQueues(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	queues := []domain.TaskColumn{domain.TaskColumnTodo, domain.TaskColumnNeedRevision}
	for _, name := range []string{"backend-developer", "frontend-developer", "data-scientist", "ui-designer"} {
		tpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
			Name: name, Description: "built-in role", BuiltIn: true, Subscriptions: queues,
		})
		require.NoError(t, err)
		agent, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{string(domain.TaskColumnTodo), string(domain.TaskColumnNeedRevision)}, board.subs[agent.ID],
			"%s: a queue hands each card to its assignee only, so there is no seat to take", name)
	}
}

func TestApplySuggestedSubscriptions_LeavesExistingSubscriptionsAlone(t *testing.T) {
	store := newMemCatalogStore()
	qaID := uuid.New()
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{
		qaID: {string(domain.TaskColumnReadyForQA)},
	}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetBoardConfigStore(board)

	require.NoError(t, svc.applySuggestedSubscriptions(context.Background(), domain.Agent{ID: qaID, Name: "qa-agent"},
		[]domain.TaskColumn{domain.TaskColumnReadyForQA, domain.TaskColumnInQA, domain.TaskColumnDone, domain.TaskColumnReleased}))

	assert.Equal(t, []string{string(domain.TaskColumnReadyForQA)}, board.subs[qaID])
}

func TestApplySuggestedRoles_FillsOnlyVacantAreas(t *testing.T) {
	developerID := uuid.New()
	backendAgent := domain.Agent{ID: uuid.New(), Name: "backend-developer"}
	roles := &memRoleAdmin{roles: map[uuid.UUID]domain.AgentRole{
		developerID: {ID: developerID, Key: "developer"},
	}}
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetRoleAdmin(roles)

	require.NoError(t, svc.applySuggestedRoles(context.Background(), backendAgent,
		[]domain.TemplateRoleSuggestion{{Key: "developer", Areas: []string{domain.RepoKindBackend}}}))
	assert.Len(t, roles.roles[developerID].Assignments, 1)
	assert.Equal(t, backendAgent.ID, roles.roles[developerID].Assignments[0].AgentID)

	secondBackend := domain.Agent{ID: uuid.New(), Name: "backend-developer-2"}
	require.NoError(t, svc.applySuggestedRoles(context.Background(), secondBackend,
		[]domain.TemplateRoleSuggestion{{Key: "developer", Areas: []string{domain.RepoKindBackend}}}))
	assert.Len(t, roles.roles[developerID].Assignments, 1, "backend is already covered")

	frontendAgent := domain.Agent{ID: uuid.New(), Name: "frontend-developer"}
	require.NoError(t, svc.applySuggestedRoles(context.Background(), frontendAgent,
		[]domain.TemplateRoleSuggestion{{Key: "developer", Areas: []string{domain.RepoKindFrontend}}}))
	assert.Len(t, roles.roles[developerID].Assignments, 2)
}

func TestApplySuggestedRoles_SkipsUnknownRole(t *testing.T) {
	roles := &memRoleAdmin{roles: map[uuid.UUID]domain.AgentRole{}}
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetRoleAdmin(roles)

	err := svc.applySuggestedRoles(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.TemplateRoleSuggestion{{Key: "no-such-role"}})

	require.NoError(t, err)
}

// Also ListAssignmentsByAgent, agentRoleSuggestions' optional read-back interface.
type memRoleAdmin struct {
	roles map[uuid.UUID]domain.AgentRole
}

func (m *memRoleAdmin) ListRoles(context.Context) ([]domain.AgentRole, error) {
	out := make([]domain.AgentRole, 0, len(m.roles))
	for _, r := range m.roles {
		out = append(out, r)
	}
	return out, nil
}

func (m *memRoleAdmin) SetRoleAssignments(_ context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error {
	r := m.roles[roleID]
	r.Assignments = assignments
	m.roles[roleID] = r
	return nil
}

func (m *memRoleAdmin) ListAssignmentsByAgent(_ context.Context, agentID uuid.UUID) ([]domain.AgentRole, error) {
	var out []domain.AgentRole
	for _, r := range m.roles {
		for _, a := range r.Assignments {
			if a.AgentID == agentID {
				out = append(out, domain.AgentRole{ID: r.ID, Key: r.Key, Name: r.Name, Assignments: []domain.RoleAssignment{a}})
			}
		}
	}
	return out, nil
}

type memBoardConfigStore struct {
	subs         map[uuid.UUID][]string
	instructions map[uuid.UUID]map[string]domain.AgentColumnInstruction
}

func (m *memBoardConfigStore) GetSettings(context.Context) (domain.BoardSettings, error) {
	return domain.BoardSettings{}, nil
}

func (m *memBoardConfigStore) UpdateSettings(context.Context, string) (domain.BoardSettings, error) {
	return domain.BoardSettings{}, nil
}

func (m *memBoardConfigStore) ListColumns(context.Context) ([]domain.BoardColumn, error) {
	return nil, nil
}

func (m *memBoardConfigStore) ReplaceColumns(context.Context, []domain.BoardColumnInput) error {
	return nil
}

func (m *memBoardConfigStore) ListMembers(context.Context) ([]domain.BoardMember, error) {
	return nil, nil
}

func (m *memBoardConfigStore) SetMembers(context.Context, []uuid.UUID) error { return nil }

func (m *memBoardConfigStore) ListSubscriptions(context.Context) ([]domain.BoardSubscription, error) {
	var out []domain.BoardSubscription
	for agentID, slugs := range m.subs {
		for _, slug := range slugs {
			out = append(out, domain.BoardSubscription{AgentID: agentID, ColumnSlug: slug})
		}
	}
	return out, nil
}

func (m *memBoardConfigStore) SetSubscriptions(context.Context, []domain.BoardSubscriptionInput) error {
	return nil
}

func (m *memBoardConfigStore) ListAgentSubscriptions(_ context.Context, agentID uuid.UUID) ([]string, error) {
	return m.subs[agentID], nil
}

func (m *memBoardConfigStore) SetAgentSubscriptions(_ context.Context, agentID uuid.UUID, columnSlugs []string) error {
	m.subs[agentID] = append([]string(nil), columnSlugs...)
	return nil
}

func (m *memBoardConfigStore) ListAgentSubscriptionsDetailed(_ context.Context, agentID uuid.UUID) ([]domain.AgentColumnSubscription, error) {
	slugs := m.subs[agentID]
	out := make([]domain.AgentColumnSubscription, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, domain.AgentColumnSubscription{ColumnSlug: slug})
	}
	return out, nil
}

func (m *memBoardConfigStore) SetAgentSubscriptionsDetailed(_ context.Context, agentID uuid.UUID, subs []domain.AgentColumnSubscription) error {
	slugs := make([]string, 0, len(subs))
	for _, s := range subs {
		slugs = append(slugs, s.ColumnSlug)
	}
	m.subs[agentID] = slugs
	return nil
}

func (m *memBoardConfigStore) ListTransitions(context.Context) ([]domain.BoardTransition, error) {
	return nil, nil
}

func (m *memBoardConfigStore) ListAgentColumnInstructions(_ context.Context, agentID uuid.UUID) ([]domain.AgentColumnInstruction, error) {
	per := m.instructions[agentID]
	out := make([]domain.AgentColumnInstruction, 0, len(per))
	for slug, ins := range per {
		ins.ColumnSlug = slug
		out = append(out, ins)
	}
	return out, nil
}

// SetAgentColumnInstruction is the operator path: it always clears
// CatalogSHA, mirroring the postgres store's own rule.
func (m *memBoardConfigStore) SetAgentColumnInstruction(_ context.Context, agentID uuid.UUID, columnSlug, instruction string) error {
	if instruction == "" {
		if m.instructions[agentID] != nil {
			delete(m.instructions[agentID], columnSlug)
		}
		return nil
	}
	m.setInstruction(agentID, columnSlug, instruction, "")
	return nil
}

func (m *memBoardConfigStore) SetCatalogColumnInstruction(_ context.Context, agentID uuid.UUID, columnSlug, instruction, sha string) error {
	m.setInstruction(agentID, columnSlug, instruction, sha)
	return nil
}

func (m *memBoardConfigStore) DeleteAgentColumnInstruction(_ context.Context, agentID uuid.UUID, columnSlug string) error {
	if m.instructions[agentID] != nil {
		delete(m.instructions[agentID], columnSlug)
	}
	return nil
}

func (m *memBoardConfigStore) setInstruction(agentID uuid.UUID, columnSlug, instruction, sha string) {
	if m.instructions == nil {
		m.instructions = map[uuid.UUID]map[string]domain.AgentColumnInstruction{}
	}
	if m.instructions[agentID] == nil {
		m.instructions[agentID] = map[string]domain.AgentColumnInstruction{}
	}
	m.instructions[agentID][columnSlug] = domain.AgentColumnInstruction{
		AgentID: agentID, ColumnSlug: columnSlug, Instruction: instruction, CatalogSHA: sha,
	}
}

func (m *memBoardConfigStore) SetTransitions(context.Context, []domain.BoardTransition) error {
	return nil
}

func (m *memBoardConfigStore) AgentsForColumn(context.Context, string, string) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *memBoardConfigStore) ValidateColumnSlug(context.Context, string) (bool, error) {
	return true, nil
}

func TestCreateAgentFromTemplate_SecondReviewerJoinsCodeReview(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	architectTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "system-architect", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnCodeReview},
	})
	require.NoError(t, err)
	architect, err := svc.CreateAgentFromTemplate(ctx, architectTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{string(domain.TaskColumnCodeReview)}, board.subs[architect.ID])

	qaTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "qa-agent", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnReadyForQA},
	})
	require.NoError(t, err)
	_, err = svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)

	securityTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "security-agent", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA},
	})
	require.NoError(t, err)
	security, err := svc.CreateAgentFromTemplate(ctx, securityTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)

	assert.Equal(t, []string{string(domain.TaskColumnCodeReview)}, board.subs[security.ID],
		"code_review takes every reviewer; ready_for_qa stays a single seat")
	assert.Equal(t, []string{string(domain.TaskColumnCodeReview)}, board.subs[architect.ID], "the architect keeps its seat")
}
