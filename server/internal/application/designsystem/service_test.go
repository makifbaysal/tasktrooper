package designsystem

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type memStore struct {
	rows     []domain.DesignSystem
	requests []domain.DesignSystemRequest
	base     map[uuid.UUID]uuid.UUID
}

func newMemStore() *memStore { return &memStore{base: map[uuid.UUID]uuid.UUID{}} }

func (m *memStore) Create(_ context.Context, d domain.DesignSystem) (domain.DesignSystem, error) {
	max := 0
	for _, r := range m.rows {
		if r.Scope == d.Scope && r.TargetID() == d.TargetID() && r.Version > max {
			max = r.Version
		}
	}
	d.ID = uuid.New()
	d.Version = max + 1
	d.CreatedAt = time.Now()
	m.rows = append(m.rows, d)
	return d, nil
}

func (m *memStore) UpdateContent(_ context.Context, d domain.DesignSystem) (domain.DesignSystem, error) {
	for i := range m.rows {
		if m.rows[i].ID == d.ID {
			m.rows[i].DesignMD, m.rows[i].Tokens, m.rows[i].InventoryMD, m.rows[i].Rationale = d.DesignMD, d.Tokens, d.InventoryMD, d.Rationale
			return m.rows[i], nil
		}
	}
	return domain.DesignSystem{}, domain.ErrDesignSystemNotFound
}

func (m *memStore) Get(_ context.Context, id uuid.UUID) (domain.DesignSystem, error) {
	for _, r := range m.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.DesignSystem{}, domain.ErrDesignSystemNotFound
}

func (m *memStore) list(match func(domain.DesignSystem) bool) []domain.DesignSystem {
	out := []domain.DesignSystem{}
	for _, r := range m.rows {
		if match(r) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out
}

func (m *memStore) ListForProject(_ context.Context, id uuid.UUID) ([]domain.DesignSystem, error) {
	return m.list(func(d domain.DesignSystem) bool { return d.Scope == domain.DesignSystemScopeProject && d.TargetID() == id }), nil
}

func (m *memStore) ListForRepository(_ context.Context, id uuid.UUID) ([]domain.DesignSystem, error) {
	return m.list(func(d domain.DesignSystem) bool { return d.Scope == domain.DesignSystemScopeRepository && d.TargetID() == id }), nil
}

func (m *memStore) ListBySourceTask(_ context.Context, id uuid.UUID) ([]domain.DesignSystem, error) {
	return m.list(func(d domain.DesignSystem) bool { return d.SourceTaskID != nil && *d.SourceTaskID == id }), nil
}

func (m *memStore) Approve(_ context.Context, id uuid.UUID) (domain.DesignSystem, error) {
	var target *domain.DesignSystem
	for i := range m.rows {
		if m.rows[i].ID == id {
			target = &m.rows[i]
		}
	}
	if target == nil {
		return domain.DesignSystem{}, domain.ErrDesignSystemNotFound
	}
	for i := range m.rows {
		if m.rows[i].Status == domain.DesignSystemApproved && m.rows[i].Scope == target.Scope && m.rows[i].TargetID() == target.TargetID() {
			m.rows[i].Status = domain.DesignSystemSuperseded
		}
	}
	target.Status = domain.DesignSystemApproved
	return *target, nil
}

func (m *memStore) CreateRequest(_ context.Context, r domain.DesignSystemRequest) (domain.DesignSystemRequest, error) {
	r.ID = uuid.New()
	r.CreatedAt = time.Now()
	m.requests = append(m.requests, r)
	return r, nil
}

func (m *memStore) latest(match func(domain.DesignSystemRequest) bool) *domain.DesignSystemRequest {
	for i := len(m.requests) - 1; i >= 0; i-- {
		if match(m.requests[i]) {
			r := m.requests[i]
			return &r
		}
	}
	return nil
}

func (m *memStore) LatestRequestForProject(_ context.Context, id uuid.UUID) (*domain.DesignSystemRequest, error) {
	return m.latest(func(r domain.DesignSystemRequest) bool { return r.ProjectID != nil && *r.ProjectID == id }), nil
}

func (m *memStore) LatestRequestForRepository(_ context.Context, id uuid.UUID) (*domain.DesignSystemRequest, error) {
	return m.latest(func(r domain.DesignSystemRequest) bool { return r.RepositoryID != nil && *r.RepositoryID == id }), nil
}

func (m *memStore) RepositoryBaseProject(_ context.Context, id uuid.UUID) (*uuid.UUID, error) {
	if p, ok := m.base[id]; ok {
		return &p, nil
	}
	return nil, nil
}

func (m *memStore) SetRepositoryBaseProject(_ context.Context, id uuid.UUID, p *uuid.UUID) error {
	if p == nil {
		delete(m.base, id)
		return nil
	}
	m.base[id] = *p
	return nil
}

type memProjects map[uuid.UUID]domain.InitiativeProject

func (m memProjects) Get(_ context.Context, id uuid.UUID) (domain.InitiativeProject, error) {
	p, ok := m[id]
	if !ok {
		return domain.InitiativeProject{}, errors.New("project not found")
	}
	return p, nil
}

type memRepos []domain.Repository

func (m memRepos) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	for _, r := range m {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.Repository{}, errors.New("repository not found")
}

func (m memRepos) List(context.Context) ([]domain.Repository, error) { return m, nil }

type memTasks struct {
	created []domain.CreateBoardTaskRequest
	tasks   map[uuid.UUID]domain.BoardTask
	hosts   []uuid.UUID
}

func (m *memTasks) CreateTask(_ context.Context, repoID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	m.created = append(m.created, req)
	m.hosts = append(m.hosts, repoID)
	task := domain.BoardTask{ID: uuid.New(), Key: "D-1", Title: req.Title, TaskType: req.TaskType, Column: req.Column}
	m.tasks[task.ID] = task
	return task, nil
}

func (m *memTasks) GetTask(_ context.Context, _, id uuid.UUID) (domain.BoardTask, error) {
	t, ok := m.tasks[id]
	if !ok {
		return domain.BoardTask{}, errors.New("task not found")
	}
	return t, nil
}

type fixture struct {
	svc      *Service
	store    *memStore
	tasks    *memTasks
	project  uuid.UUID
	other    uuid.UUID
	web      domain.Repository
	site     domain.Repository
	backend  domain.Repository
	designer uuid.UUID
}

func newFixture() fixture {
	project, other := uuid.New(), uuid.New()
	web := domain.Repository{ID: uuid.New(), Name: "web", Kind: domain.RepoKindFrontend, ProjectIDs: []uuid.UUID{project}}
	site := domain.Repository{ID: uuid.New(), Name: "site", Kind: domain.RepoKindFrontend, ProjectIDs: []uuid.UUID{project, other}}
	backend := domain.Repository{ID: uuid.New(), Name: "api", Kind: domain.RepoKindBackend, ProjectIDs: []uuid.UUID{project}}
	store := newMemStore()
	tasks := &memTasks{tasks: map[uuid.UUID]domain.BoardTask{}}
	svc := NewService(store, memProjects{
		project: {ID: project, Name: "TaskTrooper"},
		other:   {ID: other, Name: "Marketing"},
	}, memRepos{backend, web, site})
	svc.SetTaskCreator(tasks)
	return fixture{svc: svc, store: store, tasks: tasks, project: project, other: other, web: web, site: site, backend: backend}
}

func (f fixture) approveBase(t *testing.T, projectID uuid.UUID, tokens string) domain.DesignSystem {
	t.Helper()
	ds, err := f.svc.Propose(context.Background(), domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeProject, ProjectID: &projectID, TaskID: uuid.New(),
		DesignMD: "## Overview\nCalm surfaces.", Tokens: json.RawMessage(tokens),
	})
	require.NoError(t, err)
	approved := f.svc.ApproveForTask(context.Background(), *ds.SourceTaskID)
	require.Len(t, approved, 1)
	return approved[0]
}

func TestRequestForProjectOpensOneDesignTaskInAUIRepository(t *testing.T) {
	f := newFixture()
	ctx := context.Background()

	res, err := f.svc.RequestForProject(ctx, f.project, "keep the navy")
	require.NoError(t, err)
	assert.True(t, res.Created)
	require.Len(t, f.tasks.created, 1)
	req := f.tasks.created[0]
	assert.Equal(t, domain.TaskTypeDesign, req.TaskType)
	assert.Equal(t, domain.TaskColumnTodo, req.Column)
	assert.Equal(t, f.web.ID, f.tasks.hosts[0], "a project's design task lives in a UI repository")
	assert.Equal(t, &f.project, req.InitiativeProjectID)
	assert.Contains(t, req.Description, "Derive the design system of the **TaskTrooper** project")
	assert.Contains(t, req.Description, f.site.ID.String())
	assert.Contains(t, req.Description, "keep the navy")

	again, err := f.svc.RequestForProject(ctx, f.project, "")
	require.NoError(t, err)
	assert.False(t, again.Created, "a second click while the task is open returns it")
	assert.Equal(t, res.Task.ID, again.Task.ID)
	assert.Len(t, f.tasks.created, 1)

	done := f.tasks.tasks[res.Task.ID]
	done.Column = domain.TaskColumnReleased
	f.tasks.tasks[res.Task.ID] = done
	third, err := f.svc.RequestForProject(ctx, f.project, "")
	require.NoError(t, err)
	assert.True(t, third.Created)
}

func TestRequestForProjectWithoutRepositories(t *testing.T) {
	f := newFixture()
	_, err := f.svc.RequestForProject(context.Background(), f.other, "")
	require.NoError(t, err, "other has the site repository")

	lonely := uuid.New()
	f.svc.projects.(memProjects)[lonely] = domain.InitiativeProject{ID: lonely, Name: "Empty"}
	_, err = f.svc.RequestForProject(context.Background(), lonely, "")
	assert.ErrorIs(t, err, domain.ErrDesignSystemNoRepository)
}

func TestProposeReplacesTheTasksPendingVersion(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	taskID := uuid.New()
	first, err := f.svc.Propose(ctx, domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeProject, ProjectID: &f.project, TaskID: taskID, TaskKey: "D-1",
		DesignMD: "v1", Tokens: json.RawMessage(`{"color":{"primary":{"$value":"#15214b"}}}`),
	})
	require.NoError(t, err)
	second, err := f.svc.Propose(ctx, domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeProject, ProjectID: &f.project, TaskID: taskID, TaskKey: "D-1",
		DesignMD: "v1 revised", Tokens: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "v1 revised", second.DesignMD)
	assert.Len(t, f.store.rows, 1)
}

func TestProposeValidatesLayers(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	base := domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeRepository, RepositoryID: &f.site.ID, TaskID: uuid.New(),
		Tokens: json.RawMessage(`{"color":{"accent":{"$value":"#f0b86e"}}}`),
	}

	_, err := f.svc.Propose(ctx, base)
	assert.ErrorIs(t, err, domain.ErrDesignSystemInvalid, "a layer needs a rationale")

	withNull := base
	withNull.Rationale = "marketing identity"
	withNull.Tokens = json.RawMessage(`{"color":{"primary":null}}`)
	_, err = f.svc.Propose(ctx, withNull)
	assert.ErrorIs(t, err, domain.ErrDesignSystemInvalid, "a layer cannot remove a token")

	ok := base
	ok.Rationale = "marketing identity"
	ds, err := f.svc.Propose(ctx, ok)
	require.NoError(t, err)
	assert.Equal(t, domain.DesignSystemInReview, ds.Status)

	_, err = f.svc.Propose(ctx, domain.DesignSystemProposal{Scope: domain.DesignSystemScopeProject, ProjectID: &f.project, TaskID: uuid.New()})
	assert.ErrorIs(t, err, domain.ErrDesignSystemInvalid, "a base needs design_md")

	_, err = f.svc.Propose(ctx, domain.DesignSystemProposal{Scope: "team", TaskID: uuid.New()})
	assert.ErrorIs(t, err, domain.ErrDesignSystemInvalid)
}

func TestEffectiveMergesTheBaseAndTheRepositoryLayer(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	f.approveBase(t, f.project, `{"color":{"primary":{"$value":"#15214b"},"surface":{"$value":"#fff"}}}`)

	eff, err := f.svc.Effective(ctx, f.web.ID)
	require.NoError(t, err)
	require.NotNil(t, eff.Base)
	assert.Nil(t, eff.Layer)
	assert.Equal(t, "TaskTrooper", eff.Project.Name)

	taskID := uuid.New()
	_, err = f.svc.Propose(ctx, domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeRepository, RepositoryID: &f.site.ID, TaskID: taskID,
		Rationale: "marketing identity", Tokens: json.RawMessage(`{"color":{"primary":{"$value":"#f0b86e"}}}`),
	})
	require.NoError(t, err)
	f.svc.ApproveForTask(ctx, taskID)

	eff, err = f.svc.Effective(ctx, f.site.ID)
	require.NoError(t, err)
	require.NotNil(t, eff.Base, "site's other project has no base, so the only candidate wins")
	require.NotNil(t, eff.Layer)
	assert.JSONEq(t, `{"color":{"primary":{"$value":"#f0b86e"},"surface":{"$value":"#fff"}}}`, string(eff.Tokens))

	view, err := f.svc.RepositoryView(ctx, f.site.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"color.primary"}, view.Overrides)
	assert.Len(t, view.ProjectChoices, 2)
}

func TestTwoBasesAreAmbiguousUntilOneIsChosen(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	f.approveBase(t, f.project, `{"a":{"$value":1}}`)
	f.approveBase(t, f.other, `{"a":{"$value":2}}`)

	eff, err := f.svc.Effective(ctx, f.site.ID)
	require.NoError(t, err)
	assert.True(t, eff.Ambiguous)
	assert.Nil(t, eff.Base)
	assert.Empty(t, f.svc.ContextNote(ctx, f.site.ID, true))

	_, err = f.svc.SetRepositoryBaseProject(ctx, f.site.ID, &f.other)
	require.NoError(t, err)
	eff, err = f.svc.Effective(ctx, f.site.ID)
	require.NoError(t, err)
	assert.False(t, eff.Ambiguous)
	require.NotNil(t, eff.Base)
	assert.JSONEq(t, `{"a":{"$value":2}}`, string(eff.Tokens))

	stranger := uuid.New()
	_, err = f.svc.SetRepositoryBaseProject(ctx, f.site.ID, &stranger)
	assert.ErrorIs(t, err, domain.ErrDesignSystemInvalid)
}

func TestApproveForTaskSupersedesThePreviousBase(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	first := f.approveBase(t, f.project, `{}`)
	second := f.approveBase(t, f.project, `{}`)
	assert.Equal(t, 2, second.Version)

	view, err := f.svc.ProjectView(ctx, f.project)
	require.NoError(t, err)
	require.NotNil(t, view.Current)
	assert.Equal(t, second.ID, view.Current.ID)
	got, err := f.svc.Get(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.DesignSystemSuperseded, got.Status)
	assert.Len(t, view.Repositories, 3)
}

func TestContextNoteNamesTheVersionsAndTruncates(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	assert.Empty(t, f.svc.ContextNote(ctx, f.web.ID, true), "no design system, no note")

	f.approveBase(t, f.project, `{}`)
	note := f.svc.ContextNote(ctx, f.web.ID, true)
	assert.Contains(t, note, "## Design system (TaskTrooper)")
	assert.Contains(t, note, "**TaskTrooper** design system v1")
	assert.Contains(t, note, "`get_design_system`")
	assert.Contains(t, note, "Calm surfaces.")

	noTool := f.svc.ContextNote(ctx, f.web.ID, false)
	assert.NotContains(t, noTool, "get_design_system")
}

func TestRequestForRepositoryWritesTheLayerBrief(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	f.approveBase(t, f.project, `{}`)

	res, err := f.svc.RequestForRepository(ctx, f.web.ID, "")
	require.NoError(t, err)
	assert.True(t, res.Created)
	req := f.tasks.created[0]
	assert.Contains(t, req.Description, "Derive the design system layer of the **web** repository")
	assert.Contains(t, req.Description, "base v1")
	assert.Equal(t, &f.project, req.InitiativeProjectID)

	view, err := f.svc.RepositoryView(ctx, f.web.ID)
	require.NoError(t, err)
	require.NotNil(t, view.Request)
	assert.True(t, view.Request.Open)
}

func TestFilesRenderTheEffectiveDesignSystem(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	files, err := f.svc.Files(ctx, f.web.ID)
	require.NoError(t, err)
	assert.Empty(t, files, "no design system, nothing to write")

	base, err := f.svc.Propose(ctx, domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeProject, ProjectID: &f.project, TaskID: uuid.New(),
		DesignMD: "## Overview\nCalm surfaces.", InventoryMD: "- Button: primary, ghost",
		Tokens: json.RawMessage(`{"color":{"primary":{"$value":"#15214b"}}}`),
	})
	require.NoError(t, err)
	f.svc.ApproveForTask(ctx, *base.SourceTaskID)
	layer, err := f.svc.Propose(ctx, domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeRepository, RepositoryID: &f.site.ID, TaskID: uuid.New(),
		Rationale: "marketing identity", Tokens: json.RawMessage(`{"color":{"primary":{"$value":"#f0b86e"}}}`),
	})
	require.NoError(t, err)
	f.svc.ApproveForTask(ctx, *layer.SourceTaskID)

	files, err = f.svc.Files(ctx, f.site.ID)
	require.NoError(t, err)
	byPath := map[string]string{}
	for _, file := range files {
		byPath[file.Path] = file.Content
	}
	require.Len(t, byPath, 4)
	assert.Contains(t, byPath["DESIGN.md"], "Generated by TaskTrooper from the TaskTrooper design system v1 with the site layer v1")
	assert.Contains(t, byPath["DESIGN.md"], "Calm surfaces.")
	assert.Contains(t, byPath["DESIGN.md"], "Why this repository differs from the base: marketing identity")
	assert.Contains(t, byPath["design/tokens.css"], "--color-primary: #f0b86e;")
	assert.Contains(t, byPath["design/tokens.json"], "\"#f0b86e\"")
	assert.Contains(t, byPath["design/INVENTORY.md"], "- Button: primary, ghost")
}

func TestProposeAndViewsCarryLint(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	ds, err := f.svc.Propose(ctx, domain.DesignSystemProposal{
		Scope: domain.DesignSystemScopeProject, ProjectID: &f.project, TaskID: uuid.New(),
		DesignMD: "## Overview", Tokens: json.RawMessage(`{"color":{"accent":{"$value":"#f0b86e"},"on-accent":{"$value":"#ffffff"}}}`),
	})
	require.NoError(t, err)
	codes := map[string]bool{}
	for _, l := range ds.Lint {
		codes[l.Code] = true
	}
	assert.True(t, codes[domain.DesignLintContrastBelowAA])
	assert.True(t, codes[domain.DesignLintMissingSection])

	view, err := f.svc.ProjectView(ctx, f.project)
	require.NoError(t, err)
	require.Len(t, view.Pending, 1)
	assert.NotEmpty(t, view.Pending[0].Lint)
}

func (m *memTasks) UpdateTask(_ context.Context, _, id uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	t, ok := m.tasks[id]
	if !ok {
		return domain.BoardTask{}, errors.New("task not found")
	}
	if req.AssigneeAgentID.Present {
		t.AssigneeAgentID = req.AssigneeAgentID.Value
	}
	m.tasks[id] = t
	return t, nil
}

type stubDesigner struct{ id *uuid.UUID }

func (s *stubDesigner) AssigneeForNewTask(context.Context, domain.TaskType, string, *uuid.UUID) (*uuid.UUID, error) {
	return s.id, nil
}

func TestRequestWaitsForADesignerAndHandsTheTaskOverOnceThereIsOne(t *testing.T) {
	f := newFixture()
	roles := &stubDesigner{}
	f.svc.SetRoleResolver(roles)

	first, err := f.svc.RequestForRepository(context.Background(), f.web.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || !first.WaitingForDesigner {
		t.Fatalf("a task opened with no designer must say it waits for one: %+v", first)
	}

	designer := uuid.New()
	roles.id = &designer
	again, err := f.svc.RequestForRepository(context.Background(), f.web.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Task.ID != first.Task.ID {
		t.Fatalf("the open task is reused, not duplicated: %+v", again)
	}
	if again.WaitingForDesigner || again.Task.AssigneeAgentID == nil || *again.Task.AssigneeAgentID != designer {
		t.Fatalf("pressing again once the designer exists must assign it: %+v", again)
	}
	if got := f.tasks.tasks[first.Task.ID].AssigneeAgentID; got == nil || *got != designer {
		t.Fatalf("the stored task keeps no designer: %v", got)
	}
}
