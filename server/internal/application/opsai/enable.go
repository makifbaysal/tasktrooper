package opsai

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	maxEndpointLen    = 2048
	stackSummaryN     = 8
	enableTaskCreator = "system"
)

// EnableRequest is the body of POST /v1/components/:id/observability/enable.
type EnableRequest struct {
	Environment  string   `json:"environment"`
	Goals        []string `json:"goals"`
	Target       string   `json:"target"`
	OTLPEndpoint string   `json:"otlp_endpoint"`
}

// Enable opens the board task that has the release engineer define the
// instrumentation work for the component and hand it to the component's
// developer: the brief carries the component's stack, which signals are
// bound and what its code already reports.
func (s *Service) Enable(ctx context.Context, componentID uuid.UUID, req EnableRequest) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, ErrUnavailable
	}
	fe := FieldErrors{}
	env := environmentOf(req.Environment, fe)
	goals := goalsOf(req.Goals, fe)
	target := domain.ObservabilityTarget(strings.TrimSpace(req.Target))
	switch {
	case target == "":
		fe["target"] = datasource.FieldRequired
	case !domain.ValidObservabilityTarget(target):
		fe["target"] = datasource.FieldInvalid
	}
	endpoint := strings.TrimSpace(req.OTLPEndpoint)
	switch {
	case target == domain.TargetOTLPEndpoint && endpoint == "":
		fe["otlp_endpoint"] = datasource.FieldRequired
	case target != domain.TargetOTLPEndpoint && endpoint != "":
		fe["otlp_endpoint"] = datasource.FieldInvalidOption
	case endpoint != "" && !validEndpoint(endpoint):
		fe["otlp_endpoint"] = datasource.FieldInvalid
	}
	if len(fe) > 0 {
		return domain.BoardTask{}, fe
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	repo, err := s.repos.Get(ctx, comp.RepositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	agentID, err := s.releaseEngineer(ctx, domain.ComponentArea(comp.Role.Get()))
	if err != nil {
		return domain.BoardTask{}, err
	}
	readiness, err := s.Readiness(ctx, comp.ID, env)
	if err != nil {
		return domain.BoardTask{}, err
	}
	var bound, unbound, found []string
	for _, sig := range readiness.Signals {
		if sig.Status == domain.SignalStateBound {
			bound = append(bound, sig.Signal)
		} else if sig.Golden {
			unbound = append(unbound, sig.Signal)
		}
	}
	for key, ok := range readiness.Instrumentation {
		if ok {
			found = append(found, key)
		}
	}
	sort.Strings(found)
	description := enableBriefKey.Render(enableBriefInput{
		Repository:      repo.Name,
		Component:       comp.DisplayName(),
		Path:            comp.Path,
		Role:            string(comp.Role.Get()),
		Environment:     string(env),
		Provider:        string(readiness.Provider),
		Stack:           comp.Stack.Get().Summary(stackSummaryN),
		Goals:           goals,
		Target:          string(target),
		OTLPEndpoint:    endpoint,
		Bound:           bound,
		Unbound:         unbound,
		Instrumentation: found,
	})
	title := strings.TrimSpace(enableTitleKey.Render(enableTitleInput{Component: comp.DisplayName(), Environment: string(env)}))
	if open, ok, err := s.openEnableTask(ctx, repo.ID, comp.ID, title); err != nil {
		return domain.BoardTask{}, err
	} else if ok {
		return open, nil
	}
	compID := comp.ID
	return s.tasks.CreateTask(ctx, repo.ID, domain.CreateBoardTaskRequest{
		Title:           title,
		Description:     description,
		TaskType:        domain.TaskTypeTechnical,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       enableTaskCreator,
		ComponentID:     &compID,
		AssigneeAgentID: &agentID,
	})
}

// openEnableTask is the component environment's enable task still on the
// board short of done: asking again answers it rather than a second one.
func (s *Service) openEnableTask(ctx context.Context, repositoryID, componentID uuid.UUID, title string) (domain.BoardTask, bool, error) {
	tasks, err := s.tasks.ListTasks(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	for _, t := range tasks {
		if t.ComponentID != nil && *t.ComponentID == componentID && t.CreatedBy == enableTaskCreator && t.Title == title &&
			t.Column != domain.TaskColumnDone && t.Column != domain.TaskColumnReleased {
			return t, true, nil
		}
	}
	return domain.BoardTask{}, false, nil
}

func goalsOf(raw []string, fe FieldErrors) []string {
	known := domain.ObservabilityGoals()
	var out []string
	for _, g := range raw {
		goal := domain.ObservabilityGoal(strings.TrimSpace(g))
		if !slices.Contains(known, goal) {
			fe["goals"] = datasource.FieldInvalidOption
			continue
		}
		if !slices.Contains(out, string(goal)) {
			out = append(out, string(goal))
		}
	}
	if len(out) == 0 {
		if _, bad := fe["goals"]; !bad {
			fe["goals"] = datasource.FieldRequired
		}
	}
	return out
}

func validEndpoint(raw string) bool {
	if len(raw) > maxEndpointLen {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
