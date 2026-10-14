package projectmodel

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/findings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeFindingStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.ComponentFinding
}

var _ port.FindingStore = (*fakeFindingStore)(nil)

func newFakeFindingStore() *fakeFindingStore {
	return &fakeFindingStore{rows: map[uuid.UUID]domain.ComponentFinding{}}
}

func (f *fakeFindingStore) ListFindings(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentFinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ComponentFinding
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rule < out[j].Rule })
	return out, nil
}

func (f *fakeFindingStore) GetFinding(_ context.Context, id uuid.UUID) (domain.ComponentFinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.ComponentFinding{}, port.ErrNotFound
	}
	return r, nil
}

func (f *fakeFindingStore) SaveFinding(_ context.Context, r domain.ComponentFinding) (domain.ComponentFinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[r.ID] = r
	return r, nil
}

func (f *fakeFindingStore) ApplyFindings(_ context.Context, repositoryID uuid.UUID, save []domain.ComponentFinding, del []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range save {
		r.RepositoryID = repositoryID
		f.rows[r.ID] = r
	}
	for _, id := range del {
		delete(f.rows, id)
	}
	return nil
}

func (f *fakeFindingStore) LinkFindingsToTask(_ context.Context, ids []uuid.UUID, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		r := f.rows[id]
		r.TaskID = &taskID
		f.rows[id] = r
	}
	return nil
}

type fakeTaskCreator struct {
	got []domain.CreateBoardTaskRequest
}

func (f *fakeTaskCreator) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.got = append(f.got, req)
	return domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Title: req.Title, TaskType: req.TaskType}, nil
}

func detected(path, rule string, sev domain.FindingSeverity, n int) domain.DetectedFinding {
	r, _ := findings.Lookup(rule)
	return domain.DetectedFinding{ComponentPath: path, Rule: rule, Category: r.Category, Severity: sev, Occurrences: n,
		Evidence: []domain.SourceEvidence{{Path: path + "/main.go", Line: 3, Note: "fmt.Println(x)"}}}
}

func TestPlanFindings(t *testing.T) {
	repoID, scanID := uuid.New(), uuid.New()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	api := domain.Component{ID: uuid.New(), Path: "api", Status: domain.ComponentStatusActive}
	web := domain.Component{ID: uuid.New(), Path: "web", Status: domain.ComponentStatusDismissed}
	stored := func(c *domain.Component, rule string, status domain.FindingStatus) domain.ComponentFinding {
		var id *uuid.UUID
		if c != nil {
			id = &c.ID
		}
		return domain.ComponentFinding{ID: uuid.New(), RepositoryID: repoID, ComponentID: id, Rule: rule, Status: status, Occurrences: 1}
	}

	accepted := stored(&api, findings.RuleConsoleLogsInProduction, domain.FindingAccepted)
	accepted.StatusReason = "we ship a debug build to QA"
	falsePositive := stored(&api, findings.RuleHardcodedSecret, domain.FindingFalsePositive)
	open := stored(&api, findings.RulePrintInsteadOfLogger, domain.FindingOpen)
	taskID := uuid.New()
	open.TaskID = &taskID
	gone := stored(&api, findings.RuleEmptyCatchBlock, domain.FindingOpen)
	resolvedBack := stored(&api, findings.RuleBareExcept, domain.FindingResolved)
	resolvedBack.ResolvedAt = ptr(now.Add(-time.Hour))
	resolvedOld := stored(&api, findings.RuleWildcardImport, domain.FindingResolved)
	resolvedOld.ResolvedAt = ptr(now.Add(-40 * 24 * time.Hour))

	save, del := planFindings(findingPlanInput{
		repositoryID: repoID, scanID: scanID, now: now,
		components: []domain.Component{api, web},
		existing:   []domain.ComponentFinding{accepted, falsePositive, open, gone, resolvedBack, resolvedOld},
		detected: []domain.DetectedFinding{
			detected("api", findings.RuleConsoleLogsInProduction, domain.FindingSeverityMedium, 40),
			detected("api", findings.RuleHardcodedSecret, domain.FindingSeverityHigh, 1),
			detected("api", findings.RulePrintInsteadOfLogger, domain.FindingSeverityLow, 2),
			detected("api", findings.RuleBareExcept, domain.FindingSeverityMedium, 1),
			detected("api", findings.RuleNoAutomatedTests, domain.FindingSeverityMedium, 1),
			detected("web", findings.RuleNoAutomatedTests, domain.FindingSeverityMedium, 1),
			detected("", findings.RuleCommittedEnvFile, domain.FindingSeverityHigh, 1),
		},
	})

	byRule := map[string]domain.ComponentFinding{}
	for _, f := range save {
		key := f.Rule
		if f.ComponentID == nil {
			key = "repo:" + key
		}
		byRule[key] = f
	}
	require.Equal(t, []uuid.UUID{resolvedOld.ID}, del, "an old resolved finding with no task is forgotten")

	got := byRule[findings.RuleConsoleLogsInProduction]
	require.Equal(t, accepted.ID, got.ID)
	require.Equal(t, domain.FindingAccepted, got.Status, "an accepted finding never reopens")
	require.Equal(t, "we ship a debug build to QA", got.StatusReason)
	require.Equal(t, 40, got.Occurrences)
	require.Equal(t, domain.FindingFalsePositive, byRule[findings.RuleHardcodedSecret].Status)

	require.Equal(t, &taskID, byRule[findings.RulePrintInsteadOfLogger].TaskID)
	require.Equal(t, scanID, *byRule[findings.RulePrintInsteadOfLogger].LastScanID)

	require.Equal(t, domain.FindingResolved, byRule[findings.RuleEmptyCatchBlock].Status)
	require.Equal(t, now, *byRule[findings.RuleEmptyCatchBlock].ResolvedAt)

	require.Equal(t, domain.FindingOpen, byRule[findings.RuleBareExcept].Status, "a problem that comes back reopens")
	require.Nil(t, byRule[findings.RuleBareExcept].ResolvedAt)

	tests := byRule[findings.RuleNoAutomatedTests]
	require.Equal(t, api.ID, *tests.ComponentID, "a dismissed component gets no findings")
	require.Equal(t, domain.FindingOpen, tests.Status)
	require.Equal(t, domain.FindingOpen, byRule["repo:"+findings.RuleCommittedEnvFile].Status)
	require.Len(t, save, 7)
}

func TestPlanFindingsPartialScanResolvesNothing(t *testing.T) {
	api := domain.Component{ID: uuid.New(), Path: "api", Status: domain.ComponentStatusActive}
	open := domain.ComponentFinding{ID: uuid.New(), ComponentID: &api.ID, Rule: findings.RuleEmptyCatchBlock, Status: domain.FindingOpen}
	save, del := planFindings(findingPlanInput{
		repositoryID: uuid.New(), scanID: uuid.New(), now: time.Now(), partial: true,
		components: []domain.Component{api}, existing: []domain.ComponentFinding{open},
	})
	require.Empty(t, save)
	require.Empty(t, del)
}

func TestEveryRuleRendersIntoATask(t *testing.T) {
	for _, r := range findings.Rules() {
		out := findingTaskKey.Render(findingTaskInput{
			Repository: "acme",
			Findings: []findingTaskItem{{
				Rule: r.ID, Category: string(r.Category), Severity: string(r.Severity), Component: "api (api)",
				Occurrences: 3, Locations: []string{"api/main.go:3"}, More: 2,
			}},
		})
		require.Contains(t, out, "### ", r.ID)
		require.Contains(t, out, "How to fix:", r.ID)
		require.Contains(t, out, "api/main.go:3", r.ID)
	}
}

type findingFixture struct {
	svc      *Service
	store    *fakeStore
	findings *fakeFindingStore
	tasks    *fakeTaskCreator
	repo     domain.Repository
	api      domain.Component
	web      domain.Component
}

func newFindingFixture() *findingFixture {
	fx := &findingFixture{store: newFakeStore(), findings: newFakeFindingStore(), tasks: &fakeTaskCreator{}}
	fx.repo = domain.Repository{ID: uuid.New(), Name: "widgets", RootPath: "/repos/widgets"}
	fx.api = fx.store.seedComponent(domain.Component{RepositoryID: fx.repo.ID, Path: "api", Name: domain.Detected("api", domain.ConfidenceHigh), Status: domain.ComponentStatusActive})
	fx.web = fx.store.seedComponent(domain.Component{RepositoryID: fx.repo.ID, Path: "web", Status: domain.ComponentStatusActive})
	fx.svc = NewService(Deps{Store: fx.store, Repos: newFakeRepos(fx.repo)})
	fx.svc.SetFindingStore(fx.findings)
	fx.svc.SetTaskCreator(fx.tasks)
	return fx
}

func (fx *findingFixture) seed(c domain.Component, rule string, sev domain.FindingSeverity, status domain.FindingStatus) domain.ComponentFinding {
	id := c.ID
	f := domain.ComponentFinding{
		ID: uuid.New(), RepositoryID: fx.repo.ID, ComponentID: &id, Rule: rule, Severity: sev, Status: status, Occurrences: 12,
		Category: domain.FindingLogging,
		Evidence: []domain.SourceEvidence{{Path: c.Path + "/src/app.ts", Line: 14, Note: "console.log(user)"}},
	}
	fx.findings.rows[f.ID] = f
	return f
}

func TestCreateFindingTask(t *testing.T) {
	ctx := context.Background()
	fx := newFindingFixture()
	low := fx.seed(fx.api, findings.RulePrintInsteadOfLogger, domain.FindingSeverityLow, domain.FindingOpen)
	high := fx.seed(fx.api, findings.RuleHardcodedSecret, domain.FindingSeverityHigh, domain.FindingOpen)

	res, err := fx.svc.CreateFindingTask(ctx, fx.repo.ID, domain.FindingTaskRequest{
		FindingIDs: []uuid.UUID{low.ID, high.ID, low.ID}, Title: "Fix api findings", Notes: "Rotate with ops first.",
	})
	require.NoError(t, err)
	require.Len(t, fx.tasks.got, 1)
	req := fx.tasks.got[0]
	require.Equal(t, "Fix api findings", req.Title)
	require.Equal(t, domain.TaskTypeTechnical, req.TaskType)
	require.Equal(t, domain.TaskPriorityHigh, req.Priority, "the worst finding sets the priority")
	require.Equal(t, fx.api.ID, *req.ComponentID)
	require.True(t, strings.HasPrefix(req.Description, "Rotate with ops first."))
	require.Less(t, strings.Index(req.Description, "A secret is committed"), strings.Index(req.Description, "Service code prints"), "high severity first")
	require.Contains(t, req.Description, "api/src/app.ts:14 — console.log(user)")
	require.Contains(t, req.Description, "and 11 more")
	require.Len(t, res.Findings, 2)
	for _, f := range res.Findings {
		require.Equal(t, res.Task.ID, *f.TaskID)
	}

	webFinding := fx.seed(fx.web, findings.RuleNoAutomatedTests, domain.FindingSeverityMedium, domain.FindingOpen)
	apiFinding := fx.seed(fx.api, findings.RuleNoAutomatedTests, domain.FindingSeverityMedium, domain.FindingOpen)
	_, err = fx.svc.CreateFindingTask(ctx, fx.repo.ID, domain.FindingTaskRequest{FindingIDs: []uuid.UUID{webFinding.ID, apiFinding.ID}, Title: "Add tests"})
	require.NoError(t, err)
	require.Nil(t, fx.tasks.got[1].ComponentID, "findings across components make a repository task")
}

func TestCreateFindingTaskRefuses(t *testing.T) {
	ctx := context.Background()
	fx := newFindingFixture()
	accepted := fx.seed(fx.api, findings.RuleTechDebtMarkers, domain.FindingSeverityLow, domain.FindingAccepted)
	open := fx.seed(fx.api, findings.RuleEmptyCatchBlock, domain.FindingSeverityMedium, domain.FindingOpen)

	cases := map[string]domain.FindingTaskRequest{
		"no findings":    {Title: "x"},
		"no title":       {FindingIDs: []uuid.UUID{open.ID}, Title: "  "},
		"already closed": {FindingIDs: []uuid.UUID{accepted.ID}, Title: "x"},
		"unknown":        {FindingIDs: []uuid.UUID{uuid.New()}, Title: "x"},
	}
	for name, req := range cases {
		_, err := fx.svc.CreateFindingTask(ctx, fx.repo.ID, req)
		require.ErrorIs(t, err, ErrInvalidInput, name)
	}
	require.Empty(t, fx.tasks.got)
}

func TestUpdateFinding(t *testing.T) {
	ctx := context.Background()
	fx := newFindingFixture()
	f := fx.seed(fx.api, findings.RuleConsoleLogsInProduction, domain.FindingSeverityMedium, domain.FindingOpen)

	got, err := fx.svc.UpdateFinding(ctx, f.ID, domain.FindingPatch{Status: ptr(domain.FindingFalsePositive), Reason: ptr("  logger shim, not console  ")})
	require.NoError(t, err)
	require.Equal(t, domain.FindingFalsePositive, got.Status)
	require.Equal(t, "logger shim, not console", got.StatusReason)

	got, err = fx.svc.UpdateFinding(ctx, f.ID, domain.FindingPatch{Status: ptr(domain.FindingOpen)})
	require.NoError(t, err)
	require.Equal(t, domain.FindingOpen, got.Status)
	require.Empty(t, got.StatusReason)

	_, err = fx.svc.UpdateFinding(ctx, f.ID, domain.FindingPatch{Status: ptr(domain.FindingResolved)})
	require.ErrorIs(t, err, ErrInvalidInput, "resolved is the scan's to set")

	resolved := fx.seed(fx.api, findings.RuleBareExcept, domain.FindingSeverityMedium, domain.FindingResolved)
	_, err = fx.svc.UpdateFinding(ctx, resolved.ID, domain.FindingPatch{Status: ptr(domain.FindingAccepted)})
	require.ErrorIs(t, err, ErrInvalidInput)

	_, err = fx.svc.UpdateFinding(ctx, uuid.New(), domain.FindingPatch{Status: ptr(domain.FindingAccepted)})
	require.ErrorIs(t, err, port.ErrNotFound)
}

func TestReconcileFindingsSkipsAnOlderScanner(t *testing.T) {
	ctx := context.Background()
	fx := newFindingFixture()
	open := fx.seed(fx.api, findings.RuleEmptyCatchBlock, domain.FindingSeverityMedium, domain.FindingOpen)

	require.NoError(t, fx.svc.reconcileFindings(ctx, fx.repo.ID, uuid.New(), domain.ScanResult{}))
	got, _ := fx.findings.GetFinding(ctx, open.ID)
	require.Equal(t, domain.FindingOpen, got.Status, "a result with no findings section says nothing about them")

	require.NoError(t, fx.svc.reconcileFindings(ctx, fx.repo.ID, uuid.New(), domain.ScanResult{FindingsScanned: true}))
	got, _ = fx.findings.GetFinding(ctx, open.ID)
	require.Equal(t, domain.FindingResolved, got.Status)
}
