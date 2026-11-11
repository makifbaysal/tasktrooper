package readiness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/findings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeRepos struct {
	repo     domain.Repository
	index    domain.WorkspaceIndex
	indexErr error
	rootErr  error
	tasks    map[uuid.UUID]domain.BoardTask
	created  []domain.CreateBoardTaskRequest
	ciBlock  []domain.TaskRelationInput
	ciTask   *domain.BoardTask
	reindex  int
	models   int
}

func (f *fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) { return f.repo, nil }
func (f *fakeRepos) IndexStatus(context.Context, uuid.UUID) (domain.WorkspaceIndex, error) {
	return f.index, f.indexErr
}
func (f *fakeRepos) Reindex(context.Context, uuid.UUID) error { f.reindex++; return nil }
func (f *fakeRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return "/work/acme", f.rootErr
}
func (f *fakeRepos) newTask(title string, col domain.TaskColumn) domain.BoardTask {
	t := domain.BoardTask{ID: uuid.New(), Key: "TT-" + title[:2], Title: title, Column: col}
	f.tasks[t.ID] = t
	return t
}
func (f *fakeRepos) CreateTask(_ context.Context, _ uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.created = append(f.created, req)
	return f.newTask(req.Title, req.Column), nil
}
func (f *fakeRepos) GetTask(_ context.Context, _, id uuid.UUID) (domain.BoardTask, error) {
	t, ok := f.tasks[id]
	if !ok {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return t, nil
}
func (f *fakeRepos) WorkflowSetupTask(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	if f.ciTask == nil {
		return domain.BoardTask{}, false, nil
	}
	return *f.ciTask, true, nil
}
func (f *fakeRepos) CreateWorkflowSetupTaskAfter(_ context.Context, _ uuid.UUID, blockedBy []domain.TaskRelationInput) (domain.BoardTask, bool, error) {
	f.ciBlock = blockedBy
	t := f.newTask("Set up GitHub Actions CI/CD workflows", domain.TaskColumnTodo)
	return t, true, nil
}

type fakeModel struct {
	model   domain.RepositoryModel
	reads   int
	scans   int
	trigger domain.ScanTrigger
}

func (m *fakeModel) RepositoryModel(context.Context, uuid.UUID) (domain.RepositoryModel, error) {
	m.reads++
	return m.model, nil
}
func (m *fakeModel) StartScan(_ context.Context, _ uuid.UUID, t domain.ScanTrigger) (domain.ProjectScan, bool, error) {
	m.scans++
	m.trigger = t
	return domain.ProjectScan{}, true, nil
}

type fakeStore struct {
	states map[string]domain.ReadinessStepState
}

func (s *fakeStore) List(context.Context, uuid.UUID) ([]domain.ReadinessStepState, error) {
	out := []domain.ReadinessStepState{}
	for _, st := range s.states {
		out = append(out, st)
	}
	return out, nil
}
func (s *fakeStore) SetSkipped(_ context.Context, _ uuid.UUID, step string, skipped bool) error {
	st := s.states[step]
	st.StepID, st.Skipped = step, skipped
	s.states[step] = st
	return nil
}
func (s *fakeStore) SetTask(_ context.Context, _ uuid.UUID, step string, id uuid.UUID) error {
	st := s.states[step]
	st.StepID, st.TaskID = step, &id
	s.states[step] = st
	return nil
}

type fakeObs struct{ gaps bool }

func (o fakeObs) Readiness(context.Context, uuid.UUID, domain.DeployEnvironment) (domain.ObservabilityReadiness, error) {
	if o.gaps {
		return domain.ObservabilityReadiness{Suggestions: []domain.ReadinessSuggestion{{Code: "no_tracing_sdk"}}}, nil
	}
	return domain.ObservabilityReadiness{}, nil
}

type fakeDesigns struct{ eff domain.EffectiveDesignSystem }

func (d fakeDesigns) Effective(context.Context, uuid.UUID) (domain.EffectiveDesignSystem, error) {
	return d.eff, nil
}

type fakeStoreApps struct{ apps []domain.MobileStoreApp }

func (a fakeStoreApps) ListByRepository(context.Context, uuid.UUID) ([]domain.MobileStoreApp, error) {
	return a.apps, nil
}

type rig struct {
	svc   *Service
	repos *fakeRepos
	model *fakeModel
	store *fakeStore
	root  []string
	obs   *fakeObs
}

func goBackend(testCmd string, confirmedDelivery bool) domain.Component {
	c := domain.Component{
		ID: uuid.New(), Path: ".", Status: domain.ComponentStatusActive,
		Name:  domain.Detected("api", domain.ConfidenceExact),
		Role:  domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceExact),
		Stack: domain.Detected(domain.ComponentStack{Languages: []domain.StackItem{{Name: "Go"}}}, domain.ConfidenceExact),
	}
	if testCmd != "" {
		c.Commands = []domain.ComponentCommand{{Purpose: domain.CommandTest, Command: domain.Detected(testCmd, domain.ConfidenceExact)}}
	}
	if confirmedDelivery {
		c.Delivery = domain.Detected(domain.ComponentDelivery{Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorGitHubActions}, domain.ConfidenceHigh)
	}
	return c
}

func newRig(comps ...domain.Component) *rig {
	r := &rig{
		repos: &fakeRepos{
			repo:  domain.Repository{ID: uuid.New(), Name: "acme", Kind: domain.RepoKindBackend, RemoteURL: "git@github.com:acme/acme.git"},
			tasks: map[uuid.UUID]domain.BoardTask{},
			index: domain.WorkspaceIndex{Status: domain.IndexStatusCompleted, FileCount: 10},
		},
		model: &fakeModel{model: domain.RepositoryModel{
			Components: comps,
			LatestScan: &domain.ProjectScan{Status: domain.ScanSucceeded},
		}},
		store: &fakeStore{states: map[string]domain.ReadinessStepState{}},
		root:  []string{"README.md", "CLAUDE.md", "main.go"},
		obs:   &fakeObs{},
	}
	r.svc = New(Deps{
		Store: r.store, Repos: r.repos, Model: r.model, Observability: r.obs,
		ListRoot: func(string) ([]string, error) { return r.root, nil },
	})
	return r
}

func (r *rig) report(t *testing.T) domain.RepositoryReadiness {
	t.Helper()
	r.svc.forget(context.Background(), r.repos.repo.ID)
	rep, err := r.svc.Get(context.Background(), r.repos.repo.ID)
	require.NoError(t, err)
	return rep
}

func statusOf(rep domain.RepositoryReadiness, id domain.ReadinessStepID) domain.ReadinessStatus {
	st, ok := findStep(rep.Steps, id)
	if !ok {
		return "absent"
	}
	return st.Status
}

func ids(rep domain.RepositoryReadiness) []domain.ReadinessStepID {
	out := []domain.ReadinessStepID{}
	for _, s := range rep.Steps {
		out = append(out, s.ID)
	}
	return out
}

func TestAFreshImportListsTheGapsInDependencyOrder(t *testing.T) {
	r := newRig(goBackend("", false))
	r.root = []string{"main.go"}
	r.repos.index = domain.WorkspaceIndex{}
	r.obs.gaps = true

	rep := r.report(t)

	require.Equal(t, []domain.ReadinessStepID{
		domain.ReadinessScan, domain.ReadinessToolchain, domain.ReadinessIndex, domain.ReadinessGuide,
		domain.ReadinessTests, domain.ReadinessCI, domain.ReadinessDelivery, domain.ReadinessObservability, domain.ReadinessFindings,
	}, ids(rep))
	require.Equal(t, domain.ReadinessDone, statusOf(rep, domain.ReadinessScan))
	require.Equal(t, domain.ReadinessUnknown, statusOf(rep, domain.ReadinessToolchain))
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessIndex))
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessGuide))
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessTests))
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessCI))
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessDelivery))
	require.Equal(t, domain.ReadinessBlocked, statusOf(rep, domain.ReadinessObservability), "observability reads from delivery")
	require.Equal(t, domain.ReadinessDone, statusOf(rep, domain.ReadinessFindings))

	tool, _ := findStep(rep.Steps, domain.ReadinessToolchain)
	require.Equal(t, []string{"go"}, tool.Facts["languages"])
	guide, _ := findStep(rep.Steps, domain.ReadinessGuide)
	require.Equal(t, []string{"readme", "agent_guide"}, guide.Facts["missing"])

	require.Equal(t, 1, rep.RunAll.Runs, "only the index pass runs by itself")
	require.Equal(t, 3, rep.RunAll.Tasks)
	require.Equal(t, []domain.ReadinessStepID{domain.ReadinessIndex, domain.ReadinessGuide, domain.ReadinessTests, domain.ReadinessCI}, rep.RunAll.Steps)
	require.False(t, rep.Complete)
}

func TestAReadyRepositoryIsComplete(t *testing.T) {
	r := newRig(goBackend("go test ./...", true))
	r.model.model.Checks = []domain.ComponentCheck{{Status: domain.ModelStatusActive}}

	rep := r.report(t)

	for _, id := range []domain.ReadinessStepID{domain.ReadinessScan, domain.ReadinessIndex, domain.ReadinessGuide, domain.ReadinessTests, domain.ReadinessCI, domain.ReadinessDelivery, domain.ReadinessObservability, domain.ReadinessFindings} {
		require.Equal(t, domain.ReadinessDone, statusOf(rep, id), id)
	}
	require.Equal(t, domain.ReadinessUnknown, statusOf(rep, domain.ReadinessToolchain))
	require.Zero(t, rep.RunAll.Tasks+rep.RunAll.Runs)
}

func TestWithoutAScanEverythingThatReadsFromItWaits(t *testing.T) {
	r := newRig()
	r.root = []string{"main.go"}
	r.model.model.LatestScan = nil

	rep := r.report(t)

	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessScan))
	require.Equal(t, domain.ReadinessBlocked, statusOf(rep, domain.ReadinessGuide))
	require.Equal(t, "needs_scan", func() string { s, _ := findStep(rep.Steps, domain.ReadinessGuide); return s.Reason }())
	require.Equal(t, []domain.ReadinessStepID{domain.ReadinessScan}, rep.RunAll.Steps[:1])
	require.Equal(t, 0, rep.RunAll.Tasks)
}

func TestARunningScanAndAnIndexPassShowAsRunning(t *testing.T) {
	r := newRig(goBackend("go test", true))
	r.model.model.LatestScan = &domain.ProjectScan{Status: domain.ScanRunning}
	r.repos.index = domain.WorkspaceIndex{Status: domain.IndexStatusRunning, FilesTotal: 10, FilesProcessed: 4}

	rep := r.report(t)

	require.Equal(t, domain.ReadinessRunning, statusOf(rep, domain.ReadinessScan))
	require.Equal(t, domain.ReadinessRunning, statusOf(rep, domain.ReadinessIndex))
}

func TestASkippedStepCountsAsHandledButADoneStepIsNotSkipped(t *testing.T) {
	r := newRig(goBackend("", true))
	r.root = []string{"main.go"}
	require.NoError(t, r.svc.SetSkipped(context.Background(), r.repos.repo.ID, "guide", true))
	require.NoError(t, r.svc.SetSkipped(context.Background(), r.repos.repo.ID, "index", true))

	rep := r.report(t)

	require.Equal(t, domain.ReadinessSkipped, statusOf(rep, domain.ReadinessGuide))
	require.Equal(t, domain.ReadinessDone, statusOf(rep, domain.ReadinessIndex))
	require.Equal(t, 1, rep.Skipped)
	require.NotContains(t, rep.RunAll.Steps, domain.ReadinessGuide)

	require.ErrorIs(t, r.svc.SetSkipped(context.Background(), r.repos.repo.ID, "nonsense", true), domain.ErrReadinessUnknownStep)
}

func TestTheCardCanBeHiddenWithoutTouchingTheSteps(t *testing.T) {
	r := newRig(goBackend("", true))
	before := r.report(t)
	require.False(t, before.Dismissed)
	require.NoError(t, r.svc.SetDismissed(context.Background(), r.repos.repo.ID, true))
	after := r.report(t)
	require.True(t, after.Dismissed)
	require.Equal(t, before.Steps, after.Steps)
	require.Equal(t, before.Total, after.Total)
}

func TestATestGapIsAMissingCommandOrAnOpenNoTestsFinding(t *testing.T) {
	withCmd := goBackend("go test ./...", true)
	r := newRig(withCmd)
	id := withCmd.ID
	r.model.model.Findings = []domain.ComponentFinding{{Rule: findings.RuleNoAutomatedTests, Status: domain.FindingOpen, ComponentID: &id}}
	require.Equal(t, domain.ReadinessTodo, statusOf(r.report(t), domain.ReadinessTests))

	r.model.model.Findings[0].Status = domain.FindingAccepted
	require.Equal(t, domain.ReadinessDone, statusOf(r.report(t), domain.ReadinessTests))
}

func TestFindingsTheTestsStepOwnsOrThatHaveATaskDoNotCount(t *testing.T) {
	c := goBackend("go test", true)
	r := newRig(c)
	taskID := uuid.New()
	r.model.model.Findings = []domain.ComponentFinding{
		{Rule: findings.RuleNoAutomatedTests, Status: domain.FindingOpen},
		{Rule: "debug_logging", Status: domain.FindingOpen, TaskID: &taskID},
	}
	require.Equal(t, domain.ReadinessDone, statusOf(r.report(t), domain.ReadinessFindings))

	r.model.model.Findings = append(r.model.model.Findings, domain.ComponentFinding{Rule: "secrets", Status: domain.FindingOpen, Severity: domain.FindingSeverityHigh})
	rep := r.report(t)
	st, _ := findStep(rep.Steps, domain.ReadinessFindings)
	require.Equal(t, domain.ReadinessTodo, st.Status)
	require.Equal(t, 1, st.Facts["count"])
	require.Equal(t, 1, st.Facts["high"])
}

func TestObservabilityGapsShowOnceDeliveryIsSettled(t *testing.T) {
	r := newRig(goBackend("go test", true))
	r.obs.gaps = true
	rep := r.report(t)
	st, _ := findStep(rep.Steps, domain.ReadinessObservability)
	require.Equal(t, domain.ReadinessTodo, st.Status)
	require.Equal(t, domain.ReadinessActionNavigate, st.Action)
	require.NotEmpty(t, st.Facts["component_id"])
}

func TestOptionalSignalsAddStepsOnlyForTheComponentsThatNeedThem(t *testing.T) {
	ui := goBackend("npm test", true)
	ui.Role = domain.Detected(domain.ComponentRoleFrontend, domain.ConfidenceExact)
	mobile := goBackend("", false)
	mobile.ID = uuid.New()
	mobile.Path = "ios"
	mobile.Role = domain.Detected(domain.ComponentRoleMobile, domain.ConfidenceExact)
	r := newRig(ui, mobile)
	r.svc.Designs = fakeDesigns{}
	r.svc.StoreApps = fakeStoreApps{}

	rep := r.report(t)
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessDesign))
	require.Equal(t, domain.ReadinessTodo, statusOf(rep, domain.ReadinessStore))

	r.svc.StoreApps = fakeStoreApps{apps: []domain.MobileStoreApp{{StoreAppID: "123"}}}
	r.svc.Designs = fakeDesigns{eff: domain.EffectiveDesignSystem{Base: &domain.DesignSystem{}}}
	rep = r.report(t)
	require.Equal(t, domain.ReadinessDone, statusOf(rep, domain.ReadinessDesign))
	require.Equal(t, domain.ReadinessDone, statusOf(rep, domain.ReadinessStore))

	only := newRig(goBackend("go test", true))
	only.svc.Designs = fakeDesigns{}
	only.svc.StoreApps = fakeStoreApps{}
	require.Equal(t, "absent", string(statusOf(only.report(t), domain.ReadinessDesign)))
	require.Equal(t, "absent", string(statusOf(only.report(t), domain.ReadinessStore)))
}

func TestNoRemoteMeansNoCIStepAndAnUnreadableFolderIsUnknown(t *testing.T) {
	r := newRig(goBackend("go test", true))
	r.repos.repo.RemoteURL = ""
	r.svc.ListRoot = func(string) ([]string, error) { return nil, errors.New("gone") }
	rep := r.report(t)
	require.Equal(t, "absent", string(statusOf(rep, domain.ReadinessCI)))
	require.Equal(t, domain.ReadinessUnknown, statusOf(rep, domain.ReadinessGuide))
}

func TestStartingATaskStepNeedsTheApprovalAndOpensNothingWithout(t *testing.T) {
	r := newRig(goBackend("", true))
	ctx := context.Background()

	_, err := r.svc.Run(ctx, r.repos.repo.ID, "tests", false)
	require.ErrorIs(t, err, domain.ErrReadinessNotApproved)
	require.Empty(t, r.repos.created)

	res, err := r.svc.Run(ctx, r.repos.repo.ID, "tests", true)
	require.NoError(t, err)
	require.NotNil(t, res.Task)
	require.Len(t, r.repos.created, 1)
	req := r.repos.created[0]
	require.Equal(t, domain.TaskColumnTodo, req.Column)
	require.Equal(t, "system", req.CreatedBy)
	require.Empty(t, req.BlockedBy, "one click is not chained")
	require.Contains(t, req.Description, "## Done when")
	require.NotNil(t, req.ComponentID, "one gap component is the task's component")

	rep := r.report(t)
	st, _ := findStep(rep.Steps, domain.ReadinessTests)
	require.Equal(t, domain.ReadinessRunning, st.Status)
	require.Equal(t, res.Task.ID, st.Task.ID)
	require.NotContains(t, rep.RunAll.Steps, domain.ReadinessTests, "an open task is not opened twice")

	_, err = r.svc.Run(ctx, r.repos.repo.ID, "tests", true)
	require.ErrorIs(t, err, domain.ErrReadinessNotRunnable)
}

func TestAFinishedTaskThatDidNotFixTheGapLetsTheStepRunAgain(t *testing.T) {
	r := newRig(goBackend("", true))
	res, err := r.svc.Run(context.Background(), r.repos.repo.ID, "tests", true)
	require.NoError(t, err)
	task := r.repos.tasks[res.Task.ID]
	task.Column = domain.TaskColumnDone
	r.repos.tasks[task.ID] = task

	rep := r.report(t)
	st, _ := findStep(rep.Steps, domain.ReadinessTests)
	require.Equal(t, domain.ReadinessTodo, st.Status)
	require.Nil(t, st.Task)
}

func TestOnlyScansIndexesAndTaskStepsStartFromHere(t *testing.T) {
	r := newRig(goBackend("", false))
	ctx := context.Background()
	for _, id := range []string{"delivery", "toolchain", "findings"} {
		_, err := r.svc.Run(ctx, r.repos.repo.ID, id, true)
		require.Error(t, err, id)
	}
	_, err := r.svc.Run(ctx, r.repos.repo.ID, "observability", true)
	require.ErrorIs(t, err, domain.ErrReadinessNotRunnable, "blocked")
	_, err = r.svc.Run(ctx, r.repos.repo.ID, "nope", true)
	require.ErrorIs(t, err, domain.ErrReadinessUnknownStep)
	require.Empty(t, r.repos.created)
}

func TestARunStepStartsWorkWithoutATask(t *testing.T) {
	r := newRig(goBackend("go test", true))
	r.repos.index = domain.WorkspaceIndex{}
	res, err := r.svc.Run(context.Background(), r.repos.repo.ID, "index", false)
	require.NoError(t, err)
	require.True(t, res.Started)
	require.Equal(t, 1, r.repos.reindex)

	r.model.model.LatestScan = &domain.ProjectScan{Status: domain.ScanFailed}
	r.svc.forget(context.Background(), r.repos.repo.ID)
	_, err = r.svc.Run(context.Background(), r.repos.repo.ID, "scan", false)
	require.NoError(t, err)
	require.Equal(t, 1, r.model.scans)
	require.Equal(t, domain.ScanTriggerManual, r.model.trigger)
}

func TestRunAllNeedsTheExactCountTheyWereShown(t *testing.T) {
	r := newRig(goBackend("", false))
	r.root = []string{"main.go"}
	ctx := context.Background()
	three, two := 3, 2

	_, err := r.svc.RunAll(ctx, r.repos.repo.ID, nil)
	require.ErrorIs(t, err, domain.ErrReadinessNotApproved)
	_, err = r.svc.RunAll(ctx, r.repos.repo.ID, &two)
	require.ErrorIs(t, err, domain.ErrReadinessPlanChanged)
	require.Empty(t, r.repos.created)
	require.Zero(t, r.repos.reindex)

	res, err := r.svc.RunAll(ctx, r.repos.repo.ID, &three)
	require.NoError(t, err)
	require.Len(t, res.Results, 3)
}

func TestRunAllQueuesTasksOneBehindTheOtherSoTheBoardRunsThemInSequence(t *testing.T) {
	r := newRig(goBackend("", false))
	r.root = []string{"main.go"}
	r.repos.index = domain.WorkspaceIndex{}
	three := 3

	res, err := r.svc.RunAll(context.Background(), r.repos.repo.ID, &three)
	require.NoError(t, err)

	require.Equal(t, 1, r.repos.reindex)
	require.Len(t, r.repos.created, 2)
	guide, tests := r.repos.created[0], r.repos.created[1]
	require.Empty(t, guide.BlockedBy, "the first task starts at once")
	guideTask := res.Results[1].Task
	require.Equal(t, domain.ReadinessGuide, res.Results[1].Step)
	require.Len(t, tests.BlockedBy, 1)
	require.Equal(t, guideTask.ID, tests.BlockedBy[0].TargetTaskID)
	testsTask := res.Results[2].Task
	require.Equal(t, domain.ReadinessTests, res.Results[2].Step)
	require.Len(t, r.repos.ciBlock, 1)
	require.Equal(t, testsTask.ID, r.repos.ciBlock[0].TargetTaskID)

	rep := r.report(t)
	require.Equal(t, domain.ReadinessRunning, statusOf(rep, domain.ReadinessGuide))
	require.Equal(t, domain.ReadinessRunning, statusOf(rep, domain.ReadinessCI))
}

func TestRunAllBehindATaskAlreadyOpenWaitsForIt(t *testing.T) {
	r := newRig(goBackend("", true))
	r.root = []string{"main.go"}
	ctx := context.Background()
	first, err := r.svc.Run(ctx, r.repos.repo.ID, "guide", true)
	require.NoError(t, err)
	two := 2
	r.repos.created = nil

	res, err := r.svc.RunAll(ctx, r.repos.repo.ID, &two)
	require.NoError(t, err)

	require.Len(t, r.repos.created, 1)
	require.Equal(t, first.Task.ID, r.repos.created[0].BlockedBy[0].TargetTaskID)
	require.Equal(t, res.Results[0].Task.ID, r.repos.ciBlock[0].TargetTaskID)
}

func TestARunWithNothingToOpenNeedsNoApproval(t *testing.T) {
	r := newRig(goBackend("go test", true))
	r.model.model.Checks = []domain.ComponentCheck{{Status: domain.ModelStatusActive}}
	r.repos.index = domain.WorkspaceIndex{}
	res, err := r.svc.RunAll(context.Background(), r.repos.repo.ID, nil)
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.Equal(t, 1, r.repos.reindex)
}

func TestTheReportIsCachedBrieflyAndAChoiceClearsIt(t *testing.T) {
	r := newRig(goBackend("go test", true))
	now := time.Now()
	r.svc.now = func() time.Time { return now }
	ctx := context.Background()

	_, err := r.svc.Get(ctx, r.repos.repo.ID)
	require.NoError(t, err)
	_, err = r.svc.Get(ctx, r.repos.repo.ID)
	require.NoError(t, err)
	require.Equal(t, 1, r.model.reads)

	require.NoError(t, r.svc.SetSkipped(ctx, r.repos.repo.ID, "guide", true))
	_, err = r.svc.Get(ctx, r.repos.repo.ID)
	require.NoError(t, err)
	require.Equal(t, 2, r.model.reads)

	now = now.Add(cacheTTL + time.Second)
	_, err = r.svc.Get(ctx, r.repos.repo.ID)
	require.NoError(t, err)
	require.Equal(t, 3, r.model.reads)
}

func TestTheBriefsNameWhatIsMissingAndWhatToProve(t *testing.T) {
	both := guideKey.Render(guideInput{Repository: "acme", Missing: []string{"readme", "agent_guide"},
		Components: []componentBrief{{Path: ".", Role: "backend", Stack: "Go", Commands: []string{"test: go test ./..."}}}})
	require.Contains(t, both, "both a README and a guide for coding agents")
	require.Contains(t, both, "## README.md")
	require.Contains(t, both, "## AGENTS.md")
	require.NotContains(t, both, "CLAUDE.md` exist")
	require.Contains(t, both, "test: go test ./...")
	require.Contains(t, both, "## Done when")

	only := guideKey.Render(guideInput{Repository: "acme", Missing: []string{"agent_guide"}})
	require.False(t, strings.Contains(only, "## README.md"))
	require.Contains(t, only, "## AGENTS.md")
	require.Contains(t, only, "link to the existing README")
	require.Contains(t, only, "Nothing other than `AGENTS.md` changed")

	tests := testsKey.Render(testsInput{Repository: "acme", Components: []testGap{{Path: "web", Role: "frontend"}, {Path: "api", Role: "backend", TestCommand: "go test ./..."}}})
	require.Contains(t, tests, "`web`")
	require.Contains(t, tests, "No test command is on record")
	require.Contains(t, tests, "Test command on record: `go test ./...`")
}

func TestTheGuideTaskIsNamedForWhatIsMissing(t *testing.T) {
	require.Equal(t, "Add a README and an AGENTS.md agent guide", guideTaskTitle([]string{"readme", "agent_guide"}))
	require.Equal(t, "Add a README", guideTaskTitle([]string{"readme"}))
	require.Equal(t, "Add an AGENTS.md agent guide", guideTaskTitle([]string{"agent_guide"}))
}
