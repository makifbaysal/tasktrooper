package skill

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCatalog struct {
	port.CatalogStore
	agent  domain.Agent
	skills []domain.Skill
	stacks []domain.TechStack
}

func (f *fakeCatalog) GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error) {
	return f.agent, nil
}

func (f *fakeCatalog) ListSkillsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	return f.skills, nil
}

type fakeCreator struct {
	created *domain.CreateSkillRequest
	source  catalog.VersionSource
}

func (f *fakeCreator) CreateSkillForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error) {
	f.created = &req
	f.source = catalog.VersionSourceFrom(ctx)
	return domain.Skill{
		ID: uuid.New(), AgentID: agentID, Name: req.Name, Description: req.Description,
		Category: req.Category, Content: req.Content, Enabled: req.Enabled,
	}, nil
}

type fakeEvolution struct {
	port.AgentEvolutionStore
	event *domain.AgentEvolutionEvent
}

func (f *fakeEvolution) CreateEvent(ctx context.Context, e domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error) {
	f.event = &e
	return e, nil
}

func agentCtx(id uuid.UUID) context.Context {
	return registry.ContextWithAgentID(context.Background(), id)
}

func createArgs(name string) string {
	raw, _ := json.Marshal(map[string]string{
		"name":        name,
		"description": "when to use it",
		"category":    "backend",
		"content":     "step 1, step 2",
	})
	return string(raw)
}

func TestNewExecutors_CreateSkillOnlyWithCreator(t *testing.T) {
	kit := &ToolKit{Catalog: &fakeCatalog{}}
	names := executorNames(NewExecutors(kit))
	assert.Equal(t, []string{"load_skill"}, names)

	kit.Creator = &fakeCreator{}
	names = executorNames(NewExecutors(kit))
	assert.Equal(t, []string{"load_skill", "create_skill"}, names)
}

func executorNames(executors []port.ToolExecutor) []string {
	names := make([]string, 0, len(executors))
	for _, ex := range executors {
		names = append(names, ex.Name())
	}
	return names
}

func TestCreateSkill_CreatesAndRecordsEvent(t *testing.T) {
	agentID := uuid.New()
	creator := &fakeCreator{}
	evo := &fakeEvolution{}
	tool := &createSkillTool{kit: &ToolKit{
		Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}},
		Creator: creator,
		Events:  evo,
	}}

	result := tool.Execute(agentCtx(agentID), createArgs("terraform-state-recovery"))

	require.False(t, result.IsError, result.Content)
	require.NotNil(t, creator.created)
	assert.Equal(t, "terraform-state-recovery", creator.created.Name)
	assert.True(t, creator.created.Enabled)
	assert.Equal(t, domain.CatalogVersionSourceEvolution, creator.source.Source,
		"a skill the agent wrote for itself must not land in version history as a user edit")

	require.NotNil(t, evo.event)
	assert.Equal(t, domain.EvolutionChangeSkillCreated, evo.event.ChangeType)
	assert.Equal(t, domain.EvolutionTargetSkill, evo.event.TargetKind)
	assert.Equal(t, agentID, evo.event.AgentID)
	assert.Nil(t, evo.event.ReflectionID)
	assert.NotEmpty(t, evo.event.After)
}

func TestCreateSkill_RefusedWhenSelfEvolutionDisabled(t *testing.T) {
	agentID := uuid.New()
	creator := &fakeCreator{}
	tool := &createSkillTool{kit: &ToolKit{
		Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: false}},
		Creator: creator,
	}}

	result := tool.Execute(agentCtx(agentID), createArgs("anything"))

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "self-evolution is disabled")
	assert.Nil(t, creator.created)
}

func TestCreateSkill_DuplicateNameRefused(t *testing.T) {
	agentID := uuid.New()
	creator := &fakeCreator{}
	tool := &createSkillTool{kit: &ToolKit{
		Catalog: &fakeCatalog{
			agent:  domain.Agent{ID: agentID, SelfEvolutionEnabled: true},
			skills: []domain.Skill{{Name: "Terraform-State-Recovery"}},
		},
		Creator: creator,
	}}

	result := tool.Execute(agentCtx(agentID), createArgs("terraform-state-recovery"))

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "already exists")
	assert.Contains(t, result.Content, "load_skill")
	assert.Nil(t, creator.created)
}

func TestCreateSkill_RequiresAgentContextAndFields(t *testing.T) {
	tool := &createSkillTool{kit: &ToolKit{Catalog: &fakeCatalog{}, Creator: &fakeCreator{}}}

	noCtx := tool.Execute(context.Background(), createArgs("x"))
	assert.True(t, noCtx.IsError)
	assert.Contains(t, noCtx.Content, "agent context")

	missing := tool.Execute(agentCtx(uuid.New()), `{"name":"x","description":"","content":"y"}`)
	assert.True(t, missing.IsError)
	assert.Contains(t, missing.Content, "required")
}

func TestCreateSkill_EventFailureDoesNotFailCall(t *testing.T) {
	agentID := uuid.New()
	tool := &createSkillTool{kit: &ToolKit{
		Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}},
		Creator: &fakeCreator{},
		Events:  nil,
	}}

	result := tool.Execute(agentCtx(agentID), createArgs("no-events-wired"))

	assert.False(t, result.IsError, result.Content)
}

func (f *fakeCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return f.stacks, nil
}

func createArgsWithStack(name, stack string) string {
	raw, _ := json.Marshal(map[string]string{
		"name":        name,
		"description": "when to use it",
		"content":     "step 1, step 2",
		"tech_stack":  stack,
	})
	return string(raw)
}

func TestCreateSkill_FilesUnderNamedTechStack(t *testing.T) {
	agentID := uuid.New()
	django := domain.TechStack{ID: uuid.New(), Name: "Django"}
	creator := &fakeCreator{}
	kit := &ToolKit{
		Catalog: &fakeCatalog{
			agent:  domain.Agent{ID: agentID, SelfEvolutionEnabled: true},
			stacks: []domain.TechStack{django},
		},
		Creator: creator,
	}
	tool := &createSkillTool{kit: kit}

	// Case-insensitive, because the model retypes the name from its index.
	res := tool.Execute(agentCtx(agentID), createArgsWithStack("drf-viewsets", "django"))
	require.False(t, res.IsError, res.Content)
	require.NotNil(t, creator.created.TechStackID)
	assert.Equal(t, django.ID, *creator.created.TechStackID)

	creator.created = nil
	res = tool.Execute(agentCtx(agentID), createArgs("general-skill"))
	require.False(t, res.IsError, res.Content)
	assert.Nil(t, creator.created.TechStackID, "no tech_stack means a general skill")

	creator.created = nil
	res = tool.Execute(agentCtx(agentID), createArgsWithStack("flutter-widgets", "Flutter"))
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "Django", "the refusal lists the stacks that do exist")
	assert.Nil(t, creator.created, "an unknown stack creates nothing")
}
