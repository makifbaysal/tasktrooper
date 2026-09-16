package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeAutoReleaseDeployStore is a minimal port.DeployTargetStore for
// AutoReleaseIfUndeployable tests: only ListByRepository is exercised.
type fakeAutoReleaseDeployStore struct {
	targets []domain.DeployTarget
	listErr error
}

func (f *fakeAutoReleaseDeployStore) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.targets, nil
}
func (f *fakeAutoReleaseDeployStore) ListAll(context.Context) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeAutoReleaseDeployStore) Get(context.Context, uuid.UUID, string, string) (domain.DeployTarget, error) {
	return domain.DeployTarget{}, nil
}
func (f *fakeAutoReleaseDeployStore) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fakeAutoReleaseDeployStore) Delete(context.Context, uuid.UUID, string, string) error {
	return nil
}

func TestHasAnyDeployTarget(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()

	t.Run("no store wired means no target", func(t *testing.T) {
		svc := &Service{}
		has, err := svc.hasAnyDeployTarget(ctx, repoID)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("empty store means no target", func(t *testing.T) {
		svc := &Service{deployTargets: &fakeAutoReleaseDeployStore{}}
		has, err := svc.hasAnyDeployTarget(ctx, repoID)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("at least one target row", func(t *testing.T) {
		svc := &Service{deployTargets: &fakeAutoReleaseDeployStore{
			targets: []domain.DeployTarget{{Provider: domain.DeployProviderFly}},
		}}
		has, err := svc.hasAnyDeployTarget(ctx, repoID)
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("store error propagates", func(t *testing.T) {
		boom := errors.New("boom: deploy target db down")
		svc := &Service{deployTargets: &fakeAutoReleaseDeployStore{listErr: boom}}
		has, err := svc.hasAnyDeployTarget(ctx, repoID)
		require.ErrorIs(t, err, boom)
		require.False(t, has)
	})
}

func TestAutoReleaseIfUndeployable(t *testing.T) {
	t.Run("zero targets releases the task", func(t *testing.T) {
		repoID, taskID := uuid.New(), uuid.New()
		tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
			ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnDone,
		}}
		svc := &Service{
			repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
			tasks:         tasks,
			deployTargets: &fakeAutoReleaseDeployStore{},
		}

		released := svc.AutoReleaseIfUndeployable(context.Background(), repoID, taskID)

		require.True(t, released)
		require.Equal(t, domain.TaskColumnReleased, tasks.updated.Column)
	})

	t.Run("at least one target leaves the task unchanged", func(t *testing.T) {
		repoID, taskID := uuid.New(), uuid.New()
		tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
			ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnDone,
		}}
		svc := &Service{
			repos: &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
			tasks: tasks,
			deployTargets: &fakeAutoReleaseDeployStore{
				targets: []domain.DeployTarget{{Provider: domain.DeployProviderFly}},
			},
		}

		released := svc.AutoReleaseIfUndeployable(context.Background(), repoID, taskID)

		require.False(t, released)
		require.Empty(t, tasks.updated.ID)
	})

	t.Run("require_release_deploy refuses and comments, task stays in done", func(t *testing.T) {
		repoID, taskID := uuid.New(), uuid.New()
		tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
			ID: taskID, RepositoryID: repoID, TaskType: domain.TaskTypeTask, Column: domain.TaskColumnDone,
		}}
		comments := &fakeReleaseComments{}
		svc := &Service{
			repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID, RequireReleaseDeploy: true}},
			tasks:         tasks,
			deployTargets: &fakeAutoReleaseDeployStore{},
			comments:      comments,
		}

		released := svc.AutoReleaseIfUndeployable(context.Background(), repoID, taskID)

		require.False(t, released)
		require.Empty(t, tasks.updated.ID)
		require.Len(t, comments.comments, 1)
		require.Contains(t, comments.comments[0].Content, "require_release_deploy")
	})

	t.Run("deploy target lookup failure fails safe, not open", func(t *testing.T) {
		repoID, taskID := uuid.New(), uuid.New()
		tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
			ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnDone,
		}}
		boom := errors.New("boom: deploy target db down")
		svc := &Service{
			repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
			tasks:         tasks,
			deployTargets: &fakeAutoReleaseDeployStore{listErr: boom},
		}

		released := svc.AutoReleaseIfUndeployable(context.Background(), repoID, taskID)

		require.False(t, released)
		require.Empty(t, tasks.updated.ID)
	})
}
