package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeReleaseGit struct {
	hasGit  bool
	headSHA string
	infoErr error

	presence *domain.GitPresence
}

func (f *fakeReleaseGit) HasGit(string) bool { return f.hasGit }

func (f *fakeReleaseGit) Presence(string) domain.GitPresence {
	if f.presence != nil {
		return *f.presence
	}
	if f.hasGit {
		return domain.GitPresence{State: domain.GitPresenceRepository}
	}
	return domain.GitPresence{State: domain.GitPresenceNoRepository}
}
func (f *fakeReleaseGit) TaskGitInfo(context.Context, string) (domain.TaskGitInfo, error) {
	if f.infoErr != nil {
		return domain.TaskGitInfo{}, f.infoErr
	}
	return domain.TaskGitInfo{Owner: "acme", Repo: "app", Branch: "task-1", HeadSHA: f.headSHA}, nil
}
func (f *fakeReleaseGit) Status(context.Context, string) domain.GitStatus { return domain.GitStatus{} }
func (f *fakeReleaseGit) EnsureRepoWithRemote(context.Context, string, string, string) error {
	return nil
}
func (f *fakeReleaseGit) CloneRepo(context.Context, string, string) error { return nil }
func (f *fakeReleaseGit) EnsureTaskWorkspace(context.Context, string, string, string) error {
	return nil
}
func (f *fakeReleaseGit) OriginURL(context.Context, string) string            { return "" }
func (f *fakeReleaseGit) FetchLatest(context.Context, string) error           { return nil }
func (f *fakeReleaseGit) DefaultBranch(context.Context, string) string        { return "main" }
func (f *fakeReleaseGit) SyncDefaultBranch(context.Context, string) error     { return nil }
func (f *fakeReleaseGit) CommitAndPush(context.Context, string, string) error { return nil }
func (f *fakeReleaseGit) PushBranch(context.Context, string) error            { return nil }
func (f *fakeReleaseGit) EnsurePullRequest(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeReleaseGit) MergePullRequest(context.Context, domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error) {
	return domain.PullRequestMergeResult{}, nil
}
func (f *fakeReleaseGit) TaskDiff(context.Context, string) (string, error) { return "", nil }
func (f *fakeReleaseGit) TaskChangedFiles(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *fakeReleaseGit) ChangedFilesSince(context.Context, string, string) ([]string, error) {
	return nil, nil
}

type fakeReleaseRepoStore struct {
	repo domain.Repository

	rootPathWrites []string
	rootPathErr    error

	subProjectWrites [][]domain.RepoSubProject

	getByRootPathErr error

	mobilePlatformWrites []string

	appIdentityWrites []domain.AppIdentity
	buildTargetWrites []domain.BuildTargets

	releaseEngineWrites []string

	docsTaskWrites []string
}

func (f *fakeReleaseRepoStore) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

func (f *fakeReleaseRepoStore) UpdateRootPath(_ context.Context, _ uuid.UUID, rootPath string) (domain.Repository, error) {
	if f.rootPathErr != nil {
		return domain.Repository{}, f.rootPathErr
	}
	f.rootPathWrites = append(f.rootPathWrites, rootPath)
	f.repo.RootPath = rootPath
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) Create(_ context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error) {
	f.repo = domain.Repository{ID: uuid.New(), Name: name, Description: description, RootPath: rootPath, RemoteURL: remoteURL, Kind: kind}
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateRemoteURL(context.Context, uuid.UUID, string) (domain.Repository, error) {
	return domain.Repository{}, nil
}
func (f *fakeReleaseRepoStore) GetByRootPath(context.Context, string) (domain.Repository, error) {
	if f.getByRootPathErr != nil {
		return domain.Repository{}, f.getByRootPathErr
	}
	return domain.Repository{}, nil
}
func (f *fakeReleaseRepoStore) List(context.Context) ([]domain.Repository, error) { return nil, nil }
func (f *fakeReleaseRepoStore) Update(_ context.Context, _ uuid.UUID, name, description string, _, _, _ *string, _ *bool) (domain.Repository, error) {
	if name != "" {
		f.repo.Name = name
	}
	if description != "" {
		f.repo.Description = description
	}
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateMeta(_ context.Context, _ uuid.UUID, kind *string, subRepoKinds *[]string) (domain.Repository, error) {
	if kind != nil {
		f.repo.Kind = *kind
	}
	if subRepoKinds != nil {
		f.repo.SubRepoKinds = *subRepoKinds
	}
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateMobilePlatform(_ context.Context, _ uuid.UUID, platform string) (domain.Repository, error) {
	f.mobilePlatformWrites = append(f.mobilePlatformWrites, platform)
	f.repo.MobilePlatform = platform
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateReleaseEngine(_ context.Context, _ uuid.UUID, engine string) (domain.Repository, error) {
	f.releaseEngineWrites = append(f.releaseEngineWrites, engine)
	f.repo.ReleaseEngine = engine
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateDetectedAppIdentity(_ context.Context, _ uuid.UUID, identity domain.AppIdentity) (domain.Repository, error) {
	f.appIdentityWrites = append(f.appIdentityWrites, identity)
	f.repo.DetectedAppIdentity = identity
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateDetectedBuildTargets(_ context.Context, _ uuid.UUID, targets domain.BuildTargets) (domain.Repository, error) {
	f.buildTargetWrites = append(f.buildTargetWrites, targets)
	f.repo.DetectedBuildTargets = targets
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateQualityGates(_ context.Context, _ uuid.UUID, coverage, mutation domain.QualityGate) (domain.Repository, error) {
	f.repo.RequireOverallCoverage = coverage.Enabled
	f.repo.CoverageThreshold = coverage.Threshold
	f.repo.MutationEnabled = mutation.Enabled
	f.repo.MutationThreshold = mutation.Threshold
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) SetDocsTaskID(_ context.Context, _ uuid.UUID, taskID string) error {
	f.docsTaskWrites = append(f.docsTaskWrites, taskID)
	f.repo.DocsTaskID = taskID
	return nil
}
func (f *fakeReleaseRepoStore) UpdateSubProjects(_ context.Context, _ uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error) {
	f.subProjectWrites = append(f.subProjectWrites, subProjects)
	f.repo.SubProjects = subProjects
	return f.repo, nil
}
func (f *fakeReleaseRepoStore) UpdateIncidentPolicy(context.Context, uuid.UUID, domain.IncidentPolicy) (domain.Repository, error) {
	return domain.Repository{}, nil
}
func (f *fakeReleaseRepoStore) UpdateTestStrategy(context.Context, uuid.UUID, string) (domain.Repository, error) {
	return domain.Repository{}, nil
}
func (f *fakeReleaseRepoStore) UpdateDocs(context.Context, uuid.UUID, domain.RepositoryDocs) (domain.Repository, error) {
	return domain.Repository{}, nil
}
func (f *fakeReleaseRepoStore) SetWebhook(context.Context, uuid.UUID, string, int64) error {
	return nil
}
func (f *fakeReleaseRepoStore) WebhookSecret(context.Context, uuid.UUID) (string, error) {
	return "", nil
}
func (f *fakeReleaseRepoStore) Delete(context.Context, uuid.UUID) error { return nil }
func (f *fakeReleaseRepoStore) SetProjects(context.Context, uuid.UUID, []uuid.UUID) error {
	return nil
}
func (f *fakeReleaseRepoStore) ListProjectIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeReleaseRepoStore) ListProjectIDsByRepositories(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return nil, nil
}

type fakeReleaseTaskStore struct {
	task    domain.BoardTask
	updated domain.BoardTask
}

func (f *fakeReleaseTaskStore) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return f.task, nil
}
func (f *fakeReleaseTaskStore) Update(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	f.updated = task
	return task, nil
}
func (f *fakeReleaseTaskStore) Create(context.Context, domain.BoardTask) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeReleaseTaskStore) GetByNumber(context.Context, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeReleaseTaskStore) LookupByKey(context.Context, string, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeReleaseTaskStore) ListByRepository(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeReleaseTaskStore) ListAll(context.Context) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeReleaseTaskStore) ListBoardVisible(context.Context, time.Time) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeReleaseTaskStore) ListReleasedArchive(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeReleaseTaskStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeReleaseTaskStore) ClaimAssignee(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeReleaseTaskStore) MarkCompleted(context.Context, uuid.UUID, bool, time.Time) error {
	return nil
}
func (f *fakeReleaseTaskStore) SetMigrationFlag(context.Context, uuid.UUID, bool) error { return nil }
func (f *fakeReleaseTaskStore) SetTaskPullRequest(context.Context, uuid.UUID, string, int) error {
	return nil
}
func (f *fakeReleaseTaskStore) SetTaskMergeCommit(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *fakeReleaseTaskStore) MarkStageVerified(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (f *fakeReleaseTaskStore) ClearStageVerification(context.Context, uuid.UUID) error { return nil }
func (f *fakeReleaseTaskStore) NextTaskNumber(context.Context, domain.TaskType) (int, error) {
	return 1, nil
}
func (f *fakeReleaseTaskStore) BlockOnQuestion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakeReleaseTaskStore) TakeBlockedBySession(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
func (f *fakeReleaseTaskStore) BlockOnResource(context.Context, uuid.UUID, uuid.UUID, string, string) (domain.TaskColumn, error) {
	return "", nil
}

func (f *fakeReleaseTaskStore) MarkWorkOrderWaiting(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeReleaseTaskStore) ClearWorkOrderWaiting(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeReleaseTaskStore) TakeBlockedByResource(context.Context, string) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeReleaseTaskStore) TakeQuotaResumable(context.Context, time.Time) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeReleaseTaskStore) BlockOnCancel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeReleaseTaskStore) ConfirmBeforeDeploy(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}

type fakeReleasePipelineStore struct {
	created []domain.TaskPipeline
}

func (f *fakeReleasePipelineStore) Create(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	p.ID = uuid.New()
	f.created = append(f.created, p)
	return p, nil
}
func (f *fakeReleasePipelineStore) Update(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	return p, nil
}

func (f *fakeReleasePipelineStore) ClaimTerminal(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, bool, error) {
	out, err := f.Update(ctx, p)
	return out, err == nil, err
}

func (f *fakeReleasePipelineStore) Get(context.Context, uuid.UUID) (domain.TaskPipeline, error) {
	return domain.TaskPipeline{}, domain.ErrPipelineNotFound
}
func (f *fakeReleasePipelineStore) ListByTask(context.Context, uuid.UUID) ([]domain.TaskPipeline, error) {
	return nil, nil
}
func (f *fakeReleasePipelineStore) LatestByTask(context.Context, uuid.UUID) (domain.TaskPipeline, error) {
	return domain.TaskPipeline{}, domain.ErrPipelineNotFound
}
func (f *fakeReleasePipelineStore) LatestStatusByTasks(context.Context, []uuid.UUID) (map[uuid.UUID]domain.TaskPipelineDigest, error) {
	return nil, nil
}
func (f *fakeReleasePipelineStore) SupersedePending(context.Context, uuid.UUID) error { return nil }
func (f *fakeReleasePipelineStore) FailStaleRunning(context.Context, int) error       { return nil }
func (f *fakeReleasePipelineStore) ListUnfinished(context.Context, int) ([]domain.TaskPipeline, error) {
	return nil, nil
}
func (f *fakeReleasePipelineStore) ListUnfinishedByHeadSHA(context.Context, uuid.UUID, string) ([]domain.TaskPipeline, error) {
	return nil, nil
}
func (f *fakeReleasePipelineStore) CreateJob(_ context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	return j, nil
}
func (f *fakeReleasePipelineStore) UpdateJob(_ context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	return j, nil
}
func (f *fakeReleasePipelineStore) ListJobs(context.Context, uuid.UUID) ([]domain.TaskPipelineJob, error) {
	return nil, nil
}

type fakeReleaseComments struct {
	comments []domain.TaskComment
}

func (f *fakeReleaseComments) Create(_ context.Context, c domain.TaskComment) (domain.TaskComment, error) {
	f.comments = append(f.comments, c)
	return c, nil
}
func (f *fakeReleaseComments) ListByTask(context.Context, uuid.UUID) ([]domain.TaskComment, error) {
	return nil, nil
}

const (
	verifiedCommit = "1111111111111111111111111111111111111111"
	movedCommit    = "2222222222222222222222222222222222222222"
)

func TestVerifiedSHAForMove(t *testing.T) {
	cases := []struct {
		name    string
		prev    domain.TaskColumn
		next    domain.TaskColumn
		stamped string
		hasGit  bool
		headSHA string
		want    string
	}{
		{
			name:    "entering done stamps the branch head",
			prev:    domain.TaskColumnPMUAT,
			next:    domain.TaskColumnDone,
			hasGit:  true,
			headSHA: verifiedCommit,
			want:    verifiedCommit,
		},
		{
			name:    "entering done re-stamps over an older sign-off",
			prev:    domain.TaskColumnPMUAT,
			next:    domain.TaskColumnDone,
			stamped: verifiedCommit,
			hasGit:  true,
			headSHA: movedCommit,
			want:    movedCommit,
		},
		{
			name:    "entering done without a workspace leaves no stamp",
			prev:    domain.TaskColumnPMUAT,
			next:    domain.TaskColumnDone,
			stamped: verifiedCommit,
			hasGit:  false,
			want:    "",
		},
		{
			name:    "leaving done withdraws the sign-off",
			prev:    domain.TaskColumnDone,
			next:    domain.TaskColumnNeedRevision,
			stamped: verifiedCommit,
			hasGit:  true,
			headSHA: verifiedCommit,
			want:    "",
		},
		{
			name:    "done to released keeps the released commit",
			prev:    domain.TaskColumnDone,
			next:    domain.TaskColumnReleased,
			stamped: verifiedCommit,
			hasGit:  true,
			headSHA: verifiedCommit,
			want:    verifiedCommit,
		},
		{
			name:    "unrelated moves carry the stamp through",
			prev:    domain.TaskColumnCodeReview,
			next:    domain.TaskColumnReadyForQA,
			stamped: verifiedCommit,
			hasGit:  true,
			headSHA: movedCommit,
			want:    verifiedCommit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{
				git:           &fakeReleaseGit{hasGit: tc.hasGit, headSHA: tc.headSHA},
				workspaceRoot: t.TempDir(),
			}
			task := domain.BoardTask{ID: uuid.New(), VerifiedSHA: tc.stamped}
			if got := svc.verifiedSHAForMove(context.Background(), task, tc.prev, tc.next); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func (f *fakeReleaseTaskStore) FindTaskByMergeCommit(context.Context, uuid.UUID, string) (domain.BoardTask, error) {
	return domain.BoardTask{}, errors.New("not found")
}

func (f *fakeReleaseTaskStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakeReleaseTaskStore) TakeBlockedResourceTask(context.Context, string, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeReleaseGit) RevertCommitOnDefaultBranch(context.Context, string, string, string) (string, error) {
	return "", nil
}

