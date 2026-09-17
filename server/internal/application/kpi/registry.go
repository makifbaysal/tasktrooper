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

// SpanHoursReader is the slice of TaskColumnSpanStore the time metrics read.
type SpanHoursReader interface {
	CleanTaskHours(ctx context.Context, agentID uuid.UUID, columns []string, from, to time.Time) ([]float64, error)
}

// MetricDeps are the data sources metric resolvers may read from.
type MetricDeps struct {
	Perf  port.AgentPerformanceStore
	Runs  port.TaskAgentRunStore
	Tasks port.BoardTaskStore
	Spans SpanHoursReader
}

// ErrInsufficientData means the period holds too few measurements to score.
// The KPI is then left unmeasured rather than scored, because a zero on a
// lower-better metric reads as a perfect result.
var ErrInsufficientData = errors.New("insufficient data for metric")

type MetricResolver func(ctx context.Context, deps MetricDeps, agentID uuid.UUID, from, to time.Time) (float64, error)

type MetricDef struct {
	Info domain.KPIMetricInfo
	// Columns are the board columns a time metric measures. Empty for
	// non-time metrics.
	Columns []string
	// MinSample is the fewest measurements required to publish a result.
	MinSample int
	Resolve   MetricResolver
}

// metricRegistry is the single source of truth for trackable KPI metrics.
// A KPI can only be created for a key present here; adding a new trackable
// metric means adding one entry.
var metricRegistry = map[string]MetricDef{
	"tasks_completed": {
		Info: domain.KPIMetricInfo{
			Key: "tasks_completed", Label: "Tasks Completed", Unit: "tasks",
			Description: "Number of tasks moved into the done/released column",
			Direction:   domain.KPIDirectionHigherBetter,
		},
		Resolve: countScoreEvents(
			domain.ScoreEventTaskCompleted, domain.ScoreEventTaskReleased,
			domain.ScoreEventQATaskTested, domain.ScoreEventPMUATCompleted,
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
			Description: "Number of items rejected by PM or human UAT",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: countScoreEvents(domain.ScoreEventPMUATFailed, domain.ScoreEventHumanUATFailed),
	},
	"failed_runs": {
		Info: domain.KPIMetricInfo{
			Key: "failed_runs", Label: "Failed Runs", Unit: "runs",
			Description: "Agent runs that ended in the failed state",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Resolve: resolveFailedRuns,
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
		Resolve: resolveFirstPassRate,
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
	"clean_time_pm_uat": {
		Info: domain.KPIMetricInfo{
			Key: "clean_time_pm_uat", Label: "Clean Time In PM UAT", Unit: "hours",
			Description: "Median hours held in pm_uat, counting only tasks completed without a revision",
			Direction:   domain.KPIDirectionLowerBetter,
		},
		Columns: columnsPMUAT, MinSample: minCleanSample,
		Resolve: resolveCleanTime(columnsPMUAT, minCleanSample),
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
		"bugs_assigned", "first_pass_rate",
		"clean_time_in_progress", "clean_time_code_review", "clean_time_in_qa",
		"clean_time_pm_uat", "review_escapes", "tool_error_rate",
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

// Column sets and the sample floor live outside the registry so a resolver can
// close over them: reading them back out of metricRegistry would make the map's
// initialisation depend on itself.
var (
	columnsInProgress = []string{"in_progress"}
	columnsCodeReview = []string{"code_review"}
	// The wait before QA picks the task up is QA's time too, not nobody's.
	columnsQA    = []string{"ready_for_qa", "in_qa"}
	columnsPMUAT = []string{"pm_uat"}
)

// minCleanSample is the fewest clean tasks a time metric will score on. One
// quick task must not buy a full mark, and a period with nothing in it must
// stay unmeasured rather than measure zero.
const minCleanSample = 3

// minToolCallSample is the fewest tool calls tool_error_rate will score on. A
// period holding one run that made two calls and failed one of them is not a
// 50% error rate, it is no measurement.
const minToolCallSample = 20

// resolveToolErrorRate is the share of the agent's tool calls that came back as
// errors over the period.
//
// It reads the per-run counters rather than the audit log, because the audit
// entries carry no agent id — a tool call knows what it did, not who asked for
// it. Runs that recorded no tool calls are skipped entirely: they are runs from
// before this was measured, or runs that never got to call anything, and
// counting them as clean would dilute a real problem into nothing.
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

// resolveCleanTime measures only tasks that finished without rework. A rushed
// task that bounced is absent from this sample, so speed cannot be bought with
// quality: hurrying removes the reward instead of increasing it.
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
		if t.TaskType == domain.TaskTypeBug && t.AssigneeAgentID != nil && *t.AssigneeAgentID == agentID &&
			!t.CreatedAt.Before(from) && t.CreatedAt.Before(to) {
			count++
		}
	}
	return float64(count), nil
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
		case domain.ScoreEventRevisionRequested, domain.ScoreEventPMUATFailed, domain.ScoreEventHumanUATFailed:
			revised[key] = true
		}
	}
	if len(completed) == 0 {
		return 0, nil
	}
	clean := 0
	for taskID := range completed {
		if !revised[taskID] {
			clean++
		}
	}
	return float64(clean) / float64(len(completed)) * 100.0, nil
}
