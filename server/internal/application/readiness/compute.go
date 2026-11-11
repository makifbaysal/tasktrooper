package readiness

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/findings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxNamed = 5

type facts struct {
	repo     domain.Repository
	model    domain.RepositoryModel
	active   []domain.Component
	testGaps []domain.Component
}

func deployable(role domain.ComponentRole) bool {
	switch role {
	case domain.ComponentRoleFrontend, domain.ComponentRoleBackend, domain.ComponentRoleWorker, domain.ComponentRoleMobile:
		return true
	}
	return false
}

func testable(role domain.ComponentRole) bool {
	switch role {
	case domain.ComponentRoleInfra, domain.ComponentRoleOther:
		return false
	}
	return true
}

func (s *Service) compute(ctx context.Context, repositoryID uuid.UUID) (domain.RepositoryReadiness, *facts, error) {
	repo, err := s.Repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryReadiness{}, nil, err
	}
	model, err := s.Model.RepositoryModel(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryReadiness{}, nil, err
	}
	states, err := s.Store.List(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryReadiness{}, nil, err
	}
	f := &facts{repo: repo, model: model}
	for _, c := range model.Components {
		if c.Status == domain.ComponentStatusActive {
			f.active = append(f.active, c)
		}
	}
	byStep := map[string]domain.ReadinessStepState{}
	dismissed := false
	for _, st := range states {
		if st.StepID == domain.ReadinessCardKey {
			dismissed = st.Skipped
			continue
		}
		byStep[st.StepID] = st
	}

	scanDone := scanFinishedWithComponents(model, f.active)
	var steps []domain.ReadinessStep
	add := func(st *domain.ReadinessStep) {
		if st != nil {
			steps = append(steps, *st)
		}
	}
	add(s.scanStep(model, f.active))
	add(toolchainStep(f.active, scanDone))
	add(s.indexStep(ctx, repo))
	add(s.guideStep(ctx, f))
	add(testsStep(f))
	add(ciStep(f))
	add(deliveryStep(f))
	add(s.observabilityStep(ctx, f))
	add(findingsStep(f))
	add(s.designStep(ctx, f))
	add(s.storeStep(ctx, f))

	s.applyChoices(ctx, repo, steps, byStep)
	applyDependencies(steps)

	report := domain.RepositoryReadiness{RepositoryID: repositoryID, Steps: steps, Dismissed: dismissed}
	for _, st := range steps {
		switch st.Status {
		case domain.ReadinessDone:
			report.Done++
		case domain.ReadinessSkipped:
			report.Skipped++
		}
	}
	report.Total = len(steps)
	report.Complete = report.Total > 0 && report.Done+report.Skipped == report.Total
	report.RunAll = runAllPlan(steps)
	f.testGaps = testGaps(f)
	return report, f, nil
}

func scanFinishedWithComponents(model domain.RepositoryModel, active []domain.Component) bool {
	return model.LatestScan != nil && model.LatestScan.Status == domain.ScanSucceeded && len(active) > 0
}

func (s *Service) scanStep(model domain.RepositoryModel, active []domain.Component) *domain.ReadinessStep {
	st := &domain.ReadinessStep{ID: domain.ReadinessScan, Action: domain.ReadinessActionRun, DependsOn: []string{}}
	scan := model.LatestScan
	switch {
	case scan != nil && (scan.Status == domain.ScanRunning || scan.Status == domain.ScanQueued):
		st.Status = domain.ReadinessRunning
	case scan != nil && scan.Status == domain.ScanSucceeded && len(active) > 0:
		st.Status = domain.ReadinessDone
		st.Facts = map[string]any{"components": len(active)}
	case scan != nil && scan.Status == domain.ScanSucceeded:
		st.Status, st.Reason = domain.ReadinessTodo, "no_components"
	case scan != nil && scan.Status == domain.ScanFailed:
		st.Status, st.Reason = domain.ReadinessTodo, "scan_failed"
	default:
		st.Status, st.Reason = domain.ReadinessTodo, "no_scan"
	}
	return st
}

func toolchainStep(active []domain.Component, scanDone bool) *domain.ReadinessStep {
	st := &domain.ReadinessStep{ID: domain.ReadinessToolchain, Action: domain.ReadinessActionDesktop, DependsOn: []string{string(domain.ReadinessScan)}}
	if !scanDone {
		st.Status = domain.ReadinessTodo
		return st
	}
	seen := map[string]bool{}
	var languages []string
	for _, c := range active {
		for _, l := range c.Stack.Get().Languages {
			name := strings.ToLower(strings.TrimSpace(l.Name))
			if name != "" && !seen[name] {
				seen[name] = true
				languages = append(languages, name)
			}
		}
	}
	if len(languages) == 0 {
		return nil
	}
	sort.Strings(languages)
	st.Status = domain.ReadinessUnknown
	st.Facts = map[string]any{"languages": languages}
	return st
}

func (s *Service) indexStep(ctx context.Context, repo domain.Repository) *domain.ReadinessStep {
	st := &domain.ReadinessStep{ID: domain.ReadinessIndex, Action: domain.ReadinessActionRun, DependsOn: []string{}}
	idx, err := s.Repos.IndexStatus(ctx, repo.ID)
	switch {
	case err != nil:
		st.Status, st.Reason = domain.ReadinessUnknown, "unavailable"
	case idx.Status == domain.IndexStatusCompleted:
		st.Status = domain.ReadinessDone
		st.Facts = map[string]any{"files": idx.FileCount}
	case idx.Status == domain.IndexStatusRunning || (idx.Status == domain.IndexStatusPending && idx.FilesTotal > 0):
		st.Status = domain.ReadinessRunning
		st.Facts = map[string]any{"processed": idx.FilesProcessed, "total": idx.FilesTotal}
	case idx.Status == domain.IndexStatusFailed:
		st.Status, st.Reason = domain.ReadinessTodo, "failed"
	default:
		st.Status, st.Reason = domain.ReadinessTodo, "not_indexed"
	}
	return st
}

func (s *Service) guideStep(ctx context.Context, f *facts) *domain.ReadinessStep {
	st := &domain.ReadinessStep{ID: domain.ReadinessGuide, Action: domain.ReadinessActionTask, DependsOn: []string{string(domain.ReadinessScan)}}
	root, err := s.Repos.ResolveRootPath(ctx, f.repo.ID)
	var names []string
	if err == nil {
		names, err = s.ListRoot(root)
	}
	if err != nil {
		st.Status, st.Reason = domain.ReadinessUnknown, "unreadable"
		return st
	}
	hasReadme, hasGuide := false, docsConfigured(f)
	for _, n := range names {
		switch l := strings.ToLower(n); {
		case l == "readme" || strings.HasPrefix(l, "readme."):
			hasReadme = true
		case l == "claude.md" || l == "agents.md" || l == "gemini.md" || l == ".ai":
			hasGuide = true
		}
	}
	var missing []string
	if !hasReadme {
		missing = append(missing, "readme")
	}
	if !hasGuide {
		missing = append(missing, "agent_guide")
	}
	if len(missing) == 0 {
		st.Status = domain.ReadinessDone
		return st
	}
	st.Status, st.Reason = domain.ReadinessTodo, "missing"
	st.Facts = map[string]any{"missing": missing}
	return st
}

func docsConfigured(f *facts) bool {
	docsSet := func(d domain.RepositoryDocs) bool {
		return d.CodingStandards != "" || d.TestStandards != "" || d.Architecture != "" || d.LocalRun != ""
	}
	if docsSet(f.repo.Docs) {
		return true
	}
	for _, c := range f.active {
		if docsSet(c.Docs) {
			return true
		}
	}
	return false
}

func testGaps(f *facts) []domain.Component {
	open := map[uuid.UUID]bool{}
	for _, fd := range f.model.Findings {
		if fd.Rule == findings.RuleNoAutomatedTests && fd.Status == domain.FindingOpen && fd.ComponentID != nil {
			open[*fd.ComponentID] = true
		}
	}
	var gaps []domain.Component
	for _, c := range f.active {
		if !testable(c.Role.Get()) {
			continue
		}
		if c.Command(domain.CommandTest) == "" || open[c.ID] {
			gaps = append(gaps, c)
		}
	}
	return gaps
}

func componentNames(cs []domain.Component) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		if len(names) == maxNamed {
			break
		}
		names = append(names, c.DisplayName())
	}
	return names
}

func testsStep(f *facts) *domain.ReadinessStep {
	if len(f.active) == 0 {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessTests, Action: domain.ReadinessActionTask, DependsOn: []string{string(domain.ReadinessScan)}}
	gaps := testGaps(f)
	if len(gaps) == 0 {
		st.Status = domain.ReadinessDone
		return st
	}
	st.Status, st.Reason = domain.ReadinessTodo, "gaps"
	st.Facts = map[string]any{"count": len(gaps), "components": componentNames(gaps)}
	return st
}

func ciStep(f *facts) *domain.ReadinessStep {
	if len(f.active) == 0 || strings.TrimSpace(f.repo.RemoteURL) == "" {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessCI, Action: domain.ReadinessActionTask, DependsOn: []string{string(domain.ReadinessScan)}}
	checks := 0
	for _, ch := range f.model.Checks {
		if ch.Status == domain.ModelStatusActive && !ch.Missing {
			checks++
		}
	}
	if checks > 0 {
		st.Status = domain.ReadinessDone
		st.Facts = map[string]any{"checks": checks}
		return st
	}
	st.Status, st.Reason = domain.ReadinessTodo, "no_ci"
	return st
}

func deployables(f *facts) []domain.Component {
	var out []domain.Component
	for _, c := range f.active {
		if deployable(c.Role.Get()) {
			out = append(out, c)
		}
	}
	return out
}

func deliveryStep(f *facts) *domain.ReadinessStep {
	comps := deployables(f)
	if len(comps) == 0 {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessDelivery, Action: domain.ReadinessActionNavigate, DependsOn: []string{string(domain.ReadinessScan)}}
	var missing []domain.Component
	for _, c := range comps {
		d, ok := domain.DeliveryConfirmed(c.Delivery)
		if !ok || d.Mode == domain.DeliveryNone {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		st.Status = domain.ReadinessDone
		return st
	}
	st.Status, st.Reason = domain.ReadinessTodo, "gaps"
	st.Facts = map[string]any{"count": len(missing), "components": componentNames(missing)}
	return st
}

func (s *Service) observabilityStep(ctx context.Context, f *facts) *domain.ReadinessStep {
	comps := deployables(f)
	if s.Observability == nil || len(comps) == 0 {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessObservability, Action: domain.ReadinessActionNavigate, DependsOn: []string{string(domain.ReadinessDelivery)}}
	var gaps []domain.Component
	for _, c := range comps {
		r, err := s.Observability.Readiness(ctx, c.ID, domain.EnvironmentProduction)
		if err == nil && len(r.Suggestions) > 0 {
			gaps = append(gaps, c)
		}
	}
	if len(gaps) == 0 {
		st.Status = domain.ReadinessDone
		return st
	}
	st.Status, st.Reason = domain.ReadinessTodo, "gaps"
	st.Facts = map[string]any{"count": len(gaps), "components": componentNames(gaps), "component_id": gaps[0].ID.String()}
	return st
}

func findingsStep(f *facts) *domain.ReadinessStep {
	if len(f.active) == 0 {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessFindings, Action: domain.ReadinessActionNavigate, DependsOn: []string{string(domain.ReadinessScan)}}
	open, high := 0, 0
	for _, fd := range f.model.Findings {
		if fd.Rule == findings.RuleNoAutomatedTests || fd.Status != domain.FindingOpen || fd.TaskID != nil {
			continue
		}
		open++
		if fd.Severity == domain.FindingSeverityHigh {
			high++
		}
	}
	if open == 0 {
		st.Status = domain.ReadinessDone
		return st
	}
	st.Status, st.Reason = domain.ReadinessTodo, "open"
	st.Facts = map[string]any{"count": open, "high": high}
	return st
}

func (s *Service) designStep(ctx context.Context, f *facts) *domain.ReadinessStep {
	if s.Designs == nil {
		return nil
	}
	hasUI := false
	for _, c := range f.active {
		if c.Role.Get() == domain.ComponentRoleFrontend {
			hasUI = true
		}
	}
	if !hasUI {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessDesign, Action: domain.ReadinessActionNavigate, DependsOn: []string{string(domain.ReadinessScan)}}
	eff, err := s.Designs.Effective(ctx, f.repo.ID)
	switch {
	case err != nil:
		st.Status, st.Reason = domain.ReadinessUnknown, "unavailable"
	case !eff.Empty():
		st.Status = domain.ReadinessDone
	default:
		st.Status, st.Reason = domain.ReadinessTodo, "none"
	}
	return st
}

func (s *Service) storeStep(ctx context.Context, f *facts) *domain.ReadinessStep {
	if s.StoreApps == nil {
		return nil
	}
	hasMobile := false
	for _, c := range f.active {
		if c.Role.Get() == domain.ComponentRoleMobile || c.Mobile != nil {
			hasMobile = true
		}
	}
	if !hasMobile {
		return nil
	}
	st := &domain.ReadinessStep{ID: domain.ReadinessStore, Action: domain.ReadinessActionNavigate, DependsOn: []string{string(domain.ReadinessScan)}}
	apps, err := s.StoreApps.ListByRepository(ctx, f.repo.ID)
	if err != nil {
		st.Status, st.Reason = domain.ReadinessUnknown, "unavailable"
		return st
	}
	for _, a := range apps {
		if a.StoreAppID != "" || a.AppName != "" {
			st.Status = domain.ReadinessDone
			return st
		}
	}
	st.Status, st.Reason = domain.ReadinessTodo, "no_app"
	return st
}

// applyChoices layers the person's skips and the board tasks steps opened
// over the derived statuses. A step that is actually done stays done.
func (s *Service) applyChoices(ctx context.Context, repo domain.Repository, steps []domain.ReadinessStep, byStep map[string]domain.ReadinessStepState) {
	for i := range steps {
		st := &steps[i]
		if st.Status == domain.ReadinessDone {
			continue
		}
		if st.Action == domain.ReadinessActionTask && (st.Status == domain.ReadinessTodo || st.Status == domain.ReadinessUnknown) {
			if task := s.openStepTask(ctx, repo, *st, byStep[string(st.ID)]); task != nil {
				st.Task = task
				st.Status = domain.ReadinessRunning
				continue
			}
		}
		if byStep[string(st.ID)].Skipped && st.Status != domain.ReadinessRunning {
			st.Status = domain.ReadinessSkipped
		}
	}
}

func (s *Service) openStepTask(ctx context.Context, repo domain.Repository, st domain.ReadinessStep, state domain.ReadinessStepState) *domain.ReadinessTask {
	var candidates []uuid.UUID
	if state.TaskID != nil {
		candidates = append(candidates, *state.TaskID)
	}
	if st.ID == domain.ReadinessCI {
		if t, ok, err := s.Repos.WorkflowSetupTask(ctx, repo.ID); err == nil && ok {
			return readinessTask(t)
		}
	}
	for _, id := range candidates {
		t, err := s.Repos.GetTask(ctx, repo.ID, id)
		if err != nil || t.Column == domain.TaskColumnDone || t.Column == domain.TaskColumnReleased {
			continue
		}
		return readinessTask(t)
	}
	return nil
}

// applyDependencies blocks a step that is still to do while a step it reads
// from has not finished.
func applyDependencies(steps []domain.ReadinessStep) {
	status := map[string]domain.ReadinessStatus{}
	for _, st := range steps {
		status[string(st.ID)] = st.Status
	}
	for i := range steps {
		st := &steps[i]
		if st.Status != domain.ReadinessTodo {
			continue
		}
		for _, dep := range st.DependsOn {
			d, present := status[dep]
			if !present || d == domain.ReadinessDone || d == domain.ReadinessSkipped {
				continue
			}
			st.Status = domain.ReadinessBlocked
			st.Reason = "needs_" + dep
			break
		}
	}
}

func runAllPlan(steps []domain.ReadinessStep) domain.ReadinessRunAll {
	plan := domain.ReadinessRunAll{Steps: []domain.ReadinessStepID{}}
	for _, st := range steps {
		if !runnable(st) || st.Status != domain.ReadinessTodo {
			continue
		}
		plan.Steps = append(plan.Steps, st.ID)
		if st.Action == domain.ReadinessActionTask {
			plan.Tasks++
		} else {
			plan.Runs++
		}
	}
	return plan
}
