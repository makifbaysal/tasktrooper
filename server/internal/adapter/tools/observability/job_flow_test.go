package observability

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsai"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type turnSessions struct {
	turn func(ctx context.Context) domain.AgentResponse
	sid  uuid.UUID
}

func (t *turnSessions) Create(context.Context, domain.CreateSessionRequest) (domain.Session, error) {
	return domain.Session{ID: t.sid}, nil
}

func (t *turnSessions) SendMessage(ctx context.Context, sid uuid.UUID, _ domain.SessionMessageRequest, _ domain.ToolPolicy) (domain.AgentResponse, error) {
	return t.turn(registry.ContextWithSessionID(ctx, sid)), nil
}

func (t *turnSessions) CancelSession(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}

type releaseRoles struct{ agent uuid.UUID }

func (r releaseRoles) RoleByKey(context.Context, string) (domain.AgentRole, error) {
	return domain.AgentRole{ID: uuid.New()}, nil
}

func (r releaseRoles) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return &r.agent, nil
}

type oneRepo struct{ repo domain.Repository }

func (o oneRepo) Get(context.Context, uuid.UUID) (domain.Repository, error) { return o.repo, nil }

func TestASuggestionJobStoresWhatTheAgentsToolCallProposes(t *testing.T) {
	ctx := context.Background()
	repo := domain.Repository{ID: uuid.New(), Name: "acme"}
	api := domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "services/api", Status: domain.ComponentStatusActive}
	comps := &fakeComponents{comps: []domain.Component{api}}
	defStore := mocks.NewErrorDefinitionStore(t)
	jobStore := mocks.NewAIJobStore(t)
	sid := uuid.New()

	defs, err := errordef.NewService(errordef.Deps{Store: defStore, Components: comps})
	require.NoError(t, err)
	sessions := &turnSessions{sid: sid}
	ai, err := opsai.NewService(opsai.Deps{
		Jobs: jobStore, Sessions: sessions, Roles: releaseRoles{agent: uuid.New()},
		Components: comps, Repositories: oneRepo{repo: repo},
	}, opsai.WithSynchronousJobs())
	require.NoError(t, err)
	kit := &ToolKit{Components: comps, Definitions: defs, Jobs: ai}

	job := domain.AIJob{ID: uuid.New(), Kind: domain.AIJobDefineErrors, RepositoryID: repo.ID, ComponentID: &api.ID,
		Environment: domain.EnvironmentProduction, Status: domain.AIJobRunning}
	withSession := job
	withSession.SessionID = &sid
	stored := uuid.New()
	jobStore.On("FailStaleAIJobs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(int64(0), nil).Once()
	jobStore.On("CreateAIJob", mock.Anything, mock.Anything).Return(job, true, nil).Once()
	jobStore.On("StartAIJob", mock.Anything, job.ID).Return(true, nil).Once()
	jobStore.On("SetAIJobSession", mock.Anything, job.ID, sid).Return(nil).Once()
	jobStore.On("GetAIJobBySession", mock.Anything, sid).Return(withSession, nil)
	defStore.On("ListErrorDefinitions", mock.Anything, domain.ErrorDefinitionFilter{RepositoryIDs: []uuid.UUID{repo.ID}}).
		Return([]domain.ErrorDefinition{}, nil).Once()
	defStore.On("CreateErrorDefinition", mock.Anything, mock.MatchedBy(func(d domain.ErrorDefinition) bool {
		return d.Status == domain.ErrorDefinitionSuggested && d.Source == domain.ErrorDefinitionAI &&
			d.RepositoryID == repo.ID && *d.ComponentID == api.ID && d.Match.ExceptionType == "TimeoutError"
	})).Return(func(_ context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error) {
		d.ID = stored
		return d, nil
	}).Once()
	jobStore.On("AppendAIJobItem", mock.Anything, job.ID, domain.AIJobItem{
		Kind: domain.AIJobItemErrorDefinition, ID: stored, Name: "Provider timeout", Rationale: "the provider is slow",
	}).Return(nil).Once()
	jobStore.On("FinishAIJob", mock.Anything, job.ID, domain.AIJobDone, "Provider timeout — dependency, alert", "").
		Return(domain.AIJob{ID: job.ID, Status: domain.AIJobDone}, nil).Once()
	jobStore.On("GetAIJob", mock.Anything, job.ID).Return(domain.AIJob{ID: job.ID, Status: domain.AIJobDone}, nil).Once()

	sessions.turn = func(toolCtx context.Context) domain.AgentResponse {
		res := (&proposeErrorDefinitionsTool{kit: kit}).Execute(toolCtx, `{"definitions":[{"name":"Provider timeout",
			"match":{"exception_type":"TimeoutError"},"category":"dependency","action":"alert","rationale":"the provider is slow"}]}`)
		require.False(t, res.IsError, res.Content)
		return domain.AgentResponse{Message: domain.Message{Content: "Provider timeout — dependency, alert"}}
	}

	got, err := ai.SuggestErrors(ctx, opsai.SuggestErrorsRequest{RepositoryID: repo.ID.String(), ComponentID: api.ID.String()})
	require.NoError(t, err)
	assert.Equal(t, domain.AIJobDone, got.Status)
}
