package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type taskChatTaskStore struct {
	tasks    map[[2]uuid.UUID]domain.BoardTask
	prs      map[uuid.UUID]string
	merges   map[uuid.UUID]string
	mergeErr error
}

func (f *taskChatTaskStore) Get(_ context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, ok := f.tasks[[2]uuid.UUID{repositoryID, taskID}]
	if !ok {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return task, nil
}

func (f *taskChatTaskStore) SetTaskPullRequest(_ context.Context, taskID uuid.UUID, url string, _ int) error {
	if f.prs == nil {
		f.prs = map[uuid.UUID]string{}
	}
	f.prs[taskID] = url
	return nil
}

func (f *taskChatTaskStore) SetTaskMergeCommit(_ context.Context, taskID uuid.UUID, sha string) error {
	if f.mergeErr != nil {
		return f.mergeErr
	}
	if f.merges == nil {
		f.merges = map[uuid.UUID]string{}
	}
	f.merges[taskID] = sha
	return nil
}

type taskChatAgentStore struct {
	agents map[uuid.UUID]domain.Agent
}

func (f *taskChatAgentStore) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	agentRec, ok := f.agents[id]
	if !ok {
		return domain.Agent{}, errors.New("agent not found")
	}
	return agentRec, nil
}

type taskChatColumnStore struct {
	byColumn map[string][]uuid.UUID
}

func (f *taskChatColumnStore) AgentsForColumn(_ context.Context, column, _ string) ([]uuid.UUID, error) {
	return f.byColumn[column], nil
}

type taskChatRepos struct{ root string }

func (f taskChatRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return f.root, nil
}

func newTaskChatFixture(task domain.BoardTask, repositoryID uuid.UUID) (*TaskChatOpener, *fakeSessionStore, *taskChatTaskStore) {
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{
		{repositoryID, task.ID}: task,
	}}
	sessions := newFakeSessionStore()
	return NewTaskChatOpener(TaskChatOpenerDeps{
		Tasks:    tasks,
		Sessions: sessions,
		Agents:   &taskChatAgentStore{agents: map[uuid.UUID]domain.Agent{}},
		Columns:  &taskChatColumnStore{},
		Repos:    taskChatRepos{root: "/repos/widget"},
	}), sessions, tasks
}

func TestTaskChatOpenCreatesThenReusesOneThread(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link", Column: domain.TaskColumnInProgress}
	opener, sessions, _ := newTaskChatFixture(task, repositoryID)

	first, _, err := opener.Open(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)
	require.Len(t, sessions.created, 1)
	assert.Equal(t, sessions.created[0].ID, first)
	assert.Equal(t, "DE-1 Add the store link", sessions.created[0].Title)
	require.NotNil(t, sessions.existing[first].TaskID)
	assert.Equal(t, task.ID, *sessions.existing[first].TaskID)
	require.Len(t, sessions.appended, 1)
	assert.Contains(t, sessions.appended[0].Content, "DE-1")

	second, _, err := opener.Open(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Len(t, sessions.created, 1, "the bound thread is reused, not replaced")
	assert.Len(t, sessions.appended, 1, "reopening must not re-greet an existing conversation")
}

func TestTaskChatOpenRejectsATaskFromAnotherRepository(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link"}
	opener, sessions, _ := newTaskChatFixture(task, repositoryID)

	_, _, err := opener.Open(context.Background(), uuid.New(), task.ID)

	require.ErrorIs(t, err, domain.ErrBoardTaskNotFound, "the transport turns this into a 404")
	assert.Empty(t, sessions.created)
}

func TestTaskChatOpenAdoptsTheTasksClarificationThread(t *testing.T) {
	repositoryID := uuid.New()
	taskID := uuid.New()
	sessions := newFakeSessionStore()
	existing, err := sessions.Create(context.Background(), "Question: Add the store link", "gpt", "/repos/widget", &repositoryID, nil, nil)
	require.NoError(t, err)
	sessions.created = nil

	task := domain.BoardTask{ID: taskID, RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link", ClarificationSessionID: &existing.ID}
	opener := NewTaskChatOpener(TaskChatOpenerDeps{
		Tasks:    &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{{repositoryID, taskID}: task}},
		Sessions: sessions,
		Repos:    taskChatRepos{root: "/repos/widget"},
	})

	got, _, err := opener.Open(context.Background(), repositoryID, taskID)
	require.NoError(t, err)
	assert.Equal(t, existing.ID, got)
	assert.Empty(t, sessions.created, "the question thread becomes the task thread")
	require.NotNil(t, sessions.existing[got].TaskID)
	assert.Equal(t, taskID, *sessions.existing[got].TaskID)
}

func TestTaskChatOpenPicksTheAssigneeThenTheColumnAgent(t *testing.T) {
	repositoryID := uuid.New()
	assignee := domain.Agent{ID: uuid.New(), Name: "backend-developer", Model: "m1", Enabled: true}
	reviewer := domain.Agent{ID: uuid.New(), Name: "system-architect", Model: "m2", Enabled: true}
	agents := &taskChatAgentStore{agents: map[uuid.UUID]domain.Agent{assignee.ID: assignee, reviewer.ID: reviewer}}
	columns := &taskChatColumnStore{byColumn: map[string][]uuid.UUID{
		string(domain.TaskColumnCodeReview): {reviewer.ID},
	}}

	assigned := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Assigned", Column: domain.TaskColumnCodeReview, AssigneeAgentID: &assignee.ID}
	unassigned := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-2", Title: "Unassigned", Column: domain.TaskColumnCodeReview}
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{
		{repositoryID, assigned.ID}:   assigned,
		{repositoryID, unassigned.ID}: unassigned,
	}}
	sessions := newFakeSessionStore()
	opener := NewTaskChatOpener(TaskChatOpenerDeps{
		Tasks: tasks, Sessions: sessions, Agents: agents, Columns: columns,
		Repos: taskChatRepos{root: "/repos/widget"},
	})

	_, gotAssigned, err := opener.Open(context.Background(), repositoryID, assigned.ID)
	require.NoError(t, err)
	assert.Equal(t, assignee.ID, gotAssigned)

	_, gotUnassigned, err := opener.Open(context.Background(), repositoryID, unassigned.ID)
	require.NoError(t, err)
	assert.Equal(t, reviewer.ID, gotUnassigned, "an unassigned task talks to the column's agent")
}

func (f *taskChatTaskStore) FindTaskByMergeCommit(context.Context, uuid.UUID, string) (domain.BoardTask, error) {
	return domain.BoardTask{}, errors.New("not found")
}

func (f *taskChatTaskStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *taskChatTaskStore) TakeBlockedResourceTask(context.Context, string, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *taskChatTaskStore) ReleaseAnalysisQuestionsBlock(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func TestTaskChatOnAnUnassignedQueueCardTalksToItsAreasDeveloper(t *testing.T) {
	repositoryID := uuid.New()
	backend := domain.Agent{ID: uuid.New(), Name: "backend-developer", Enabled: true}
	frontend := domain.Agent{ID: uuid.New(), Name: "frontend-developer", Enabled: true}
	data := domain.Agent{ID: uuid.New(), Name: "data-scientist", Enabled: true}
	designer := domain.Agent{ID: uuid.New(), Name: "ui-designer", Enabled: true}
	agents := &taskChatAgentStore{agents: map[uuid.UUID]domain.Agent{
		backend.ID: backend, frontend.ID: frontend, data.ID: data, designer.ID: designer,
	}}
	columns := &taskChatColumnStore{byColumn: map[string][]uuid.UUID{
		string(domain.TaskColumnTodo): {backend.ID, designer.ID, data.ID, frontend.ID},
	}}
	roles := workflowtest.Fixture{Roles: []domain.AgentRole{
		{ID: uuid.New(), Key: "developer", Assignments: []domain.RoleAssignment{
			{AgentID: backend.ID, Areas: []string{"backend"}},
			{AgentID: frontend.ID, Areas: []string{"frontend"}},
			{AgentID: data.ID, Areas: []string{"data"}},
		}},
		{ID: uuid.New(), Key: "designer", Assignments: []domain.RoleAssignment{{AgentID: designer.ID, Areas: []string{}}}},
	}}.Resolver()

	card := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "T-9", Title: "Login form", Column: domain.TaskColumnTodo}
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{{repositoryID, card.ID}: card}}
	open := func(area string) []uuid.UUID {
		got := subscribersForTaskArea(context.Background(), roles, func(context.Context, uuid.UUID) string { return area }, card,
			columns.byColumn[string(domain.TaskColumnTodo)])
		return got
	}

	assert.Equal(t, []uuid.UUID{frontend.ID, designer.ID}, open("frontend"), "the area's developer first, then the area-less designer")
	assert.Equal(t, []uuid.UUID{backend.ID, designer.ID}, open("game"), "no game developer: game falls back to backend")
	assert.Equal(t, []uuid.UUID{backend.ID, designer.ID}, open(""), "an unreadable repository settles on backend")

	opener := NewTaskChatOpener(TaskChatOpenerDeps{
		Tasks: tasks, Sessions: newFakeSessionStore(), Agents: agents, Columns: columns,
		Repos: taskChatRepos{root: "/repos/web"}, Roles: roles,
		RepoArea: func(context.Context, uuid.UUID) string { return "data" },
	})
	_, got, err := opener.Open(context.Background(), repositoryID, card.ID)
	require.NoError(t, err)
	assert.Equal(t, data.ID, got, "the data card's chat goes to the data scientist, not the designer listed before it")
}
