package projectmodel

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/ci"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SetRoleResolver wires who a system-opened gate CI task is handed to; nil
// leaves the task unassigned.
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

type gateCITaskInput struct {
	Gate       string
	Repository string
	Component  string
	Threshold  string
	Jobs       []string
	Measured   []string
	Toolchains []gateCIToolchain
}

type gateCIToolchain struct {
	Gate string
	ID   string
	Name string
}

var gateCITaskKey = prompt.Define("projectmodel.gate_ci_task", gateCITaskInput{
	Gate:       "coverage",
	Repository: "acme",
	Component:  "api (services/api)",
	Threshold:  "80",
	Jobs:       []string{".github/workflows/ci.yml › test"},
	Measured:   []string{"Go"},
	Toolchains: []gateCIToolchain{{Gate: "coverage", ID: "node", Name: "TypeScript/JavaScript"}},
})

// gatesSwitchedOn is what a patch turned on; a threshold edit or a gate that
// was already on opens nothing.
func gatesSwitchedOn(before, after domain.ComponentGates) []domain.QualityGateKind {
	var out []domain.QualityGateKind
	for _, kind := range []domain.QualityGateKind{domain.QualityGateCoverage, domain.QualityGateMutation} {
		if !before.Enabled(kind) && after.Enabled(kind) {
			out = append(out, kind)
		}
	}
	return out
}

// openGateCITasks never fails the edit that switched the gates on: the gate
// itself already landed, and a task the board could not open is logged.
func (s *Service) openGateCITasks(ctx context.Context, componentID uuid.UUID, kinds []domain.QualityGateKind) []domain.GateCITask {
	if len(kinds) == 0 || s.tasks == nil {
		return nil
	}
	s.gateTaskMu.Lock()
	defer s.gateTaskMu.Unlock()

	var out []domain.GateCITask
	for _, kind := range kinds {
		res, ok, err := s.openGateCITask(ctx, componentID, kind)
		if err != nil {
			log.Warn().Err(err).Str("component_id", componentID.String()).Str("gate", string(kind)).Msg("project model: gate CI task not opened")
			continue
		}
		if ok {
			out = append(out, res)
		}
	}
	return out
}

func (s *Service) openGateCITask(ctx context.Context, componentID uuid.UUID, kind domain.QualityGateKind) (domain.GateCITask, bool, error) {
	comp, err := s.store.GetComponent(ctx, componentID)
	if err != nil {
		return domain.GateCITask{}, false, err
	}
	open, ok, err := s.openGateTask(ctx, comp, kind)
	if err != nil {
		return domain.GateCITask{}, false, err
	}
	if ok {
		return gateCITaskResult(kind, false, nil, open), true, nil
	}
	toolchains := ci.ToolchainsFor(stackLanguages(comp))
	if len(toolchains) == 0 {
		return domain.GateCITask{}, false, nil
	}

	checks, err := s.componentCIChecks(ctx, comp)
	if err != nil {
		return domain.GateCITask{}, false, err
	}
	measured, missing := ci.SplitByGate(kind, toolchains, checks)
	if len(missing) == 0 {
		return domain.GateCITask{}, false, nil
	}

	repo, err := s.repos.Get(ctx, comp.RepositoryID)
	if err != nil {
		return domain.GateCITask{}, false, err
	}
	input := gateCITaskInput{
		Gate:       string(kind),
		Repository: repo.Name,
		Component:  componentRef(comp),
		Measured:   toolchainNames(measured),
	}
	if threshold := comp.Gates.Threshold(kind); threshold > 0 {
		input.Threshold = fmt.Sprintf("%.0f", threshold)
	}
	for _, c := range checks {
		input.Jobs = append(input.Jobs, checkRef(c))
	}
	for _, tc := range missing {
		input.Toolchains = append(input.Toolchains, gateCIToolchain{Gate: string(kind), ID: tc.ID, Name: tc.Name})
	}

	task, err := s.tasks.CreateTask(ctx, comp.RepositoryID, domain.CreateBoardTaskRequest{
		Title:           gateCITaskTitle(kind, comp, toolchainNames(missing)),
		TaskType:        domain.TaskTypeTechnical,
		Description:     gateCITaskKey.Render(input),
		ComponentID:     &comp.ID,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.systemAssignee(ctx, comp.Role.Get()),
	})
	if err != nil {
		return domain.GateCITask{}, false, fmt.Errorf("create task: %w", err)
	}

	// Re-read so this save keeps whatever another edit wrote while the task
	// was being opened.
	latest, err := s.store.GetComponent(ctx, componentID)
	if err != nil {
		return domain.GateCITask{}, false, err
	}
	taskID := task.ID
	latest.Gates.SetCITask(kind, &taskID)
	if _, err := s.store.SaveComponent(ctx, latest); err != nil {
		return domain.GateCITask{}, false, err
	}
	return gateCITaskResult(kind, true, toolchainNames(missing), task), true, nil
}

// openGateTask: a deleted task, or one that reached done/released, no longer
// holds the slot — the CI step it was for may have been removed since.
func (s *Service) openGateTask(ctx context.Context, comp domain.Component, kind domain.QualityGateKind) (domain.BoardTask, bool, error) {
	id := comp.Gates.CITask(kind)
	if id == nil {
		return domain.BoardTask{}, false, nil
	}
	task, err := s.tasks.GetTask(ctx, comp.RepositoryID, *id)
	if errors.Is(err, domain.ErrBoardTaskNotFound) || errors.Is(err, port.ErrNotFound) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if task.Column == domain.TaskColumnDone || task.Column == domain.TaskColumnReleased {
		return domain.BoardTask{}, false, nil
	}
	return task, true, nil
}

func (s *Service) componentCIChecks(ctx context.Context, comp domain.Component) ([]domain.ComponentCheck, error) {
	all, err := s.store.ListChecks(ctx, comp.RepositoryID)
	if err != nil {
		return nil, err
	}
	var out []domain.ComponentCheck
	for _, c := range all {
		if c.ComponentID == comp.ID && c.Source == domain.CheckSourceCI && c.Status == domain.ModelStatusActive && !c.Missing {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Service) systemAssignee(ctx context.Context, role domain.ComponentRole) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, domain.RepoArea(role.LegacyRepoKind(), nil))
	if err != nil {
		return nil
	}
	return id
}

func stackLanguages(comp domain.Component) []string {
	var out []string
	for _, l := range comp.Stack.Get().Languages {
		out = append(out, l.Name)
	}
	return out
}

func toolchainNames(tcs []ci.Toolchain) []string {
	out := make([]string, 0, len(tcs))
	for _, tc := range tcs {
		out = append(out, tc.Name)
	}
	return out
}

func componentRef(comp domain.Component) string {
	if comp.Path == "." || comp.Path == "" {
		return comp.DisplayName() + " (repository root)"
	}
	return fmt.Sprintf("%s (%s)", comp.DisplayName(), comp.Path)
}

func checkRef(c domain.ComponentCheck) string {
	job := c.JobKey
	if c.JobName != "" && c.JobName != c.JobKey {
		job = c.JobName
	}
	return c.Workflow + " › " + job
}

func gateCITaskTitle(kind domain.QualityGateKind, comp domain.Component, languages []string) string {
	what := "coverage reporting"
	if kind == domain.QualityGateMutation {
		what = "mutation testing"
	}
	return fmt.Sprintf("Add %s to CI for %s (%s)", what, comp.DisplayName(), strings.Join(languages, ", "))
}

func gateCITaskResult(kind domain.QualityGateKind, created bool, languages []string, task domain.BoardTask) domain.GateCITask {
	return domain.GateCITask{
		Gate:      kind,
		Created:   created,
		Languages: languages,
		TaskID:    task.ID,
		TaskKey:   task.Key,
		TaskTitle: task.Title,
	}
}
