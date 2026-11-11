// Package readiness builds a repository's "get this project ready" checklist
// from signals the product already has (the scan, the code index, the
// project's checks and delivery profile, observability readiness, code-health
// findings, the design system, the store apps) and starts a step on the
// person's click. Every status is derived from live data when asked; only a
// person's skip choice and the board task a step opened are stored.
package readiness

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const cacheTTL = 3 * time.Second

type Repositories interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	IndexStatus(ctx context.Context, repositoryID uuid.UUID) (domain.WorkspaceIndex, error)
	Reindex(ctx context.Context, repositoryID uuid.UUID) error
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	WorkflowSetupTask(ctx context.Context, repositoryID uuid.UUID) (domain.BoardTask, bool, error)
	CreateWorkflowSetupTaskAfter(ctx context.Context, repositoryID uuid.UUID, blockedBy []domain.TaskRelationInput) (domain.BoardTask, bool, error)
}

type Model interface {
	RepositoryModel(ctx context.Context, repositoryID uuid.UUID) (domain.RepositoryModel, error)
	StartScan(ctx context.Context, repositoryID uuid.UUID, trigger domain.ScanTrigger) (domain.ProjectScan, bool, error)
}

type Observability interface {
	Readiness(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ObservabilityReadiness, error)
}

type DesignSystems interface {
	Effective(ctx context.Context, repositoryID uuid.UUID) (domain.EffectiveDesignSystem, error)
}

type StoreApps interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error)
}

type Deps struct {
	Store         port.ReadinessStore
	Repos         Repositories
	Model         Model
	Roles         port.RoleResolver
	Observability Observability
	Designs       DesignSystems
	StoreApps     StoreApps
	// ListRoot names the entries of a working copy's top directory; defaults
	// to the filesystem. A failure leaves the guide step "unknown".
	ListRoot func(root string) ([]string, error)
	// CacheKey scopes the short cache; defaults to the repository id.
	CacheKey func(ctx context.Context, repositoryID uuid.UUID) string
}

type Service struct {
	Deps
	mu    sync.Mutex
	cache map[string]cached
	now   func() time.Time
}

type cached struct {
	at     time.Time
	report domain.RepositoryReadiness
}

func New(d Deps) *Service {
	if d.ListRoot == nil {
		d.ListRoot = listDir
	}
	if d.CacheKey == nil {
		d.CacheKey = func(_ context.Context, id uuid.UUID) string { return id.String() }
	}
	return &Service{Deps: d, cache: map[string]cached{}, now: time.Now}
}

func listDir(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// Get is GET /v1/repositories/:id/readiness.
func (s *Service) Get(ctx context.Context, repositoryID uuid.UUID) (domain.RepositoryReadiness, error) {
	key := s.CacheKey(ctx, repositoryID)
	s.mu.Lock()
	if c, ok := s.cache[key]; ok && s.now().Sub(c.at) < cacheTTL {
		s.mu.Unlock()
		return c.report, nil
	}
	s.mu.Unlock()

	report, _, err := s.compute(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryReadiness{}, err
	}
	s.mu.Lock()
	s.cache[key] = cached{at: s.now(), report: report}
	s.mu.Unlock()
	return report, nil
}

func (s *Service) forget(ctx context.Context, repositoryID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, s.CacheKey(ctx, repositoryID))
	s.mu.Unlock()
}

// SetSkipped records the person's choice to leave a step out.
func (s *Service) SetSkipped(ctx context.Context, repositoryID uuid.UUID, stepID string, skipped bool) error {
	if !domain.ValidReadinessStepID(stepID) {
		return domain.ErrReadinessUnknownStep
	}
	if _, err := s.Repos.Get(ctx, repositoryID); err != nil {
		return err
	}
	defer s.forget(ctx, repositoryID)
	return s.Store.SetSkipped(ctx, repositoryID, stepID, skipped)
}

// SetDismissed hides (or shows again) the card on the repository page.
func (s *Service) SetDismissed(ctx context.Context, repositoryID uuid.UUID, dismissed bool) error {
	if _, err := s.Repos.Get(ctx, repositoryID); err != nil {
		return err
	}
	defer s.forget(ctx, repositoryID)
	return s.Store.SetSkipped(ctx, repositoryID, domain.ReadinessCardKey, dismissed)
}

// Run starts one step. A step that opens a board task spends an agent's time,
// so it only runs when the request says the person approved it.
func (s *Service) Run(ctx context.Context, repositoryID uuid.UUID, stepID string, approved bool) (domain.ReadinessRunResult, error) {
	if !domain.ValidReadinessStepID(stepID) {
		return domain.ReadinessRunResult{}, domain.ErrReadinessUnknownStep
	}
	report, facts, err := s.compute(ctx, repositoryID)
	if err != nil {
		return domain.ReadinessRunResult{}, err
	}
	step, ok := findStep(report.Steps, domain.ReadinessStepID(stepID))
	if !ok {
		return domain.ReadinessRunResult{}, domain.ErrReadinessUnknownStep
	}
	if !runnable(step) {
		return domain.ReadinessRunResult{}, domain.ErrReadinessNotRunnable
	}
	if step.Action == domain.ReadinessActionTask && !approved {
		return domain.ReadinessRunResult{}, domain.ErrReadinessNotApproved
	}
	defer s.forget(ctx, repositoryID)
	return s.start(ctx, facts, step, nil)
}

// RunAll starts every runnable step in order. Tasks are opened one behind the
// other with blocked_by, so the board works through them in sequence.
// approveTasks is the number of tasks the person saw and agreed to.
func (s *Service) RunAll(ctx context.Context, repositoryID uuid.UUID, approveTasks *int) (domain.ReadinessRunAllResult, error) {
	report, facts, err := s.compute(ctx, repositoryID)
	if err != nil {
		return domain.ReadinessRunAllResult{}, err
	}
	plan := report.RunAll
	if plan.Tasks > 0 {
		if approveTasks == nil {
			return domain.ReadinessRunAllResult{}, domain.ErrReadinessNotApproved
		}
		if *approveTasks != plan.Tasks {
			return domain.ReadinessRunAllResult{}, domain.ErrReadinessPlanChanged
		}
	}
	defer s.forget(ctx, repositoryID)

	// The task opened last, in step order, is the one the next waits for.
	var last *domain.ReadinessTask
	out := domain.ReadinessRunAllResult{Results: []domain.ReadinessRunResult{}}
	for _, step := range report.Steps {
		if step.Status == domain.ReadinessRunning && step.Task != nil {
			last = step.Task
			continue
		}
		if !planned(plan, step.ID) {
			continue
		}
		res, err := s.start(ctx, facts, step, last)
		if err != nil {
			return out, fmt.Errorf("%s: %w", step.ID, err)
		}
		out.Results = append(out.Results, res)
		if res.Task != nil {
			last = res.Task
		}
	}
	return out, nil
}

func planned(plan domain.ReadinessRunAll, id domain.ReadinessStepID) bool {
	for _, s := range plan.Steps {
		if s == id {
			return true
		}
	}
	return false
}

func runnable(step domain.ReadinessStep) bool {
	if step.Action != domain.ReadinessActionRun && step.Action != domain.ReadinessActionTask {
		return false
	}
	return step.Status == domain.ReadinessTodo || step.Status == domain.ReadinessUnknown
}

func findStep(steps []domain.ReadinessStep, id domain.ReadinessStepID) (domain.ReadinessStep, bool) {
	for _, st := range steps {
		if st.ID == id {
			return st, true
		}
	}
	return domain.ReadinessStep{}, false
}

func (s *Service) start(ctx context.Context, f *facts, step domain.ReadinessStep, after *domain.ReadinessTask) (domain.ReadinessRunResult, error) {
	res := domain.ReadinessRunResult{Step: step.ID}
	var blockedBy []domain.TaskRelationInput
	if after != nil {
		blockedBy = []domain.TaskRelationInput{{TargetTaskID: after.ID}}
	}
	switch step.ID {
	case domain.ReadinessScan:
		if _, _, err := s.Model.StartScan(ctx, f.repo.ID, domain.ScanTriggerManual); err != nil {
			return res, err
		}
		res.Started = true
	case domain.ReadinessIndex:
		if err := s.Repos.Reindex(ctx, f.repo.ID); err != nil {
			return res, err
		}
		res.Started = true
	case domain.ReadinessGuide:
		task, err := s.openTask(ctx, f.repo.ID, step.ID, guideTaskRequest(f, step, blockedBy))
		if err != nil {
			return res, err
		}
		res.Task = readinessTask(task)
	case domain.ReadinessTests:
		task, err := s.openTask(ctx, f.repo.ID, step.ID, s.testsTaskRequest(ctx, f, blockedBy))
		if err != nil {
			return res, err
		}
		res.Task = readinessTask(task)
	case domain.ReadinessCI:
		task, _, err := s.Repos.CreateWorkflowSetupTaskAfter(ctx, f.repo.ID, blockedBy)
		if err != nil {
			return res, err
		}
		if err := s.Store.SetTask(ctx, f.repo.ID, string(step.ID), task.ID); err != nil {
			return res, err
		}
		res.Task = readinessTask(task)
	default:
		return res, domain.ErrReadinessNotRunnable
	}
	return res, nil
}

func (s *Service) openTask(ctx context.Context, repositoryID uuid.UUID, step domain.ReadinessStepID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	task, err := s.Repos.CreateTask(ctx, repositoryID, req)
	if err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.Store.SetTask(ctx, repositoryID, string(step), task.ID); err != nil {
		return task, err
	}
	return task, nil
}

func readinessTask(t domain.BoardTask) *domain.ReadinessTask {
	return &domain.ReadinessTask{ID: t.ID, Key: t.Key, Title: t.Title, Column: t.Column}
}

func (s *Service) assignee(ctx context.Context, area string) *uuid.UUID {
	if s.Roles == nil {
		return nil
	}
	id, err := s.Roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, area)
	if err != nil {
		return nil
	}
	return id
}

func guideTaskRequest(f *facts, step domain.ReadinessStep, blockedBy []domain.TaskRelationInput) domain.CreateBoardTaskRequest {
	missing, _ := step.Facts["missing"].([]string)
	in := guideInput{Repository: f.repo.Name, Kind: f.repo.Kind, Missing: missing}
	for _, c := range f.active {
		in.Components = append(in.Components, componentBrief{
			Path: c.Path, Role: string(c.Role.Get()), Stack: c.Stack.Get().Summary(6), Commands: commandLines(c),
		})
	}
	return domain.CreateBoardTaskRequest{
		Title:       guideTaskTitle(missing),
		Description: guideKey.Render(in),
		Priority:    domain.TaskPriorityMedium,
		Column:      domain.TaskColumnTodo,
		CreatedBy:   "system",
		BlockedBy:   blockedBy,
	}
}

// guideTaskTitle names only what is actually missing, so a repository that
// already has a README is not asked for one.
func guideTaskTitle(missing []string) string {
	hasReadme, hasGuide := true, true
	for _, m := range missing {
		switch m {
		case "readme":
			hasReadme = false
		case "agent_guide":
			hasGuide = false
		}
	}
	switch {
	case !hasReadme && !hasGuide:
		return "Add a README and an AGENTS.md agent guide"
	case !hasReadme:
		return "Add a README"
	default:
		return "Add an AGENTS.md agent guide"
	}
}

func (s *Service) testsTaskRequest(ctx context.Context, f *facts, blockedBy []domain.TaskRelationInput) domain.CreateBoardTaskRequest {
	in := testsInput{Repository: f.repo.Name}
	var only *uuid.UUID
	for _, c := range f.testGaps {
		in.Components = append(in.Components, testGap{
			Path: c.Path, Role: string(c.Role.Get()), Stack: c.Stack.Get().Summary(6), TestCommand: c.Command(domain.CommandTest),
		})
	}
	if len(f.testGaps) == 1 {
		id := f.testGaps[0].ID
		only = &id
	}
	area := domain.RepoArea(f.repo.Kind, f.repo.SubProjects)
	if only != nil {
		area = domain.ComponentArea(f.testGaps[0].Role.Get())
	}
	return domain.CreateBoardTaskRequest{
		Title:           "Add automated tests",
		Description:     testsKey.Render(in),
		TaskType:        domain.TaskTypeTechnical,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		ComponentID:     only,
		AssigneeAgentID: s.assignee(ctx, area),
		BlockedBy:       blockedBy,
	}
}

func commandLines(c domain.Component) []string {
	var out []string
	for _, cmd := range c.Commands {
		if v := strings.TrimSpace(cmd.Command.Get()); v != "" {
			out = append(out, string(cmd.Purpose)+": "+v)
		}
	}
	return out
}
