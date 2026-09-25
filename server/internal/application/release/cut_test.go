package release

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type cutFixture struct {
	svc          *Service
	store        *fakeReleaseStore
	components   *fakeComponents
	git          *fakeGit
	tasks        *fakeTasks
	waker        *fakeWaker
	repositoryID uuid.UUID
	componentID  uuid.UUID
}

func newCutFixture() *cutFixture {
	store := newFakeReleaseStore()
	components := newFakeComponents()
	git := newFakeGit()
	tasks := newFakeTasks()
	waker := &fakeWaker{}
	repos := &fakeRepos{repo: domain.Repository{ID: uuid.New(), Name: "desktop", RootPath: "/repos/desktop"}}
	svc := New(Deps{
		Store:      store,
		Components: components,
		Git:        git,
		Tasks:      tasks,
		Waker:      waker,
		Repos:      repos,
		Clock:      func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &cutFixture{svc: svc, store: store, components: components, git: git, tasks: tasks, waker: waker, repositoryID: repos.repo.ID}
}

func (f *cutFixture) addDraft(component domain.Component, tasks ...domain.ReleaseTaskRef) domain.Release {
	f.componentID = component.ID
	f.components.add(f.repositoryID, component)
	r := domain.Release{
		RepositoryID: f.repositoryID,
		ComponentID:  &component.ID,
		Mode:         domain.DeliveryBatch,
		Executor:     component.Delivery.Override.Executor,
		Status:       domain.ReleaseDraft,
		Profile:      *component.Delivery.Override,
	}
	ids := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
		f.tasks.tasks[t.ID] = domain.BoardTask{ID: t.ID, Key: t.Key, Title: t.Title, TaskType: t.TaskType, RepositoryID: f.repositoryID}
	}
	created, err := f.store.Create(context.Background(), r, ids)
	if err != nil {
		panic(err)
	}
	// fakeReleaseStore.Create only fills Tasks with bare IDs; give the test
	// the richer refs CutPreview/Cut actually read (Key, Title, TaskType).
	created.Tasks = tasks
	f.store.releases[created.ID] = created
	return created
}

func taskRef(taskType domain.TaskType) domain.ReleaseTaskRef {
	return domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1", Title: "Fix the thing", TaskType: taskType, MergeCommitSHA: mergeSHA}
}

func TestCutPreviewRejectsAnEmptyDraft(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component)

	_, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseEmpty)
}

func TestCutPreviewRejectsANonDraftRelease(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	draft.Status = domain.ReleasePending
	f.store.releases[draft.ID] = draft

	_, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}

func TestCutPreviewSuggestsAPatchBumpWhenEveryTaskIsABug(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component, taskRef(domain.TaskTypeBug))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true
	f.git.tag = "v1.2.3"

	preview, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", preview.PreviousVersion)
	assert.Equal(t, "1.2.4", preview.SuggestedVersion)
	assert.Equal(t, f.git.head, preview.CommitSHA)
	assert.Equal(t, "v1.2.4", preview.Tag)
	assert.Contains(t, preview.Notes, "### Fixes")
	assert.NotContains(t, preview.Notes, "### Features")
}

func TestCutPreviewSuggestsAMinorBumpWhenAnyTaskIsNotABug(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component, taskRef(domain.TaskTypeBug), taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	for _, tr := range draft.Tasks {
		f.git.ancestors[tr.MergeCommitSHA] = true
	}
	f.git.tag = "v1.2.3"

	preview, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Equal(t, "1.3.0", preview.SuggestedVersion)
	assert.Contains(t, preview.Notes, "### Features")
	assert.Contains(t, preview.Notes, "### Fixes")
}

func TestCutPreviewWithNoPreviousVersionSuggestsZeroOneZero(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true
	f.git.tag = ""

	preview, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Empty(t, preview.PreviousVersion)
	assert.Equal(t, "0.1.0", preview.SuggestedVersion)
}

func TestCutPreviewLeavesSuggestedEmptyForANonSemverPrevious(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true
	f.git.tag = "v2024-edition"

	preview, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Equal(t, "2024-edition", preview.PreviousVersion)
	assert.Empty(t, preview.SuggestedVersion, "a human must type the version when the previous one is not plain semver")
}

func TestCutPreviewErrorsWhenATasksMergeCommitIsNotOnTheDefaultBranch(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	// ancestors left empty: the task's merge commit is not reachable.

	_, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "T-1")
}

func TestCutFreezesTheCurrentProfileAndMovesToPending(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorLocal)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true

	updated, err := f.svc.Cut(context.Background(), draft.ID, domain.ReleaseActorHuman, domain.ReleaseCutRequest{Version: "1.0.0"})
	require.NoError(t, err)

	assert.Equal(t, domain.ReleasePending, updated.Status)
	assert.Equal(t, "1.0.0", updated.Version)
	assert.Equal(t, "v1.0.0", updated.Tag)
	assert.Equal(t, f.git.head, updated.CommitSHA)
	require.NotNil(t, updated.CutAt)
	assert.Contains(t, updated.Notes, "### Features")
	assert.Len(t, f.waker.calls, 1, "cutting a draft must wake the release engineer the way a dispatch OpenPending catch-up does")
	assert.Equal(t, domain.ReleasePending, f.waker.calls[0].status)
}

func TestCutRejectsAnInvalidVersion(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorLocal)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true

	_, err := f.svc.Cut(context.Background(), draft.ID, domain.ReleaseActorHuman, domain.ReleaseCutRequest{Version: "-bad"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInvalidVersion)
}

func TestCutRejectsANonDraftRelease(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorLocal)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	draft.Status = domain.ReleasePending
	f.store.releases[draft.ID] = draft

	_, err := f.svc.Cut(context.Background(), draft.ID, domain.ReleaseActorHuman, domain.ReleaseCutRequest{Version: "1.0.0"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}

func TestCutRefusesWhenTheComponentNoLongerBatches(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorLocal)
	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true

	dispatch := deliveryProfile(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	changed := component
	changed.Delivery = domain.Fact[domain.ComponentDelivery]{Override: &dispatch}
	f.components.byID[component.ID] = changed

	_, err := f.svc.Cut(context.Background(), draft.ID, domain.ReleaseActorHuman, domain.ReleaseCutRequest{Version: "1.0.0"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}
