package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeLocalPreviewStopper struct {
	stoppedRepos []uuid.UUID
}

func (f *fakeLocalPreviewStopper) Stop(repositoryID uuid.UUID) {
	f.stoppedRepos = append(f.stoppedRepos, repositoryID)
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
	require.Len(t, stopper.stoppedRepos, 1)
	assert.Equal(t, repoID, stopper.stoppedRepos[0])
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
	assert.Empty(t, stopper.stoppedRepos)
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
	require.Len(t, stopper.stoppedRepos, 1)
	assert.Equal(t, repoID, stopper.stoppedRepos[0])
}
