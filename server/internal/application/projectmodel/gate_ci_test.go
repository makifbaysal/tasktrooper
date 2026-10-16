package projectmodel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/ci"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeRoles struct {
	port.RoleResolver
	agent uuid.UUID
	areas []string
}

func (f *fakeRoles) AgentForPurpose(_ context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error) {
	if purpose != domain.PurposeSystemTaskAssignee {
		return nil, nil
	}
	f.areas = append(f.areas, area)
	return &f.agent, nil
}

type gateHarness struct {
	svc   *Service
	store *b2Store
	tasks *fakeTaskCreator
	roles *fakeRoles
	repo  domain.Repository
	comp  domain.Component
}

func newGateHarness(t *testing.T, languages ...string) *gateHarness {
	t.Helper()
	svc, store, repos, _, _, _ := newB2Service(t)
	h := &gateHarness{svc: svc, store: store, tasks: &fakeTaskCreator{}, roles: &fakeRoles{agent: uuid.New()}}
	svc.SetTaskCreator(h.tasks)
	svc.SetRoleResolver(h.roles)
	h.repo = b2SeedRepo(t, repos, "acme")
	var stack domain.ComponentStack
	for _, l := range languages {
		stack.Languages = append(stack.Languages, domain.StackItem{Name: l})
	}
	h.comp = b2SeedComponent(t, store, domain.Component{
		RepositoryID: h.repo.ID, Path: "services/api", Status: domain.ComponentStatusActive,
		Name:  domain.Detected("api", domain.ConfidenceHigh),
		Role:  domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceHigh),
		Stack: domain.Detected(stack, domain.ConfidenceHigh),
	})
	return h
}

func (h *gateHarness) ci(t *testing.T, jobKey string, runs ...string) {
	t.Helper()
	check := domain.ComponentCheck{
		RepositoryID: h.repo.ID, ComponentID: h.comp.ID, Source: domain.CheckSourceCI, Status: domain.ModelStatusActive,
		Workflow: ".github/workflows/ci.yml", JobKey: jobKey,
	}
	for _, r := range runs {
		check.Steps = append(check.Steps, domain.CheckStep{Run: r})
	}
	_, err := h.store.SaveCheck(context.Background(), check)
	require.NoError(t, err)
}

func (h *gateHarness) setGates(t *testing.T, gates domain.ComponentGates) (domain.Component, []domain.GateCITask) {
	t.Helper()
	comp, tasks, err := h.svc.UpdateComponentWithGateTasks(context.Background(), h.comp.ID, domain.ComponentPatch{Gates: &gates})
	require.NoError(t, err)
	return comp, tasks
}

func on() *bool  { v := true; return &v }
func off() *bool { v := false; return &v }

func TestSwitchingCoverageOnOpensATaskForEachLanguageCIDoesNotMeasure(t *testing.T) {
	h := newGateHarness(t, "Go", "TypeScript", "Shell")
	h.ci(t, "test", "go test -coverprofile=coverage.out ./...", "npx vitest run")

	threshold := 85.0
	comp, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on(), CoverageThreshold: &threshold})

	require.Len(t, h.tasks.got, 1)
	req := h.tasks.got[0]
	assert.Equal(t, "Add coverage reporting to CI for api (TypeScript/JavaScript)", req.Title)
	assert.Equal(t, domain.TaskColumnTodo, req.Column)
	assert.Equal(t, domain.TaskTypeTechnical, req.TaskType)
	assert.Equal(t, "system", req.CreatedBy)
	require.NotNil(t, req.ComponentID)
	assert.Equal(t, h.comp.ID, *req.ComponentID)
	assert.Equal(t, &h.roles.agent, req.AssigneeAgentID)
	assert.Equal(t, []string{domain.RepoKindBackend}, h.roles.areas)
	assertGolden(t, "gate_ci_task_coverage", req.Description)

	require.NotNil(t, comp.Gates.CoverageCITaskID, "the returned component carries the recorded task")
	require.Len(t, opened, 1)
	assert.Equal(t, domain.GateCITask{
		Gate: domain.QualityGateCoverage, Created: true, Languages: []string{"TypeScript/JavaScript"},
		TaskID: *comp.Gates.CoverageCITaskID, TaskKey: "T-1", TaskTitle: req.Title,
	}, opened[0])
	assert.Equal(t, threshold, *comp.Gates.CoverageThreshold)
}

func TestSwitchingMutationOnWithNoCIJobDescribesAWorkflowToAdd(t *testing.T) {
	h := newGateHarness(t, "Python")

	_, opened := h.setGates(t, domain.ComponentGates{MutationEnabled: on()})

	require.Len(t, opened, 1)
	assert.Equal(t, domain.QualityGateMutation, opened[0].Gate)
	assert.Equal(t, "Add mutation testing to CI for api (Python)", h.tasks.got[0].Title)
	assertGolden(t, "gate_ci_task_mutation_no_ci", h.tasks.got[0].Description)
}

func TestSwitchingBothGatesOnOpensOneTaskPerGate(t *testing.T) {
	h := newGateHarness(t, "Rust")
	h.ci(t, "test", "cargo test")

	_, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on(), MutationEnabled: on()})

	require.Len(t, opened, 2)
	assert.Equal(t, domain.QualityGateCoverage, opened[0].Gate)
	assert.Equal(t, domain.QualityGateMutation, opened[1].Gate)
	comp, err := h.store.GetComponent(context.Background(), h.comp.ID)
	require.NoError(t, err)
	assert.Equal(t, opened[0].TaskID, *comp.Gates.CoverageCITaskID)
	assert.Equal(t, opened[1].TaskID, *comp.Gates.MutationCITaskID)
}

func TestCIThatAlreadyMeasuresTheGateOpensNothing(t *testing.T) {
	h := newGateHarness(t, "Go", "TypeScript")
	h.ci(t, "server", "go test -coverprofile=c.out ./...", "gremlins unleash")
	h.ci(t, "web", "npx vitest run --coverage", "npx stryker run")

	_, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on(), MutationEnabled: on()})

	assert.Empty(t, opened)
	assert.Empty(t, h.tasks.got)
}

func TestCIChecksOfOtherComponentsOrDismissedOnesDoNotCount(t *testing.T) {
	h := newGateHarness(t, "Go")
	other := domain.ComponentCheck{
		RepositoryID: h.repo.ID, ComponentID: uuid.New(), Source: domain.CheckSourceCI, Status: domain.ModelStatusActive,
		JobKey: "other", Steps: []domain.CheckStep{{Run: "go test -coverprofile=c.out ./..."}},
	}
	dismissed := domain.ComponentCheck{
		RepositoryID: h.repo.ID, ComponentID: h.comp.ID, Source: domain.CheckSourceCI, Status: domain.ModelStatusDismissed,
		JobKey: "old", Steps: []domain.CheckStep{{Run: "go test -coverprofile=c.out ./..."}},
	}
	for _, c := range []domain.ComponentCheck{other, dismissed} {
		_, err := h.store.SaveCheck(context.Background(), c)
		require.NoError(t, err)
	}

	_, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	assert.Len(t, opened, 1)
}

func TestAnOpenGateTaskIsReportedInsteadOfOpeningASecond(t *testing.T) {
	h := newGateHarness(t, "Go")
	_, first := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	require.Len(t, first, 1)

	h.setGates(t, domain.ComponentGates{CoverageEnabled: off()})
	_, again := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})

	require.Len(t, h.tasks.got, 1)
	require.Len(t, again, 1)
	assert.False(t, again[0].Created)
	assert.Equal(t, first[0].TaskID, again[0].TaskID)
	assert.Equal(t, "T-1", again[0].TaskKey)
}

func TestADoneOrDeletedGateTaskNoLongerHoldsTheSlot(t *testing.T) {
	h := newGateHarness(t, "Go")
	_, first := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	done := h.tasks.tasks[first[0].TaskID]
	done.Column = domain.TaskColumnDone
	h.tasks.tasks[done.ID] = done

	h.setGates(t, domain.ComponentGates{CoverageEnabled: off()})
	_, second := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	require.Len(t, second, 1)
	assert.True(t, second[0].Created)
	assert.NotEqual(t, first[0].TaskID, second[0].TaskID)

	delete(h.tasks.tasks, second[0].TaskID)
	h.setGates(t, domain.ComponentGates{CoverageEnabled: off()})
	_, third := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	require.Len(t, third, 1)
	assert.True(t, third[0].Created)
	assert.Len(t, h.tasks.got, 3)
}

func TestOnlyASwitchFromOffToOnOpensATask(t *testing.T) {
	h := newGateHarness(t, "Go")
	h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	require.Len(t, h.tasks.got, 1)

	threshold := 70.0
	_, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on(), CoverageThreshold: &threshold})
	assert.Empty(t, opened, "a threshold edit on a gate already on is not a switch")

	_, opened = h.setGates(t, domain.ComponentGates{CoverageEnabled: off()})
	assert.Empty(t, opened)
	assert.Len(t, h.tasks.got, 1)
}

func TestAPatchCannotSetOrClearTheRecordedGateTask(t *testing.T) {
	h := newGateHarness(t, "Go")
	comp, _ := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	recorded := *comp.Gates.CoverageCITaskID

	forged := uuid.New()
	got, _ := h.setGates(t, domain.ComponentGates{CoverageEnabled: on(), CoverageCITaskID: &forged, MutationCITaskID: &forged})
	assert.Equal(t, recorded, *got.Gates.CoverageCITaskID)
	assert.Nil(t, got.Gates.MutationCITaskID)

	got, _ = h.setGates(t, domain.ComponentGates{})
	assert.Equal(t, recorded, *got.Gates.CoverageCITaskID)
}

func TestNoGateTaskForALanguageWithoutTools(t *testing.T) {
	h := newGateHarness(t, "Shell", "SQL")
	_, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	assert.Empty(t, opened)
	assert.Empty(t, h.tasks.got)
}

func TestNoGateTaskForADismissedComponent(t *testing.T) {
	h := newGateHarness(t, "Go")
	dismissed := domain.ComponentStatusDismissed
	gates := domain.ComponentGates{CoverageEnabled: on()}
	_, opened, err := h.svc.UpdateComponentWithGateTasks(context.Background(), h.comp.ID, domain.ComponentPatch{Gates: &gates, Status: &dismissed})
	require.NoError(t, err)
	assert.Empty(t, opened)
	assert.Empty(t, h.tasks.got)
}

func TestNoBoardMeansNoGateTaskAndNoError(t *testing.T) {
	h := newGateHarness(t, "Go")
	h.svc.SetTaskCreator(nil)
	comp, opened := h.setGates(t, domain.ComponentGates{CoverageEnabled: on()})
	assert.Empty(t, opened)
	assert.True(t, comp.Gates.Enabled(domain.QualityGateCoverage))
}

func TestEveryToolchainHasNotesForBothGates(t *testing.T) {
	all := ci.ToolchainsFor([]string{"Go", "TypeScript", "Python", "Java", "Scala", "C#", "Rust", "Ruby", "PHP", "Swift", "Dart", "Elixir", "C"})
	require.Len(t, all, 13)
	for _, gate := range []domain.QualityGateKind{domain.QualityGateCoverage, domain.QualityGateMutation} {
		for _, tc := range all {
			out := gateCITaskKey.Render(gateCITaskInput{
				Gate: string(gate), Repository: "acme", Component: "api (api)",
				Toolchains: []gateCIToolchain{{Gate: string(gate), ID: tc.ID, Name: tc.Name}},
			})
			assert.Contains(t, out, "### "+tc.Name+"\n\n- ", "%s/%s", gate, tc.ID)
		}
	}
}
