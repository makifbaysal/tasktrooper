package kpi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type SpanHoursReader interface {
	CleanTaskHours(ctx context.Context, agentID uuid.UUID, columns []string, from, to time.Time) ([]float64, error)
}

type MetricDeps struct {
	Perf  port.AgentPerformanceStore
	Runs  port.TaskAgentRunStore
	Tasks port.BoardTaskStore
	Spans SpanHoursReader

	Workflows port.WorkflowReader
}

var ErrInsufficientData = errors.New("insufficient data for metric")

type MetricResolver func(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error)

type MetricDef struct {
	Info domain.KPIMetricInfo

	Columns []string

	MinSample int
	Resolve   MetricResolver
}

var metricRegistry = map[string]MetricDef{
	"tasks_completed": {
		Info: domain.KPIMetricInfo{
			Key: "tasks_completed", Label: "Tasks Completed", Unit: "tasks",
			Description: "Number of tasks moved into the done/released column",
			Direction:   domain.KPIDirectionHigherBetter,
		},
		Resolve: countDistinctTaskEvents(
			domain.ScoreEventTaskCompleted, domain.ScoreEventTaskReleased,
			domain.ScoreEventQATaskTested,
		),
	},
	"revisions_received": {
		Info: domain.KPIMetricInfo{
			Key: "revisions_received", Label: "Revisions", Unit: "revisions",
			Description: "Number of items bounced back to the need_revision column",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: countScoreEvents(domain.ScoreEventRevisionRequested),
	},
	"uat_failures": {
		Info: domain.KPIMetricInfo{
			Key: "uat_failures", Label: "UAT Failures", Unit: "items",
			Description: "Number of items rejected at human UAT",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: countScoreEvents(domain.ScoreEventHumanUATFailed),
	},
	"failed_runs": {
		Info: domain.KPIMetricInfo{
			Key: "failed_runs", Label: "Failed Runs", Unit: "runs",
			Description: "Agent runs that ended in the failed state",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: resolveFailedRuns,
	},
	"gate_rejected_runs": {
		Info: domain.KPIMetricInfo{
			Key: "gate_rejected_runs", Label: "Gate-Rejected Runs", Unit: "runs",
			Description: "Runs the host rejected for skipping required grounding (reading the repo, running or checking the product)",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: resolveGateRejectedRuns,
	},
	"bugs_assigned": {
		Info: domain.KPIMetricInfo{
			Key: "bugs_assigned", Label: "Bugs Assigned", Unit: "bugs",
			Description: "Bug-type tasks created and assigned to the agent within the period",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: resolveBugsAssigned,
	},
	"first_pass_rate": {
		Info: domain.KPIMetricInfo{
			Key: "first_pass_rate", Label: "First-Pass Rate", Unit: "%",
			Description: "Percentage of tasks completed without a revision (0-100)",
			Direction:   domain.KPIDirectionHigherBetter,
		},
		MinSample: minFirstPassSample,
		Resolve:   resolveFirstPassRate,
	},
	"clean_time_in_progress": {
		Info: domain.KPIMetricInfo{
			Key: "clean_time_in_progress", Label: "Clean Time In Progress", Unit: "hours",
			Description: "Median hours held in in_progress, counting only tasks completed without a revision",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Columns: columnsInProgress, MinSample: minCleanSample,
		Resolve: resolveCleanTime(columnsInProgress, minCleanSample),
	},
	"clean_time_code_review": {
		Info: domain.KPIMetricInfo{
			Key: "clean_time_code_review", Label: "Clean Time In Review", Unit: "hours",
			Description: "Median hours held in code_review, counting only tasks completed without a revision",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Columns: columnsCodeReview, MinSample: minCleanSample,
		Resolve: resolveCleanTime(columnsCodeReview, minCleanSample),
	},
	"clean_time_in_qa": {
		Info: domain.KPIMetricInfo{
			Key: "clean_time_in_qa", Label: "Clean Time In QA", Unit: "hours",
			Description: "Median hours from QA hand-off to QA sign-off, counting only tasks completed without a revision",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Columns: columnsQA, MinSample: minCleanSample,
		Resolve: resolveCleanTime(columnsQA, minCleanSample),
	},
	"tool_error_rate": {
		Info: domain.KPIMetricInfo{
			Key: "tool_error_rate", Label: "Tool Error Rate", Unit: "%",
			Description: "Percentage of the agent's tool calls that came back as errors (0-100)",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		MinSample: minToolCallSample,
		Resolve:   resolveToolErrorRate,
	},
	"review_escapes": {
		Info: domain.KPIMetricInfo{
			Key: "review_escapes", Label: "Review Escapes", Unit: "items",
			Description: "Approvals a human later rejected at the same review gate",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: countScoreEvents(domain.ScoreEventReviewEscape),
	},
}

func ListMetrics() []domain.KPIMetricInfo {
	out := make([]domain.KPIMetricInfo, 0, len(metricRegistry))
	for _, key := range []string{
		"tasks_completed", "revisions_received", "uat_failures", "failed_runs",
		"gate_rejected_runs",
		"bugs_assigned", "first_pass_rate",
		"clean_time_in_progress", "clean_time_code_review", "clean_time_in_qa",
		"review_escapes", "tool_error_rate",
	} {
		out = append(out, metricRegistry[key].Info)
	}
	return out
}

func MetricByKey(key string) (MetricDef, error) {
	def, ok := metricRegistry[key]
	if !ok {
		return MetricDef{}, fmt.Errorf("metric %q is not trackable by the system", key)
	}
	return def, nil
}

func countDistinctTaskEvents(eventTypes ...string) MetricResolver {
	return func(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
		events, err := deps.Perf.EventsInWindow(ctx, agentID, from, to)
		if err != nil {
			return 0, err
		}
		matches := func(eventType string) bool {
			for _, t := range eventTypes {
				if eventType == t {
					return true
				}
			}
			return false
		}
		seen := make(map[uuid.UUID]bool)
		count := 0
		for _, e := range events {
			if !matches(e.EventType) {
				continue
			}
			if e.TaskID == nil {
				count++
				continue
			}
			if !seen[*e.TaskID] {
				seen[*e.TaskID] = true
				count++
			}
		}
		return float64(count), nil
	}
}

func countScoreEvents(eventTypes ...string) MetricResolver {
	return func(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
		events, err := deps.Perf.EventsInWindow(ctx, agentID, from, to)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, e := range events {
			for _, t := range eventTypes {
				if e.EventType == t {
					count++
					break
				}
			}
		}
		return float64(count), nil
	}
}

var (
	columnsInProgress = []string{"in_progress"}
	columnsCodeReview = []string{"code_review"}

	columnsQA = []string{"ready_for_qa", "in_qa"}
)

const minCleanSample = 3

const minToolCallSample = 20

const minFirstPassSample = 3

func resolveToolErrorRate(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
	if deps.Runs == nil {
		return 0, fmt.Errorf("task run store unavailable")
	}
	runs, err := deps.Runs.ListRecent(ctx, 500)
	if err != nil {
		return 0, err
	}
	var calls, errorCalls int
	for _, r := range runs {
		if r.AgentID != agentID || r.ToolCalls == 0 {
			continue
		}
		if r.CreatedAt.Before(from) || !r.CreatedAt.Before(to) {
			continue
		}
		calls += r.ToolCalls
		errorCalls += r.ToolErrors
	}
	if calls < minToolCallSample {
		return 0, ErrInsufficientData
	}
	return float64(errorCalls) / float64(calls) * 100.0, nil
}

func resolveCleanTime(columns []string, minSample int) MetricResolver {
	return func(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
		if deps.Spans == nil {
			return 0, fmt.Errorf("span store unavailable")
		}
		hours, err := deps.Spans.CleanTaskHours(ctx, agentID, columns, from, to)
		if err != nil {
			return 0, err
		}
		if len(hours) < minSample {
			return 0, ErrInsufficientData
		}
		return median(hours), nil
	}
}

func median(values []float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func resolveFailedRuns(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
	if deps.Runs == nil {
		return 0, fmt.Errorf("task run store unavailable")
	}
	runs, err := deps.Runs.ListRecent(ctx, 500)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range runs {
		if r.AgentID == agentID && r.Status == domain.TaskAgentRunStatusFailed &&
			!r.CreatedAt.Before(from) && r.CreatedAt.Before(to) {
			count++
		}
	}
	return float64(count), nil
}

func resolveBugsAssigned(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
	if deps.Tasks == nil {
		return 0, fmt.Errorf("board task store unavailable")
	}
	tasks, err := deps.Tasks.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, t := range tasks {
		if !isDefectType(ctx, deps, t.TaskType) {
			continue
		}
		if t.AssigneeAgentID != nil && *t.AssigneeAgentID == agentID &&
			!t.CreatedAt.Before(from) && t.CreatedAt.Before(to) {
			count++
		}
	}
	return float64(count), nil
}

func isDefectType(ctx context.Context, deps MetricDeps, t domain.TaskType) bool {
	if deps.Workflows == nil {
		return false
	}
	wf, err := deps.Workflows.Workflow(ctx, t)
	return err == nil && wf.Type.IsDefect
}

func resolveFirstPassRate(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
	events, err := deps.Perf.EventsInWindow(ctx, agentID, from, to)
	if err != nil {
		return 0, err
	}
	completed := make(map[string]bool)
	revised := make(map[string]bool)
	for _, e := range events {
		if e.TaskID == nil {
			continue
		}
		key := e.TaskID.String()
		switch e.EventType {
		case domain.ScoreEventTaskCompleted, domain.ScoreEventTaskReleased:
			completed[key] = true
		case domain.ScoreEventRevisionRequested, domain.ScoreEventHumanUATFailed:
			revised[key] = true
		}
	}
	if len(completed) < minFirstPassSample {
		return 0, ErrInsufficientData
	}
	clean := 0
	for taskID := range completed {
		if !revised[taskID] {
			clean++
		}
	}
	return float64(clean) / float64(len(completed)) * 100.0, nil
}

func resolveGateRejectedRuns(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error) {
	if deps.Runs == nil {
		return 0, fmt.Errorf("task run store unavailable")
	}
	runs, err := deps.Runs.ListRecent(ctx, 500)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range runs {
		if r.AgentID == agentID && r.Status == domain.TaskAgentRunStatusFailed &&
			!r.CreatedAt.Before(from) && r.CreatedAt.Before(to) &&
			domain.IsRunGateRejection(r.Summary) {
			count++
		}
	}
	return float64(count), nil
}
