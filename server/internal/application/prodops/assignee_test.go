package prodops_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeIncidentTaskBoard struct {
	created []domain.CreateBoardTaskRequest
}

func (f *fakeIncidentTaskBoard) CreateTask(_ context.Context, _ uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.created = append(f.created, req)
	return domain.BoardTask{ID: uuid.New(), Key: "B-1"}, nil
}

func (f *fakeIncidentTaskBoard) AddComment(context.Context, uuid.UUID, uuid.UUID, domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	return domain.TaskComment{}, nil
}

type fakeIncidentRepos struct {
	repo domain.Repository
}

func (f fakeIncidentRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

type fakeIncidentRoleResolver struct {
	agentID uuid.UUID
}

func (f fakeIncidentRoleResolver) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return nil, nil
}

func (f fakeIncidentRoleResolver) AgentForPurpose(_ context.Context, purpose domain.RolePurposeKey, _ string) (*uuid.UUID, error) {
	if purpose != domain.PurposeSystemTaskAssignee {
		return nil, nil
	}
	id := f.agentID
	return &id, nil
}

func (f fakeIncidentRoleResolver) AgentArea(context.Context, uuid.UUID) string { return "" }

func (f fakeIncidentRoleResolver) AgentAreas(context.Context, uuid.UUID) []string { return nil }

func (f fakeIncidentRoleResolver) AssigneeForNewTask(_ context.Context, _ domain.TaskType, _ string, requested *uuid.UUID) (*uuid.UUID, error) {
	return requested, nil
}

func TestIngestAssignsRemediationTaskThroughRoleResolver(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend}
	tasks := &fakeIncidentTaskBoard{}
	resolver := fakeIncidentRoleResolver{agentID: uuid.New()}
	svc := prodops.NewService(prodops.Deps{
		Incidents: newFakeIncidents(),
		Repos:     fakeIncidentRepos{repo: repo},
		Tasks:     tasks,
	})
	svc.SetRoleResolver(resolver)

	_, err := svc.Ingest(context.Background(), domain.IncidentInput{
		RepositoryID: repo.ID,
		Severity:     domain.IncidentSeverityCritical,
		Title:        "prod is down",
	})
	require.NoError(t, err)
	require.Len(t, tasks.created, 1)
	require.NotNil(t, tasks.created[0].AssigneeAgentID)
	require.Equal(t, resolver.agentID, *tasks.created[0].AssigneeAgentID)
}

func TestIngestLeavesRemediationTaskUnassignedWithoutRoleResolver(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend}
	tasks := &fakeIncidentTaskBoard{}
	svc := prodops.NewService(prodops.Deps{
		Incidents: newFakeIncidents(),
		Repos:     fakeIncidentRepos{repo: repo},
		Tasks:     tasks,
	})

	_, err := svc.Ingest(context.Background(), domain.IncidentInput{
		RepositoryID: repo.ID,
		Severity:     domain.IncidentSeverityCritical,
		Title:        "prod is down",
	})
	require.NoError(t, err)
	require.Len(t, tasks.created, 1)
	require.Nil(t, tasks.created[0].AssigneeAgentID)
}
