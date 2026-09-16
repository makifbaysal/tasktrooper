package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakePackageStore is an in-memory port.DeployPackageStore. Membership is kept
// as the ids the caller supplied, so position is the slice index — exactly what
// the real store writes.
type fakePackageStore struct {
	pkg     domain.DeployPackage
	members []domain.DeployPackageTask
	updates int
}

func (f *fakePackageStore) Create(_ context.Context, pkg domain.DeployPackage) (domain.DeployPackage, error) {
	pkg.ID = uuid.New()
	f.pkg = pkg
	return pkg, nil
}

func (f *fakePackageStore) Get(context.Context, uuid.UUID, uuid.UUID) (domain.DeployPackage, error) {
	if f.pkg.ID == uuid.Nil {
		return domain.DeployPackage{}, fmt.Errorf("no package: %w", port.ErrNotFound)
	}
	return f.pkg, nil
}

func (f *fakePackageStore) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployPackage, error) {
	if f.pkg.ID == uuid.Nil {
		return nil, nil
	}
	return []domain.DeployPackage{f.pkg}, nil
}

func (f *fakePackageStore) Update(_ context.Context, _, _ uuid.UUID, name, description, status, note *string) (domain.DeployPackage, error) {
	f.updates++
	if name != nil {
		f.pkg.Name = *name
	}
	if description != nil {
		f.pkg.Description = *description
	}
	if status != nil {
		f.pkg.Status = *status
	}
	if note != nil {
		f.pkg.Note = *note
	}
	return f.pkg, nil
}

func (f *fakePackageStore) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	f.pkg = domain.DeployPackage{}
	return nil
}

func (f *fakePackageStore) ReplaceTasks(_ context.Context, _ uuid.UUID, taskIDs []uuid.UUID) error {
	f.members = nil
	for i, id := range taskIDs {
		f.members = append(f.members, domain.DeployPackageTask{TaskID: id, Position: i})
	}
	return nil
}

func (f *fakePackageStore) ListTasks(context.Context, uuid.UUID) ([]domain.DeployPackageTask, error) {
	out := make([]domain.DeployPackageTask, len(f.members))
	copy(out, f.members)
	return out, nil
}

// fakePackageTaskStore serves several tasks by id — the release-gate fake only
// ever holds one, and a package needs a board.
type fakePackageTaskStore struct {
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *fakePackageTaskStore) Get(_ context.Context, _ uuid.UUID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, fmt.Errorf("task %s not found", taskID)
	}
	return task, nil
}
func (f *fakePackageTaskStore) Update(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	f.tasks[task.ID] = task
	return task, nil
}
func (f *fakePackageTaskStore) Create(context.Context, domain.BoardTask) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) GetByNumber(context.Context, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) LookupByKey(context.Context, string, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) ListByRepository(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}

// ListAll answers FindTaskRepositoryID, which the relation guards use to render
// a task as "T-12 (API migration)" in their error messages. Returning nil made
// every such message fall back to a UUID.
func (f *fakePackageTaskStore) ListAll(context.Context) ([]domain.BoardTask, error) {
	out := make([]domain.BoardTask, 0, len(f.tasks))
	for _, task := range f.tasks {
		out = append(out, task)
	}
	return out, nil
}

func (f *fakePackageTaskStore) ListBoardVisible(ctx context.Context, _ time.Time) ([]domain.BoardTask, error) {
	return f.ListAll(ctx)
}

func (f *fakePackageTaskStore) ListReleasedArchive(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakePackageTaskStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakePackageTaskStore) ClaimAssignee(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) MarkCompleted(context.Context, uuid.UUID, bool, time.Time) error {
	return nil
}
func (f *fakePackageTaskStore) SetMigrationFlag(context.Context, uuid.UUID, bool) error { return nil }
func (f *fakePackageTaskStore) SetTaskPullRequest(context.Context, uuid.UUID, string, int) error {
	return nil
}
func (f *fakePackageTaskStore) SetTaskMergeCommit(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *fakePackageTaskStore) MarkStageVerified(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (f *fakePackageTaskStore) ClearStageVerification(context.Context, uuid.UUID) error { return nil }
func (f *fakePackageTaskStore) NextTaskNumber(context.Context, domain.TaskType) (int, error) {
	return 1, nil
}
func (f *fakePackageTaskStore) BlockOnQuestion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakePackageTaskStore) TakeBlockedBySession(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
func (f *fakePackageTaskStore) BlockOnResource(context.Context, uuid.UUID, uuid.UUID, string, string) (domain.TaskColumn, error) {
	return "", nil
}

func (f *fakePackageTaskStore) MarkWorkOrderWaiting(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakePackageTaskStore) ClearWorkOrderWaiting(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakePackageTaskStore) TakeBlockedByResource(context.Context, string) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakePackageTaskStore) TakeQuotaResumable(context.Context, time.Time) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakePackageTaskStore) BlockOnCancel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

// fakeRelationStore answers deploy_depends_on lookups from a plain
// source → targets map.
type fakeRelationStore struct {
	deps    map[uuid.UUID][]uuid.UUID
	listErr error
	// replaced records the last ReplaceForTaskOfType call so the "only one type
	// is touched" contract can be asserted.
	replaced map[uuid.UUID][]uuid.UUID
}

func (f *fakeRelationStore) ReplaceForTask(context.Context, uuid.UUID, []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	return nil, nil
}

func (f *fakeRelationStore) ReplaceForTaskOfType(_ context.Context, sourceTaskID uuid.UUID, relType domain.TaskRelationType, rels []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	if f.replaced == nil {
		f.replaced = map[uuid.UUID][]uuid.UUID{}
	}
	var targets []uuid.UUID
	out := make([]domain.TaskRelation, 0, len(rels))
	for _, rel := range rels {
		targets = append(targets, rel.TargetTaskID)
		out = append(out, domain.TaskRelation{
			ID: uuid.New(), SourceTaskID: sourceTaskID, TargetTaskID: rel.TargetTaskID, RelationType: relType,
		})
	}
	f.replaced[sourceTaskID] = targets
	return out, nil
}

func (f *fakeRelationStore) ListBySource(_ context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.TaskRelation
	for _, target := range f.deps[sourceTaskID] {
		out = append(out, domain.TaskRelation{
			SourceTaskID: sourceTaskID,
			TargetTaskID: target,
			RelationType: domain.TaskRelationDeployDependsOn,
			TargetKey:    "T-" + target.String()[:4],
		})
	}
	return out, nil
}

func (f *fakeRelationStore) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakeRelationStore) ListBlockedBy(context.Context, uuid.UUID) ([]domain.TaskRelation, error) {
	return nil, nil
}

func (f *fakeRelationStore) AddBlockers(_ context.Context, targetTaskID uuid.UUID, sourceTaskIDs []uuid.UUID) ([]domain.TaskRelation, error) {
	out := make([]domain.TaskRelation, 0, len(sourceTaskIDs))
	for _, sourceID := range sourceTaskIDs {
		out = append(out, domain.TaskRelation{
			ID: uuid.New(), SourceTaskID: sourceID, TargetTaskID: targetTaskID, RelationType: domain.TaskRelationBlocks,
		})
	}
	return out, nil
}

// fakePackagePipelineStore keeps per-task pipeline history and records what a
// dispatch created — a dispatched deploy immediately becomes a pending run, the
// way the real store behaves, so the idempotency guard is exercised for real.
type fakePackagePipelineStore struct {
	runs    map[uuid.UUID][]domain.TaskPipeline
	created []domain.TaskPipeline
}

func (f *fakePackagePipelineStore) Create(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	p.ID = uuid.New()
	f.created = append(f.created, p)
	if f.runs == nil {
		f.runs = map[uuid.UUID][]domain.TaskPipeline{}
	}
	f.runs[p.TaskID] = append(f.runs[p.TaskID], p)
	return p, nil
}
func (f *fakePackagePipelineStore) Update(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	return p, nil
}

// ClaimTerminal mirrors the real guard: a pipeline reaches a terminal state
// once, and a second caller is told it lost. See port.TaskPipelineStore.
func (f *fakePackagePipelineStore) ClaimTerminal(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, bool, error) {
	out, err := f.Update(ctx, p)
	return out, err == nil, err
}
func (f *fakePackagePipelineStore) Get(context.Context, uuid.UUID) (domain.TaskPipeline, error) {
	return domain.TaskPipeline{}, domain.ErrPipelineNotFound
}
func (f *fakePackagePipelineStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskPipeline, error) {
	return f.runs[taskID], nil
}
func (f *fakePackagePipelineStore) LatestByTask(context.Context, uuid.UUID) (domain.TaskPipeline, error) {
	return domain.TaskPipeline{}, domain.ErrPipelineNotFound
}
func (f *fakePackagePipelineStore) LatestStatusByTasks(context.Context, []uuid.UUID) (map[uuid.UUID]domain.TaskPipelineDigest, error) {
	return nil, nil
}
func (f *fakePackagePipelineStore) SupersedePending(context.Context, uuid.UUID) error { return nil }
func (f *fakePackagePipelineStore) FailStaleRunning(context.Context, int) error       { return nil }
func (f *fakePackagePipelineStore) ListUnfinished(context.Context, int) ([]domain.TaskPipeline, error) {
	return nil, nil
}
func (f *fakePackagePipelineStore) ListUnfinishedByHeadSHA(context.Context, uuid.UUID, string) ([]domain.TaskPipeline, error) {
	return nil, nil
}
func (f *fakePackagePipelineStore) CreateJob(_ context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	return j, nil
}
func (f *fakePackagePipelineStore) UpdateJob(_ context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	return j, nil
}
func (f *fakePackagePipelineStore) ListJobs(context.Context, uuid.UUID) ([]domain.TaskPipelineJob, error) {
	return nil, nil
}

// packageFixture wires a service whose every release gate is satisfiable, so a
// test that sees no dispatch is seeing the package logic refuse — not an
// unrelated gate.
type packageFixture struct {
	svc       *Service
	repoID    uuid.UUID
	tasks     *fakePackageTaskStore
	packages  *fakePackageStore
	relations *fakeRelationStore
	pipelines *fakePackagePipelineStore
	comments  *fakeReleaseComments
}

// newPackageFixture builds a package of len(keys) done tasks, in the given
// order, each stamped at the commit its branch is on so releaseTargetGate
// passes. AutoReleaseOnDone is deliberately FALSE: a batched-release repository
// is the only kind that needs a deploy package, and the package path must work
// there.
func newPackageFixture(t *testing.T, keys ...string) *packageFixture {
	t.Helper()
	repoID := uuid.New()
	tasks := &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}}
	packages := &fakePackageStore{}
	pipelines := &fakePackagePipelineStore{runs: map[uuid.UUID][]domain.TaskPipeline{}}
	relations := &fakeRelationStore{deps: map[uuid.UUID][]uuid.UUID{}}
	comments := &fakeReleaseComments{}

	packages.pkg = domain.DeployPackage{
		ID:           uuid.New(),
		RepositoryID: repoID,
		Name:         "release train",
		Status:       domain.DeployPackageStatusDraft,
	}
	for i, key := range keys {
		id := uuid.New()
		tasks.tasks[id] = domain.BoardTask{
			ID:           id,
			RepositoryID: repoID,
			Key:          key,
			Column:       domain.TaskColumnDone,
			VerifiedSHA:  verifiedCommit,
		}
		packages.members = append(packages.members, domain.DeployPackageTask{TaskID: id, Position: i, Key: key})
	}

	svc := &Service{
		repos:          &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID, AutoReleaseOnDone: false}},
		tasks:          tasks,
		relations:      relations,
		comments:       comments,
		deployPackages: packages,
		pipelineStore:  pipelines,
		git:            &fakeReleaseGit{hasGit: true, headSHA: verifiedCommit},
		workspaceRoot:  t.TempDir(),
		pipelines:      board.NewPipelineRunner(board.PipelineRunnerDeps{Store: pipelines}),
	}
	return &packageFixture{
		svc: svc, repoID: repoID, tasks: tasks, packages: packages,
		relations: relations, pipelines: pipelines, comments: comments,
	}
}

// taskID looks a member up by its board key.
func (f *packageFixture) taskID(t *testing.T, key string) uuid.UUID {
	t.Helper()
	for _, m := range f.packages.members {
		if m.Key == key {
			return m.TaskID
		}
	}
	t.Fatalf("no package member with key %q", key)
	return uuid.Nil
}

// dependsOn records "later must deploy after earlier".
func (f *packageFixture) dependsOn(t *testing.T, later, earlier string) {
	t.Helper()
	l, e := f.taskID(t, later), f.taskID(t, earlier)
	f.relations.deps[l] = append(f.relations.deps[l], e)
}

// markLive puts a member in the released column, which is production evidence.
func (f *packageFixture) markLive(t *testing.T, key string) {
	t.Helper()
	id := f.taskID(t, key)
	task := f.tasks.tasks[id]
	task.Column = domain.TaskColumnReleased
	f.tasks.tasks[id] = task
	// A landed deploy is no longer in flight.
	f.pipelines.runs[id] = []domain.TaskPipeline{{
		TaskID: id, Trigger: domain.PipelineTriggerProdDeploy, Status: domain.PipelineStatusSuccess,
	}}
}

// dispatchedKeys names the tasks a dispatch created a deploy for, in order.
func (f *packageFixture) dispatchedKeys(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, p := range f.pipelines.created {
		for _, m := range f.packages.members {
			if m.TaskID == p.TaskID {
				out = append(out, m.Key)
			}
		}
	}
	return out
}

// A package's release order is its dependency order, and where dependencies do
// not constrain it, the order the human assembled it in.
func TestOrderMembersSortsByDeployDependencies(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		// deps maps "later" → "earlier it must deploy after".
		deps map[string]string
		want []string
	}{
		{
			name: "no dependencies keeps the assembled order",
			keys: []string{"DE-1", "DE-2", "DE-3"},
			want: []string{"DE-1", "DE-2", "DE-3"},
		},
		{
			name: "a dependency overrides the assembled order",
			keys: []string{"DE-1", "DE-2"},
			deps: map[string]string{"DE-1": "DE-2"},
			want: []string{"DE-2", "DE-1"},
		},
		{
			name: "a chain is fully linearised",
			keys: []string{"DE-1", "DE-2", "DE-3"},
			deps: map[string]string{"DE-1": "DE-2", "DE-2": "DE-3"},
			want: []string{"DE-3", "DE-2", "DE-1"},
		},
		{
			name: "unconstrained members keep their position among the constrained ones",
			keys: []string{"DE-1", "DE-2", "DE-3"},
			deps: map[string]string{"DE-1": "DE-3"},
			want: []string{"DE-2", "DE-3", "DE-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPackageFixture(t, tc.keys...)
			for later, earlier := range tc.deps {
				f.dependsOn(t, later, earlier)
			}
			members, _ := f.packages.ListTasks(context.Background(), f.packages.pkg.ID)
			ordered, err := f.svc.orderMembers(context.Background(), members)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var got []string
			for _, m := range ordered {
				got = append(got, m.Key)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("want order %v, got %v", tc.want, got)
			}
		})
	}
}

// A cycle has no release order. Picking one arbitrarily would dispatch a task
// whose own dependency gate is guaranteed to refuse it, so ordering must fail
// loudly and name the tasks involved.
func TestOrderMembersRejectsCycle(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-2", "DE-1")

	members, _ := f.packages.ListTasks(context.Background(), f.packages.pkg.ID)
	_, err := f.svc.orderMembers(context.Background(), members)
	if !errors.Is(err, domain.ErrDeployPackageCycle) {
		t.Fatalf("want ErrDeployPackageCycle, got %v", err)
	}
	for _, key := range []string{"DE-1", "DE-2"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("want %q named in the cycle error, got: %v", key, err)
		}
	}
}

// A dependency outside the package cannot be ordered by this package, so it
// must not be treated as an edge — deployDependencyGate is the authority on it.
func TestOrderMembersIgnoresDependenciesOutsideThePackage(t *testing.T) {
	f := newPackageFixture(t, "DE-1")
	outsider := uuid.New()
	f.relations.deps[f.taskID(t, "DE-1")] = []uuid.UUID{outsider}

	members, _ := f.packages.ListTasks(context.Background(), f.packages.pkg.ID)
	ordered, err := f.svc.orderMembers(context.Background(), members)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 1 || ordered[0].Key != "DE-1" {
		t.Fatalf("want the single member ordered, got %+v", ordered)
	}
}

// ReleasePackage starts the train: it dispatches only the members whose
// dependencies are already satisfied and leaves the package releasing. The
// second wave must NOT go out yet — its dependency's deploy is still running.
func TestReleasePackageDispatchesOnlyTheFirstWave(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-1", "DE-2") // DE-1 ships after DE-2

	pkg, err := f.svc.ReleasePackage(context.Background(), f.repoID, f.packages.pkg.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusReleasing {
		t.Fatalf("want the package releasing, got %q (note: %s)", pkg.Status, pkg.Note)
	}
	got := f.dispatchedKeys(t)
	if len(got) != 1 || got[0] != "DE-2" {
		t.Fatalf("want only the dependency dispatched, got %v", got)
	}
}

// The wave that could not go out on release goes out on the next read, once its
// dependency has actually landed. This is the whole reason advancement is lazy.
func TestAdvancePackageDispatchesTheNextWaveWhenTheDependencyLands(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-1", "DE-2")

	if _, err := f.svc.ReleasePackage(context.Background(), f.repoID, f.packages.pkg.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	f.markLive(t, "DE-2")

	pkg, err := f.svc.AdvancePackage(context.Background(), f.repoID, f.packages.pkg)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusReleasing {
		t.Fatalf("want still releasing while DE-1 deploys, got %q", pkg.Status)
	}
	got := f.dispatchedKeys(t)
	if len(got) != 2 || got[1] != "DE-1" {
		t.Fatalf("want DE-1 dispatched second, got %v", got)
	}
}

// Every member live → the train is released. Nothing else writes that status:
// it is evidence, not a claim.
func TestAdvancePackageReleasesWhenEveryMemberIsLive(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	f.packages.pkg.Status = domain.DeployPackageStatusReleasing
	f.markLive(t, "DE-1")
	f.markLive(t, "DE-2")

	pkg, err := f.svc.AdvancePackage(context.Background(), f.repoID, f.packages.pkg)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusReleased {
		t.Fatalf("want released, got %q", pkg.Status)
	}
	if len(f.pipelines.created) != 0 {
		t.Fatalf("a fully-live package must dispatch nothing, got %d deploys", len(f.pipelines.created))
	}
	for _, m := range pkg.Tasks {
		if !m.Released {
			t.Fatalf("want every member marked released, got %+v", m)
		}
	}
}

// Advancement is called on every read, so it must be idempotent: a member whose
// deploy is already in flight is not dispatched a second time.
func TestAdvancePackageDoesNotRedispatchAnInFlightMember(t *testing.T) {
	f := newPackageFixture(t, "DE-1")

	if _, err := f.svc.ReleasePackage(context.Background(), f.repoID, f.packages.pkg.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if len(f.pipelines.created) != 1 {
		t.Fatalf("want one dispatch from the release, got %d", len(f.pipelines.created))
	}
	for i := 0; i < 3; i++ {
		if _, err := f.svc.AdvancePackage(context.Background(), f.repoID, f.packages.pkg); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
	}
	if len(f.pipelines.created) != 1 {
		t.Fatalf("repeated advances must not re-dispatch, got %d deploys", len(f.pipelines.created))
	}
}

// A refused member fails the whole train, and the note has to name which member
// and why — a failed package with no reason is indistinguishable from a stuck
// one.
func TestAdvancePackageFailsWithTheOffendingTaskNamed(t *testing.T) {
	f := newPackageFixture(t, "DE-1")
	// Withdraw the sign-off: releaseTargetGate now refuses this task.
	id := f.taskID(t, "DE-1")
	task := f.tasks.tasks[id]
	task.VerifiedSHA = ""
	f.tasks.tasks[id] = task

	pkg, err := f.svc.ReleasePackage(context.Background(), f.repoID, f.packages.pkg.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusFailed {
		t.Fatalf("want the package failed, got %q", pkg.Status)
	}
	if !strings.Contains(pkg.Note, "DE-1") {
		t.Fatalf("want the failing task named in the note, got %q", pkg.Note)
	}
	if len(f.pipelines.created) != 0 {
		t.Fatalf("a refused member must not deploy, got %d deploys", len(f.pipelines.created))
	}
}

// A draft package is enriched but never advanced: nobody started it.
func TestAdvancePackageLeavesADraftAlone(t *testing.T) {
	f := newPackageFixture(t, "DE-1")

	pkg, err := f.svc.AdvancePackage(context.Background(), f.repoID, f.packages.pkg)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusDraft {
		t.Fatalf("want the draft untouched, got %q", pkg.Status)
	}
	if len(f.pipelines.created) != 0 {
		t.Fatalf("a draft must dispatch nothing, got %d deploys", len(f.pipelines.created))
	}
	if len(pkg.Tasks) != 1 {
		t.Fatalf("want the draft's membership returned, got %+v", pkg.Tasks)
	}
}

// A cycle discovered at release time fails the package before anything is
// dispatched — half a train shipped with no way to finish it is worse than a
// train that never left.
func TestReleasePackageFailsOnCycleWithoutDispatching(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-2", "DE-1")

	pkg, err := f.svc.ReleasePackage(context.Background(), f.repoID, f.packages.pkg.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusFailed {
		t.Fatalf("want failed, got %q", pkg.Status)
	}
	if len(f.pipelines.created) != 0 {
		t.Fatalf("a cyclic package must dispatch nothing, got %d deploys", len(f.pipelines.created))
	}
}

// A package is the release path FOR batched-release repositories, so the flag
// that disables per-task auto release must not disable it. The fixture's repo
// has AutoReleaseOnDone=false throughout; this pins the behaviour explicitly.
func TestPackageReleaseIgnoresAutoReleaseOnDone(t *testing.T) {
	f := newPackageFixture(t, "DE-1")

	// The ordinary path is still refused for this repository…
	if _, err := f.svc.TriggerRelease(context.Background(), f.repoID, f.taskID(t, "DE-1")); !errors.Is(err, ErrReleaseDisabled) {
		t.Fatalf("want ErrReleaseDisabled on the per-task path, got %v", err)
	}
	// …while the package path releases it.
	pkg, err := f.svc.ReleasePackage(context.Background(), f.repoID, f.packages.pkg.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkg.Status != domain.DeployPackageStatusReleasing {
		t.Fatalf("want releasing, got %q (note: %s)", pkg.Status, pkg.Note)
	}
	if len(f.pipelines.created) != 1 {
		t.Fatalf("want the member dispatched, got %d deploys", len(f.pipelines.created))
	}
}

// The dependency gate is what makes deploy_depends_on more than documentation.
func TestDeployDependencyGateBlocksUntilTheDependencyIsLive(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	f.dependsOn(t, "DE-1", "DE-2")

	task := f.tasks.tasks[f.taskID(t, "DE-1")]
	err := f.svc.deployDependencyGate(context.Background(), f.repoID, task)
	if !errors.Is(err, domain.ErrDeployDependencyNotReleased) {
		t.Fatalf("want ErrDeployDependencyNotReleased, got %v", err)
	}
	if len(f.comments.comments) != 1 {
		t.Fatalf("want the block explained on the task, got %d comments", len(f.comments.comments))
	}

	f.markLive(t, "DE-2")
	if err := f.svc.deployDependencyGate(context.Background(), f.repoID, task); err != nil {
		t.Fatalf("a satisfied dependency must not block, got %v", err)
	}
}

// An unreadable dependency list fails closed. "Cannot check" is the case the
// gate exists for; treating it as "checked and fine" is how an out-of-order
// deploy reaches production during a database hiccup.
func TestDeployDependencyGateFailsClosedOnUnreadableRelations(t *testing.T) {
	f := newPackageFixture(t, "DE-1")
	f.relations.listErr = errors.New("boom: the database is unreachable")

	task := f.tasks.tasks[f.taskID(t, "DE-1")]
	err := f.svc.deployDependencyGate(context.Background(), f.repoID, task)
	if !errors.Is(err, domain.ErrDeployDependencyNotReleased) {
		t.Fatalf("want the gate to fail closed, got %v", err)
	}
}

// Editing deploy order must not touch the task's other relations — a full
// replace would silently drop its blocks relations.
func TestReplaceDeployDependenciesTouchesOnlyItsOwnType(t *testing.T) {
	f := newPackageFixture(t, "DE-1", "DE-2")
	source, target := f.taskID(t, "DE-1"), f.taskID(t, "DE-2")

	rels, err := f.svc.ReplaceDeployDependencies(context.Background(), source,
		[]domain.TaskRelationInput{{TargetTaskID: target}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rels) != 1 || rels[0].RelationType != domain.TaskRelationDeployDependsOn {
		t.Fatalf("want one deploy_depends_on relation, got %+v", rels)
	}
	if got := f.relations.replaced[source]; len(got) != 1 || got[0] != target {
		t.Fatalf("want the target replaced for this source only, got %v", got)
	}
}

// A task that deploy-depends on itself would deadlock its own release gate
// forever, so it is refused at the edge rather than written and discovered
// later.
func TestReplaceDeployDependenciesRejectsSelfDependency(t *testing.T) {
	f := newPackageFixture(t, "DE-1")
	id := f.taskID(t, "DE-1")

	_, err := f.svc.ReplaceDeployDependencies(context.Background(), id,
		[]domain.TaskRelationInput{{TargetTaskID: id}})
	if err == nil {
		t.Fatal("want a self-dependency to be refused")
	}
}

// The pre-deploy checklist is posted from the structured fields when the deploy
// is dispatched — that is what replaced the "write a checklist comment"
// instruction in the tool description.
func TestTriggerReleasePostsTheRunbookOnDispatch(t *testing.T) {
	f := newPackageFixture(t, "DE-1")
	id := f.taskID(t, "DE-1")
	task := f.tasks.tasks[id]
	before, rollback := "- migrasyonu kontrol et", "- önceki imaja dön"
	task.BeforeDeploy = &before
	task.RollbackPlan = &rollback
	f.tasks.tasks[id] = task

	if _, err := f.svc.triggerRelease(context.Background(), f.repoID, id, releaseOptions{FromPackage: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.comments.comments) != 1 {
		t.Fatalf("want exactly one runbook comment, got %d", len(f.comments.comments))
	}
	content := f.comments.comments[0].Content
	for _, want := range []string{"Deploy öncesi kontrol listesi:", before, "Rollback planı:", rollback} {
		if !strings.Contains(content, want) {
			t.Fatalf("want %q in the runbook comment, got:\n%s", want, content)
		}
	}
}

// A task with no runbook must not produce an empty comment on every release.
func TestTriggerReleasePostsNothingWithoutARunbook(t *testing.T) {
	f := newPackageFixture(t, "DE-1")

	if _, err := f.svc.triggerRelease(context.Background(), f.repoID, f.taskID(t, "DE-1"), releaseOptions{FromPackage: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.comments.comments) != 0 {
		t.Fatalf("want no comment for a task with no runbook, got %+v", f.comments.comments)
	}
}

func (f *fakePackageTaskStore) FindTaskByMergeCommit(context.Context, uuid.UUID, string) (domain.BoardTask, error) {
	return domain.BoardTask{}, errors.New("not found")
}

func (f *fakePackageTaskStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakePackageTaskStore) TakeBlockedResourceTask(context.Context, string, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
