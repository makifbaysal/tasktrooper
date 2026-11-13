package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeLocalPreviewStopper is safe for concurrent use because UpdateTask fires
// the real Stop off the request path (see the "go s.localPreview.Stop" call
// in service.go) so it can be waited on without blocking the column move.
type fakeLocalPreviewStopper struct {
	mu           sync.Mutex
	stoppedRepos []uuid.UUID
}

func (f *fakeLocalPreviewStopper) Stop(repositoryID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stoppedRepos = append(f.stoppedRepos, repositoryID)
}

func (f *fakeLocalPreviewStopper) stopped() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.stoppedRepos...)
}

func requireEventuallyStopped(t *testing.T, stopper *fakeLocalPreviewStopper, repoID uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		stopped := stopper.stopped()
		return len(stopped) == 1 && stopped[0] == repoID
	}, time.Second, time.Millisecond, "want the repository's preview stopped off the request path")
}

// TestUpdateTaskStopsTheRepositorysLocalPreviewOnAnyColumnMove pins the rule
// that a running preview is tied to the task's column, not to a specific
// transition: whichever column the task lands in, the single UpdateTask
// chokepoint stops the repository's preview the same way Stop/Close already do.
func TestUpdateTaskStopsTheRepositorysLocalPreviewOnAnyColumnMove(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, Key: "APP-1", TaskType: "task", Column: domain.TaskColumnInProgress,
	}}
	stopper := &fakeLocalPreviewStopper{}
	svc := &Service{
		repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:     tasks,
		workflows: workflowtest.Default().Reader(),
	}
	svc.SetLocalPreview(stopper)

	target := domain.TaskColumnNeedRevision
	_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
		Column: &target,
		Actor:  domain.TaskActorHuman,
	})

	require.NoError(t, err)
	requireEventuallyStopped(t, stopper, repoID)
}

// TestUpdateTaskLeavingTheColumnUnchangedDoesNotTouchThePreview makes sure the
// stop is wired to an actual column change, not to every UpdateTask call.
func TestUpdateTaskLeavingTheColumnUnchangedDoesNotTouchThePreview(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, Key: "APP-2", TaskType: "task", Column: domain.TaskColumnInProgress,
	}}
	stopper := &fakeLocalPreviewStopper{}
	svc := &Service{
		repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:     tasks,
		workflows: workflowtest.Default().Reader(),
	}
	svc.SetLocalPreview(stopper)

	title := "renamed, column untouched"
	_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
		Title: &title,
		Actor: domain.TaskActorHuman,
	})

	require.NoError(t, err)
	// No goroutine is ever fired for a no-op column, so there is nothing to
	// wait on; a short sleep would just mask a bug, not prove its absence.
	assert.Empty(t, stopper.stopped())
}

// TestUpdateTaskWithoutALocalPreviewWiredIsANoOp proves a repository with no
// running preview (or a server built with local preview disabled, nil
// localPreview) never errors on a column move — the gate is purely additive.
func TestUpdateTaskWithoutALocalPreviewWiredIsANoOp(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, Key: "APP-3", TaskType: "task", Column: domain.TaskColumnInProgress,
	}}
	svc := &Service{
		repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:     tasks,
		workflows: workflowtest.Default().Reader(),
	}

	target := domain.TaskColumnNeedRevision
	_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
		Column: &target,
		Actor:  domain.TaskActorHuman,
	})

	require.NoError(t, err)
}

// TestUpdateTaskStopsThePreviewWhenHumanUATIsApprovedToDone pins the task's
// own scenario: approving human_uat into done must not leave the task's
// local preview running.
func TestUpdateTaskStopsThePreviewWhenHumanUATIsApprovedToDone(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, Key: "APP-4", TaskType: "task", Column: domain.TaskColumnHumanUAT,
	}}
	stopper := &fakeLocalPreviewStopper{}
	svc := &Service{
		repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:         tasks,
		spans:         visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnHumanUAT),
		pipelineStore: &fakeDeployPipelines{},
		workflows:     workflowtest.Default().Reader(),
	}
	svc.SetLocalPreview(stopper)

	done := domain.TaskColumnDone
	_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
		Column: &done,
		Actor:  domain.TaskActorHuman,
	})

	require.NoError(t, err)
	requireEventuallyStopped(t, stopper, repoID)
}

// TestUpdateTaskDoesNotBlockOnAStopThatTakesLonger_thanTheRequest proves the
// fix for the hot-path regression: UpdateTask must return well before a slow
// Stop (modelling the real stopGrace wait for a process group to exit)
// finishes, because UpdateTask is the chokepoint every column move goes
// through — the board runner and pipeline automation included, not just the
// human_uat approval that happens to have a preview running.
func TestUpdateTaskDoesNotBlockOnAStopThatTakesLongerThanTheRequest(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, Key: "APP-5", TaskType: "task", Column: domain.TaskColumnInProgress,
	}}
	released := make(chan struct{})
	slowStopper := &slowLocalPreviewStopper{block: released}
	svc := &Service{
		repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:     tasks,
		workflows: workflowtest.Default().Reader(),
	}
	svc.SetLocalPreview(slowStopper)
	defer close(released)

	target := domain.TaskColumnNeedRevision
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
			Column: &target,
			Actor:  domain.TaskActorHuman,
		})
		done <- err
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
		assert.Less(t, time.Since(start), 500*time.Millisecond,
			"UpdateTask must not wait on Stop; it should return immediately and let Stop finish in its own goroutine")
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateTask hung waiting on a slow Stop; it must fire Stop off the request path")
	}
}

type slowLocalPreviewStopper struct {
	block chan struct{}
}

func (f *slowLocalPreviewStopper) Stop(uuid.UUID) {
	<-f.block
}
