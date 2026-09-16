package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeReleaseGit is a port.GitClient whose only interesting answers are the
// two the release-identity checks ask for: "is there a working copy" and "what
// commit is the task branch on". Everything else is inert.
type fakeReleaseGit struct {
	hasGit  bool
	headSHA string
	infoErr error
	// presence overrides what Presence answers. Zero value = derived from
	// hasGit, so every test that only cares about the gate stays as it was.
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

// fakeReleaseRepoStore serves one repository row; Get is what most tests
// exercise, plus UpdateRootPath for the working-copy restore.
type fakeReleaseRepoStore struct {
	repo domain.Repository
	// rootPathWrites records every root_path the service saved, in order, so a
	// restore test can assert the row was re-pointed (and that a refused
	// restore wrote nothing).
	rootPathWrites []string
	rootPathErr    error
	// subProjectWrites records every sub-project list the service saved, in
	// order, so an Open() test can assert detection was persisted (or wasn't,
	// when the caller supplied an explicit kind).
	subProjectWrites [][]domain.RepoSubProject
	// getByRootPathErr, set, makes GetByRootPath report "not found" so Open()
	// takes its new-registration branch instead of its re-open branch. Unset
	// (nil), it keeps the original zero-value/no-error behaviour every other
	// test here relies on.
	getByRootPathErr error
	// mobilePlatformWrites records every mobile_platform the service saved, so
	// an Open() test can assert the platform detection was persisted.
	mobilePlatformWrites []string
	// appIdentityWrites records every detected app identity the service saved,
	// the Open() twin of mobilePlatformWrites.
	appIdentityWrites []domain.AppIdentity
	buildTargetWrites []domain.BuildTargets
	// releaseEngineWrites records every release_engine the service saved.
	releaseEngineWrites []string
	// docsTaskWrites records every docs_task_id written, "" included.
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
func (f *fakeReleaseRepoStore) UpdateMeta(_ context.Context, _ uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error) {
	if kind != nil {
		f.repo.Kind = *kind
	}
	if subRepoKinds != nil {
		f.repo.SubRepoKinds = *subRepoKinds
	}
	if autoReleaseOnDone != nil {
		f.repo.AutoReleaseOnDone = *autoReleaseOnDone
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
func (f *fakeReleaseRepoStore) UpdateMutationGate(_ context.Context, _ uuid.UUID, enabled *bool, threshold *float64) (domain.Repository, error) {
	if enabled != nil {
		f.repo.MutationEnabled = *enabled
	}
	if threshold != nil {
		f.repo.MutationThreshold = *threshold
	}
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
func (f *fakeReleaseRepoStore) UpdateLifecycleGates(context.Context, uuid.UUID, *bool, *bool, *bool, *bool, *float64) (domain.Repository, error) {
	return domain.Repository{}, nil
}
func (f *fakeReleaseRepoStore) UpdateProfile(context.Context, uuid.UUID, string) (domain.Repository, error) {
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

// fakeReleaseTaskStore serves one task row; Update echoes what it is given so
// the stamp written by a column move can be asserted.
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

// fakeReleasePipelineStore counts the pipeline rows a dispatch creates — the
// evidence that TriggerRelease got past every gate.
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

// ClaimTerminal mirrors the real guard: a pipeline reaches a terminal state
// once, and a second caller is told it lost. See port.TaskPipelineStore.
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

// fakeReleaseComments captures the system comments a block posts, so "the
// refusal is visible on the board" is asserted rather than assumed.
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

// TriggerRelease must dispatch only when the branch still carries the commit
// the task was signed off at. Every other outcome — moved branch, no stamp, no
// workspace, git failure — blocks: an unprovable release target is exactly the
// case this gate exists for.
func TestTriggerReleaseChecksReleaseTargetBeforeDispatch(t *testing.T) {
	cases := []struct {
		name         string
		verifiedSHA  string
		headSHA      string
		hasGit       bool
		gitErr       error
		wantDispatch bool
		wantErr      error
	}{
		{
			name:         "target unchanged dispatches",
			verifiedSHA:  verifiedCommit,
			headSHA:      verifiedCommit,
			hasGit:       true,
			wantDispatch: true,
		},
		{
			name:         "sha casing is not a mismatch",
			verifiedSHA:  strings.ToUpper(verifiedCommit[:8]) + verifiedCommit[8:],
			headSHA:      verifiedCommit,
			hasGit:       true,
			wantDispatch: true,
		},
		{
			name:        "target moved is blocked",
			verifiedSHA: verifiedCommit,
			headSHA:     movedCommit,
			hasGit:      true,
			wantErr:     domain.ErrReleaseTargetMoved,
		},
		{
			name:        "no verified commit fails closed",
			verifiedSHA: "",
			headSHA:     verifiedCommit,
			hasGit:      true,
			wantErr:     domain.ErrReleaseTargetUnverified,
		},
		{
			name:        "missing workspace fails closed",
			verifiedSHA: verifiedCommit,
			headSHA:     verifiedCommit,
			hasGit:      false,
			wantErr:     domain.ErrReleaseTargetUnverified,
		},
		{
			name:        "git failure fails closed",
			verifiedSHA: verifiedCommit,
			hasGit:      true,
			gitErr:      errors.New("boom: git is wedged"),
			wantErr:     domain.ErrReleaseTargetUnverified,
		},
		{
			name:        "empty head sha fails closed",
			verifiedSHA: verifiedCommit,
			headSHA:     "",
			hasGit:      true,
			wantErr:     domain.ErrReleaseTargetUnverified,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoID, taskID := uuid.New(), uuid.New()
			pipelineStore := &fakeReleasePipelineStore{}
			comments := &fakeReleaseComments{}
			svc := &Service{
				repos: &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID, AutoReleaseOnDone: true}},
				tasks: &fakeReleaseTaskStore{task: domain.BoardTask{
					ID:           taskID,
					RepositoryID: repoID,
					Column:       domain.TaskColumnDone,
					VerifiedSHA:  tc.verifiedSHA,
				}},
				git:           &fakeReleaseGit{hasGit: tc.hasGit, headSHA: tc.headSHA, infoErr: tc.gitErr},
				workspaceRoot: t.TempDir(),
				comments:      comments,
				pipelines:     board.NewPipelineRunner(board.PipelineRunnerDeps{Store: pipelineStore}),
			}

			_, err := svc.TriggerRelease(context.Background(), repoID, taskID)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if len(pipelineStore.created) != 0 {
					t.Fatalf("a blocked release must not dispatch anything, got %d pipelines", len(pipelineStore.created))
				}
				if len(comments.comments) != 1 {
					t.Fatalf("want the block explained on the task, got %d comments", len(comments.comments))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantDispatch {
				t.Fatalf("test case declares neither an error nor a dispatch")
			}
			if len(pipelineStore.created) != 1 {
				t.Fatalf("want exactly one dispatched pipeline, got %d", len(pipelineStore.created))
			}
			if got := pipelineStore.created[0].Trigger; got != domain.PipelineTriggerProdDeploy {
				t.Fatalf("want a prod deploy trigger, got %q", got)
			}
			if len(comments.comments) != 0 {
				t.Fatalf("a clean release must not comment a block, got %+v", comments.comments)
			}
		})
	}
}

// The blocked-release error is what the operator and the release agent read:
// it has to name both commits, not just say "mismatch".
func TestReleaseTargetGateErrorNamesBothCommits(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	svc := &Service{
		git:           &fakeReleaseGit{hasGit: true, headSHA: movedCommit},
		workspaceRoot: t.TempDir(),
	}
	err := svc.releaseTargetGate(context.Background(), repoID,
		domain.BoardTask{ID: taskID, RepositoryID: repoID, VerifiedSHA: verifiedCommit})
	if !errors.Is(err, domain.ErrReleaseTargetMoved) {
		t.Fatalf("want ErrReleaseTargetMoved, got %v", err)
	}
	for _, want := range []string{verifiedCommit[:12], movedCommit[:12]} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q named in the block reason, got: %v", want, err)
		}
	}
}

// The stamp is what makes the gate a real check, so the moves that write it
// (and the moves that withdraw it) are pinned down here.
func TestVerifiedSHAForMove(t *testing.T) {
	cases := []struct {
		name    string
		prev    domain.TaskColumn
		next    domain.TaskColumn
		stamped string // stamp already on the task
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

// Nothing about this gate may depend on git being wired: a control plane
// without a git client cannot identify any commit, so it must refuse the
// release rather than treat "cannot check" as "checked and fine".
func TestReleaseTargetGateWithoutGitFailsClosed(t *testing.T) {
	svc := &Service{}
	err := svc.releaseTargetGate(context.Background(), uuid.New(),
		domain.BoardTask{ID: uuid.New(), VerifiedSHA: verifiedCommit})
	if !errors.Is(err, domain.ErrReleaseTargetUnverified) {
		t.Fatalf("want ErrReleaseTargetUnverified, got %v", err)
	}
}

// --- port.BoardTaskStore / port.GitClient additions (migration 105) ---

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

// ---------------------------------------------------------------------------
// The done-column assertion.
//
// trigger_release's tool description has always said "for a task that is in the
// done column", and its task_id parameter has always said "must be in the done
// column". Nothing checked it. An agent could release a card sitting in
// in_progress or need_revision, and the gate that eventually caught it was the
// release-target gate complaining about a missing verified commit — which is
// true, but describes a symptom rather than the mistake.

func TestTriggerReleaseRefusesOutsideDone(t *testing.T) {
	blocked := []domain.TaskColumn{
		domain.TaskColumnTodo,
		domain.TaskColumnInProgress,
		domain.TaskColumnCodeReview,
		domain.TaskColumnInQA,
		domain.TaskColumnPMUAT,
		domain.TaskColumnNeedRevision,
	}
	for _, col := range blocked {
		t.Run(string(col), func(t *testing.T) {
			repoID, taskID := uuid.New(), uuid.New()
			pipelineStore := &fakeReleasePipelineStore{}
			comments := &fakeReleaseComments{}
			svc := &Service{
				repos: &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID, AutoReleaseOnDone: true}},
				tasks: &fakeReleaseTaskStore{task: domain.BoardTask{
					ID: taskID, RepositoryID: repoID, Column: col,
					// Deliberately stamped and consistent: without the column
					// gate this task would sail through every other check.
					VerifiedSHA: verifiedCommit,
				}},
				git:           &fakeReleaseGit{hasGit: true, headSHA: verifiedCommit},
				workspaceRoot: t.TempDir(),
				comments:      comments,
				pipelines:     board.NewPipelineRunner(board.PipelineRunnerDeps{Store: pipelineStore}),
			}

			_, err := svc.TriggerRelease(context.Background(), repoID, taskID)
			if !errors.Is(err, ErrReleaseNotDone) {
				t.Fatalf("err = %v, want ErrReleaseNotDone", err)
			}
			if len(pipelineStore.created) != 0 {
				t.Fatalf("a task in %s must not dispatch a deploy, got %d pipelines", col, len(pipelineStore.created))
			}
			// The block has to be visible on the board, not only in a failed
			// API call — same contract as the migration and identity gates.
			if len(comments.comments) != 1 {
				t.Fatalf("want exactly one system comment explaining the block, got %d", len(comments.comments))
			}
			if !strings.Contains(comments.comments[0].Content, string(col)) {
				t.Fatalf("the comment should name the column the task is actually in: %q", comments.comments[0].Content)
			}
		})
	}
}

// done releases, and so does released — a re-release of an already-released
// task is legitimate (a deploy package replaying its members, a re-run after an
// infrastructure failure) and the card has moved on by then.
func TestTriggerReleaseAllowsDoneAndReleased(t *testing.T) {
	for _, col := range []domain.TaskColumn{domain.TaskColumnDone, domain.TaskColumnReleased} {
		t.Run(string(col), func(t *testing.T) {
			repoID, taskID := uuid.New(), uuid.New()
			pipelineStore := &fakeReleasePipelineStore{}
			svc := &Service{
				repos: &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID, AutoReleaseOnDone: true}},
				tasks: &fakeReleaseTaskStore{task: domain.BoardTask{
					ID: taskID, RepositoryID: repoID, Column: col, VerifiedSHA: verifiedCommit,
				}},
				git:           &fakeReleaseGit{hasGit: true, headSHA: verifiedCommit},
				workspaceRoot: t.TempDir(),
				comments:      &fakeReleaseComments{},
				pipelines:     board.NewPipelineRunner(board.PipelineRunnerDeps{Store: pipelineStore}),
			}

			if _, err := svc.TriggerRelease(context.Background(), repoID, taskID); err != nil {
				t.Fatalf("TriggerRelease from %s: %v", col, err)
			}
			if len(pipelineStore.created) != 1 {
				t.Fatalf("want one deploy pipeline, got %d", len(pipelineStore.created))
			}
		})
	}
}
