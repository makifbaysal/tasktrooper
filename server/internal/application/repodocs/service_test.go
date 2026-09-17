package repodocs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeRepos is a one-row repository store. It applies the same subset of
// UpdateRepositoryRequest the service actually sends (docs and sub-projects),
// so a test can assert where a doc path landed.
type fakeRepos struct {
	repo        domain.Repository
	docsTaskIDs []string
	updateErr   error
}

func (f *fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

func (f *fakeRepos) Update(_ context.Context, _ uuid.UUID, req domain.UpdateRepositoryRequest) (domain.Repository, error) {
	if f.updateErr != nil {
		return domain.Repository{}, f.updateErr
	}
	if req.Docs != nil {
		f.repo.Docs = *req.Docs
	}
	if req.SubProjects != nil {
		f.repo.SubProjects = *req.SubProjects
	}
	return f.repo, nil
}

func (f *fakeRepos) SetDocsTaskID(_ context.Context, _ uuid.UUID, taskID string) error {
	f.docsTaskIDs = append(f.docsTaskIDs, taskID)
	f.repo.DocsTaskID = taskID
	return nil
}

type fakeTasks struct {
	created []domain.CreateBoardTaskRequest
	task    domain.BoardTask
	getErr  error
}

func (f *fakeTasks) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.created = append(f.created, req)
	if f.task.ID == uuid.Nil {
		f.task = domain.BoardTask{ID: uuid.New()}
	}
	f.task.RepositoryID = repositoryID
	f.task.Title = req.Title
	f.task.Description = req.Description
	f.task.Column = req.Column
	return f.task, nil
}

func (f *fakeTasks) GetTask(_ context.Context, _ uuid.UUID, taskID uuid.UUID) (domain.BoardTask, error) {
	if f.getErr != nil {
		return domain.BoardTask{}, f.getErr
	}
	if f.task.ID != taskID {
		return domain.BoardTask{}, errors.New("no such task")
	}
	return f.task, nil
}

type fakeMerger struct {
	calls  []uuid.UUID
	result domain.TaskPRMergeResult
	err    error
}

func (f *fakeMerger) MergeTaskPullRequest(_ context.Context, _, taskID uuid.UUID) (domain.TaskPRMergeResult, error) {
	f.calls = append(f.calls, taskID)
	return f.result, f.err
}

func newFixture(t *testing.T, repo domain.Repository) (*repodocs.Service, *fakeRepos, *fakeTasks) {
	t.Helper()
	if repo.ID == uuid.Nil {
		repo.ID = uuid.New()
	}
	repos := &fakeRepos{repo: repo}
	tasks := &fakeTasks{}
	svc := repodocs.NewService(repos)
	svc.SetTaskCreator(tasks)
	return svc, repos, tasks
}

func TestCreateDocsBundleTaskOpensOneTaskNamingEveryPath(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	task, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocCodingStandards},
		{Kind: domain.RepoDocArchitecture},
		{Kind: domain.RepoDocLocalRun},
	})
	require.NoError(t, err)
	require.Len(t, tasks.created, 1, "one bundle is one task, whatever it holds")

	req := tasks.created[0]
	require.Equal(t, "Generate reference docs", req.Title)
	require.Equal(t, domain.TaskColumnTodo, req.Column)
	for _, want := range []string{".ai/coding-standards.md", ".ai/architecture.md", "scripts/dev.sh"} {
		require.Contains(t, req.Description, want)
	}
	require.Contains(t, req.Description, "single branch")
	require.Contains(t, req.Description, "one pull request")
	require.Contains(t, req.Description, "Do not open a pull request per document")

	// Every path is claimed by the repository before the task exists.
	require.Equal(t, ".ai/coding-standards.md", repos.repo.Docs.CodingStandards)
	require.Equal(t, ".ai/architecture.md", repos.repo.Docs.Architecture)
	require.Equal(t, "scripts/dev.sh", repos.repo.Docs.LocalRun)
	require.Equal(t, []string{task.ID.String()}, repos.docsTaskIDs)
}

func TestCreateDocsBundleTaskPrefixesSubProjectPaths(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{
		Kind: domain.RepoKindMonorepo,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/web", Kind: domain.RepoKindFrontend},
			{Path: "apps/api", Kind: domain.RepoKindBackend},
		},
	})
	_, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture, SubProjectPath: "apps/web"},
		{Kind: domain.RepoDocLocalRun, SubProjectPath: "apps/api"},
		{Kind: domain.RepoDocArchitecture, Path: "docs/overview.md"},
	})
	require.NoError(t, err)

	desc := tasks.created[0].Description
	require.Contains(t, desc, "apps/web/.ai/architecture.md")
	require.Contains(t, desc, "apps/api/scripts/dev.sh")
	require.Contains(t, desc, "docs/overview.md")

	// The sub-project rows keep their own paths, unprefixed, and neither write
	// clobbered the other.
	byPath := map[string]domain.RepoSubProject{}
	for _, sp := range repos.repo.SubProjects {
		byPath[sp.Path] = sp
	}
	require.Equal(t, ".ai/architecture.md", byPath["apps/web"].Docs.Architecture)
	require.Equal(t, "scripts/dev.sh", byPath["apps/api"].Docs.LocalRun)
	require.Equal(t, "docs/overview.md", repos.repo.Docs.Architecture)
}

// local_run asks for a script, not prose: the whole point of feature 3 is that
// a task cannot be satisfied by a markdown guide.
func TestBundleDescriptionAsksForAScriptForLocalRun(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	_, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocLocalRun},
	})
	require.NoError(t, err)
	desc := tasks.created[0].Description
	require.Contains(t, desc, "NOT a markdown guide")
	require.Contains(t, desc, "chmod +x")
	require.Contains(t, desc, "idempotent")
	require.Contains(t, desc, "usage header comment")
	require.NotContains(t, desc, "LOCAL_SETUP.md")
}

func TestCreateDocTaskAlsoAsksForAScriptForLocalRun(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	_, err := svc.CreateDocTask(context.Background(), repos.repo.ID, "", domain.RepoDocLocalRun, "")
	require.NoError(t, err)
	require.Contains(t, tasks.created[0].Description, "NOT a markdown guide")
	require.Equal(t, "scripts/dev.sh", repos.repo.Docs.LocalRun)
}

func TestCreateDocsBundleTaskRejectsBadInput(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})

	_, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, nil)
	require.Error(t, err)

	_, err = svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{{Kind: "nonsense"}})
	require.ErrorContains(t, err, "invalid doc kind")

	_, err = svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture},
		{Kind: domain.RepoDocArchitecture},
	})
	require.ErrorContains(t, err, "duplicate doc kind")

	require.Empty(t, tasks.created, "a rejected bundle opens no task")
	require.Empty(t, repos.docsTaskIDs)
}

func TestDocsTaskIsEmptyWithoutABundle(t *testing.T) {
	svc, repos, _ := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	status, err := svc.DocsTask(context.Background(), repos.repo.ID)
	require.NoError(t, err)
	require.Equal(t, repodocs.DocsTaskStatus{}, status)
}

func TestDocsTaskReportsColumnAndPullRequest(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	task, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture},
	})
	require.NoError(t, err)

	tasks.task.Key = "T-7"
	tasks.task.Column = domain.TaskColumnDone
	tasks.task.PRURL = "https://github.com/acme/app/pull/12"
	tasks.task.PRNumber = 12

	status, err := svc.DocsTask(context.Background(), repos.repo.ID)
	require.NoError(t, err)
	require.Equal(t, task.ID.String(), status.TaskID)
	require.Equal(t, "T-7", status.TaskKey)
	require.Equal(t, string(domain.TaskColumnDone), status.Column)
	require.Equal(t, "https://github.com/acme/app/pull/12", status.PRURL)
	require.Equal(t, 12, status.PRNumber)
	require.False(t, status.Merged)
}

// A task that was deleted from the board reads as "nothing outstanding", not
// as an error: the screen asking should offer to open a new bundle.
func TestDocsTaskForgivesAVanishedTask(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	_, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture},
	})
	require.NoError(t, err)
	tasks.getErr = errors.New("board task not found")

	status, err := svc.DocsTask(context.Background(), repos.repo.ID)
	require.NoError(t, err)
	require.Empty(t, status.TaskID)
}

func TestMergeDocsTaskMergesAndForgetsTheTask(t *testing.T) {
	svc, repos, _ := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	merger := &fakeMerger{result: domain.TaskPRMergeResult{Merged: true, PRNumber: 12, Message: "Merged pull request #12."}}
	svc.SetTaskPRMerger(merger)

	task, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture},
	})
	require.NoError(t, err)

	result, err := svc.MergeDocsTask(context.Background(), repos.repo.ID)
	require.NoError(t, err)
	require.True(t, result.Merged)
	require.Equal(t, []uuid.UUID{task.ID}, merger.calls)
	require.Equal(t, []string{task.ID.String(), ""}, repos.docsTaskIDs, "a landed merge clears the task")
}

func TestMergeDocsTaskRefusesWithoutABundle(t *testing.T) {
	svc, repos, _ := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	svc.SetTaskPRMerger(&fakeMerger{})
	_, err := svc.MergeDocsTask(context.Background(), repos.repo.ID)
	require.ErrorContains(t, err, "no reference-doc task is outstanding")
}

// The pull request was already merged — by an earlier call here, by the QA
// agent's own merge_task_pull_request, or by hand on GitHub. Retrying the
// merge is refused by the gate, but the screen must read that as "done", not
// as a failure: it re-reads the task for the commit the gate just recorded
// and still forgets the bundle.
func TestMergeDocsTaskTreatsAlreadyMergedAsDoneAndForgetsTheTask(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	svc.SetTaskPRMerger(&fakeMerger{err: domain.ErrMergeAlreadyMerged})

	task, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture},
	})
	require.NoError(t, err)

	// The gate would have recorded these on the task before refusing.
	tasks.task.PRNumber = 12
	tasks.task.PRURL = "https://github.com/acme/app/pull/12"
	tasks.task.MergeCommitSHA = "abc1234567890000000000000000000000000000"

	result, err := svc.MergeDocsTask(context.Background(), repos.repo.ID)
	require.NoError(t, err)
	require.True(t, result.Merged)
	require.Equal(t, "abc1234567890000000000000000000000000000", result.MergeCommitSHA)
	require.Contains(t, result.Message, "already merged")
	require.Equal(t, []string{task.ID.String(), ""}, repos.docsTaskIDs, "an already-merged PR still clears the task")
}

// A refused merge leaves the task recorded: the docs are not on the default
// branch, and forgetting the card would strand the branch.
func TestMergeDocsTaskKeepsTheTaskWhenTheMergeIsRefused(t *testing.T) {
	svc, repos, _ := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	svc.SetTaskPRMerger(&fakeMerger{err: errors.New("merge refused: the task is in `code_review`")})

	task, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocArchitecture},
	})
	require.NoError(t, err)

	_, err = svc.MergeDocsTask(context.Background(), repos.repo.ID)
	require.Error(t, err)
	require.Equal(t, []string{task.ID.String()}, repos.docsTaskIDs)
	require.Equal(t, task.ID.String(), repos.repo.DocsTaskID)
}

func TestBundleDescriptionListsEveryDocBeforeDetailingThem(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	_, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocCodingStandards},
		{Kind: domain.RepoDocTestStandards},
	})
	require.NoError(t, err)
	desc := tasks.created[0].Description
	require.Contains(t, desc, "1. `.ai/coding-standards.md`")
	require.Contains(t, desc, "2. `.ai/test-standards.md`")
	// The numbered list comes before the per-doc sections, so an agent reading
	// the first paragraph already knows how many files it owes.
	require.Less(t, strings.Index(desc, "1. `.ai/coding-standards.md`"),
		strings.Index(desc, "## `.ai/coding-standards.md`"))
}
