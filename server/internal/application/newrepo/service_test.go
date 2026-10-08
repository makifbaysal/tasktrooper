package newrepo

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeCreator struct {
	calls []domain.CreateRepositoryRequest
	err   error
	repo  domain.Repository
}

func (f *fakeCreator) CreateWithoutScan(_ context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return domain.Repository{}, f.err
	}
	name, err := domain.NewRepoDirName(req.Name)
	if err != nil {
		return domain.Repository{}, err
	}
	f.repo = domain.Repository{ID: uuid.New(), Name: name, Description: req.Description, Kind: req.Kind, ProjectIDs: req.ProjectIDs}
	return f.repo, nil
}

func (f *fakeCreator) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

type fakeComponents struct {
	added   []domain.NewComponentRequest
	patches []domain.ComponentPatch
	comp    domain.Component
	addErr  error
}

func (f *fakeComponents) AddComponent(_ context.Context, repoID uuid.UUID, req domain.NewComponentRequest) (domain.Component, error) {
	f.added = append(f.added, req)
	if f.addErr != nil {
		return domain.Component{}, f.addErr
	}
	role, name := req.Role, req.Name
	f.comp = domain.Component{
		ID:            uuid.New(),
		RepositoryID:  repoID,
		Path:          req.Path,
		Name:          domain.Fact[string]{Override: &name},
		Role:          domain.Fact[domain.ComponentRole]{Override: &role},
		Status:        domain.ComponentStatusActive,
		ManuallyAdded: true,
	}
	return f.comp, nil
}

func (f *fakeComponents) UpdateComponent(_ context.Context, _ uuid.UUID, patch domain.ComponentPatch) (domain.Component, error) {
	f.patches = append(f.patches, patch)
	if patch.Docs != nil {
		f.comp.Docs = *patch.Docs
	}
	return f.comp, nil
}

type fakeTasks struct {
	reqs []domain.CreateBoardTaskRequest
}

func (f *fakeTasks) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.reqs = append(f.reqs, req)
	return domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "T-7", Title: req.Title, Description: req.Description, Column: req.Column}, nil
}

type fakeRoles struct {
	agent *uuid.UUID
	areas []string
}

func (f *fakeRoles) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return nil, nil
}

func (f *fakeRoles) AgentForPurpose(_ context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error) {
	if purpose != domain.PurposeSystemTaskAssignee {
		return nil, errors.New("unexpected purpose")
	}
	f.areas = append(f.areas, area)
	return f.agent, nil
}

func (f *fakeRoles) AgentArea(context.Context, uuid.UUID) string { return "" }

func (f *fakeRoles) AgentAreas(context.Context, uuid.UUID) []string { return nil }

func (f *fakeRoles) AssigneeForNewTask(context.Context, domain.TaskType, string, *uuid.UUID) (*uuid.UUID, error) {
	return nil, nil
}

type harness struct {
	svc        *Service
	creator    *fakeCreator
	components *fakeComponents
	tasks      *fakeTasks
	roles      *fakeRoles
}

func newHarness() harness {
	agent := uuid.New()
	h := harness{
		creator:    &fakeCreator{},
		components: &fakeComponents{},
		tasks:      &fakeTasks{},
		roles:      &fakeRoles{agent: &agent},
	}
	h.svc = NewService(h.creator, h.components, h.tasks)
	h.svc.SetRoleResolver(h.roles)
	return h
}

func TestCreateRejectsInvalidRequestsBeforeCreatingAnything(t *testing.T) {
	tests := []struct {
		name string
		req  domain.NewRepositoryRequest
		want string
	}{
		{"missing role", domain.NewRepositoryRequest{Name: "app"}, "role is required"},
		{"unknown role", domain.NewRepositoryRequest{Name: "app", Role: "wizard"}, `unknown role "wizard"`},
		{"unknown doc kind", domain.NewRepositoryRequest{Name: "app", Role: domain.ComponentRoleBackend, Docs: []string{"vibes"}}, `unknown doc kind "vibes"`},
		{"missing name", domain.NewRepositoryRequest{Role: domain.ComponentRoleBackend}, "name is required"},
		{"unusable name", domain.NewRepositoryRequest{Name: "../..", Role: domain.ComponentRoleBackend}, "no letters or digits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			_, err := h.svc.Create(context.Background(), tt.req)
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, h.creator.calls)
			require.Empty(t, h.components.added)
			require.Empty(t, h.tasks.reqs)
		})
	}
}

func TestCreateSetsUpTheRootComponentAndTheBootstrapTask(t *testing.T) {
	h := newHarness()
	projectID := uuid.New()
	req := domain.NewRepositoryRequest{
		Name:        "My App",
		Owner:       " acme ",
		Description: "A storefront for hand-made chairs.",
		ProjectIDs:  []uuid.UUID{projectID},
		Role:        domain.ComponentRoleFrontend,
		Stack:       "Next.js 15, TypeScript",
		Notes:       "Use pnpm.\nNo CSS-in-JS.",
		Scaffold:    true,
		Docs:        []string{domain.RepoDocCodingStandards, domain.RepoDocLocalRun, domain.RepoDocCodingStandards},
	}

	res, err := h.svc.Create(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, h.creator.calls, 1)
	call := h.creator.calls[0]
	require.Equal(t, "acme", call.Owner)
	require.Equal(t, req.Description, call.Description)
	require.Equal(t, []uuid.UUID{projectID}, call.ProjectIDs)
	require.Equal(t, domain.RepoKindFrontend, call.Kind)
	require.Equal(t, "my-app", res.Repository.Name)

	require.Equal(t, []domain.NewComponentRequest{{Path: ".", Name: "my-app", Role: domain.ComponentRoleFrontend}}, h.components.added)
	require.Equal(t, h.components.comp.ID, res.ComponentID)
	require.Equal(t, domain.RepositoryDocs{CodingStandards: ".ai/coding-standards.md", LocalRun: "scripts/dev.sh"}, h.components.comp.Docs)

	require.NotNil(t, res.Task)
	require.Len(t, h.tasks.reqs, 1)
	task := h.tasks.reqs[0]
	require.Equal(t, "Set up my-app", task.Title)
	require.Equal(t, domain.TaskColumnTodo, task.Column)
	require.Equal(t, domain.TaskPriorityMedium, task.Priority)
	require.Equal(t, "system", task.CreatedBy)
	require.Equal(t, h.roles.agent, task.AssigneeAgentID)
	require.Equal(t, []string{domain.RepoKindFrontend}, h.roles.areas)
	require.NotNil(t, task.ComponentID)
	require.Equal(t, res.ComponentID, *task.ComponentID)

	for _, want := range []string{
		req.Description, "frontend", req.Stack, req.Notes,
		"search_boilerplate_catalog", "README",
		"`.ai/coding-standards.md`", "Prescribe the conventions",
		"`scripts/dev.sh`", "set -euo pipefail", "idempotent",
		"`CLAUDE.md` and `AGENTS.md`", "exactly one pull request",
	} {
		require.Contains(t, task.Description, want)
	}
	require.NotContains(t, task.Description, "`.ai/test-standards.md`")
	require.NotContains(t, task.Description, "describe what it does, don't prescribe")
}

func TestCreateWithDocsOnlyLeavesTheSkeletonOut(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Create(context.Background(), domain.NewRepositoryRequest{
		Name: "api", Role: domain.ComponentRoleBackend, Docs: []string{domain.RepoDocArchitecture},
	})
	require.NoError(t, err)
	require.NotNil(t, res.Task)
	desc := h.tasks.reqs[0].Description
	require.Contains(t, desc, "`.ai/architecture.md`")
	require.Contains(t, desc, "Prescribe the architecture")
	require.NotContains(t, desc, "search_boilerplate_catalog")
	require.Contains(t, desc, "(not given)")
	require.Equal(t, []string{domain.RepoKindBackend}, h.roles.areas)
}

func TestCreateWithNothingRequestedOpensNoTask(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Create(context.Background(), domain.NewRepositoryRequest{Name: "lib", Role: domain.ComponentRoleLibrary})
	require.NoError(t, err)
	require.Nil(t, res.Task)
	require.Empty(t, h.tasks.reqs)
	require.Empty(t, h.components.patches)
	require.Len(t, h.components.added, 1)
}

func TestCreatePassesGitFailuresThroughUnwrapped(t *testing.T) {
	h := newHarness()
	h.creator.err = errors.New("git/GitHub setup failed: github repo create: name already exists")
	_, err := h.svc.Create(context.Background(), domain.NewRepositoryRequest{Name: "app", Role: domain.ComponentRoleBackend, Scaffold: true})
	require.EqualError(t, err, h.creator.err.Error())
	var incomplete *IncompleteError
	require.False(t, errors.As(err, &incomplete))
	require.Empty(t, h.components.added)
	require.Empty(t, h.tasks.reqs)
}

func TestCreateReportsAFailureAfterTheRepositoryExistsAsIncomplete(t *testing.T) {
	h := newHarness()
	h.components.addErr = errors.New("db down")
	_, err := h.svc.Create(context.Background(), domain.NewRepositoryRequest{Name: "app", Role: domain.ComponentRoleBackend, Scaffold: true})
	var incomplete *IncompleteError
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, "app", incomplete.Repository.Name)
	require.ErrorContains(t, err, `repository "app" was created`)
	require.Empty(t, h.tasks.reqs)
}
