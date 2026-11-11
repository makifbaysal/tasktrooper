package kpi_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/require"
)

type stubRuns struct {
	port.TaskAgentRunStore
	runs []domain.TaskAgentRun
}

func (s *stubRuns) ListRecent(context.Context, int) ([]domain.TaskAgentRun, error) {
	return s.runs, nil
}

type stubSpans struct{ hours []float64 }

func (s *stubSpans) CleanTaskHours(context.Context, uuid.UUID, []string, time.Time, time.Time) ([]float64, error) {
	return s.hours, nil
}

type stubPerf struct{ events []domain.AgentScoreEvent }

func (s *stubPerf) GetScore(context.Context, uuid.UUID) (domain.AgentPerformanceScore, error) {
	return domain.AgentPerformanceScore{}, nil
}

func (s *stubPerf) ApplyDelta(context.Context, domain.ApplyScoreInput) (domain.AgentPerformanceScore, error) {
	return domain.AgentPerformanceScore{}, nil
}

func (s *stubPerf) RecentEvents(context.Context, uuid.UUID, int) ([]domain.AgentScoreEvent, error) {
	return s.events, nil
}

func (s *stubPerf) EventsInWindow(context.Context, uuid.UUID, time.Time, time.Time) ([]domain.AgentScoreEvent, error) {
	return s.events, nil
}

func (s *stubPerf) HasEventForTask(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}

func resolveMetric(t *testing.T, key string, hours []float64) (float64, error) {
	t.Helper()
	def, err := kpi.MetricByKey(key)
	require.NoError(t, err)
	return def.Resolve(context.Background(), kpi.MetricDeps{Spans: &stubSpans{hours: hours}}, uuid.New(),
		time.Now().Add(-24*time.Hour), time.Now())
}

func mustInfo(t *testing.T, key string) domain.KPIMetricInfo {
	t.Helper()
	def, err := kpi.MetricByKey(key)
	require.NoError(t, err)
	return def.Info
}

func TestCleanTimeReturnsMedianHours(t *testing.T) {

	value, err := resolveMetric(t, "clean_time_in_progress", []float64{1, 2, 60})
	require.NoError(t, err)
	require.InDelta(t, 2.0, value, 0.001)
}

func TestCleanTimeMedianOfEvenSample(t *testing.T) {
	value, err := resolveMetric(t, "clean_time_in_qa", []float64{1, 2, 3, 10})
	require.NoError(t, err)
	require.InDelta(t, 2.5, value, 0.001)
}

func TestCleanTimeBelowMinSampleIsInsufficient(t *testing.T) {

	_, err := resolveMetric(t, "clean_time_in_progress", []float64{1, 2})
	require.ErrorIs(t, err, kpi.ErrInsufficientData)
}

func TestCleanTimeWithNoDataIsInsufficientNotZero(t *testing.T) {

	_, err := resolveMetric(t, "clean_time_in_qa", nil)
	require.ErrorIs(t, err, kpi.ErrInsufficientData)
	require.Equal(t, domain.KPIDirectionLowerBetter, mustInfo(t, "clean_time_in_qa").Direction)
	require.Equal(t, 1.0, domain.KPIAttainment(domain.KPIDirectionLowerBetter, 0, 6, 16),
		"a zero measurement scores full marks, which is why it must never be published")
}

func TestQAStageCoversHandoverAndTesting(t *testing.T) {
	def, err := kpi.MetricByKey("clean_time_in_qa")
	require.NoError(t, err)
	require.Equal(t, []string{"ready_for_qa", "in_qa"}, def.Columns)
}

func TestReviewEscapesIsTracked(t *testing.T) {
	info := mustInfo(t, "review_escapes")
	require.Equal(t, domain.KPIDirectionLowerBetter, info.Direction)
}

func TestTasksCompletedCountsQATaskTested(t *testing.T) {
	def, err := kpi.MetricByKey("tasks_completed")
	require.NoError(t, err)
	agentID := uuid.New()
	deps := kpi.MetricDeps{Perf: &stubPerf{events: []domain.AgentScoreEvent{
		{AgentID: agentID, EventType: domain.ScoreEventQATaskTested},
	}}}
	value, err := def.Resolve(context.Background(), deps, agentID, time.Now().Add(-24*time.Hour), time.Now())
	require.NoError(t, err)
	require.Equal(t, 1.0, value)
}

func TestTasksCompletedForDeveloperIsUnchangedByQAPMEvents(t *testing.T) {
	def, err := kpi.MetricByKey("tasks_completed")
	require.NoError(t, err)
	agentID := uuid.New()
	deps := kpi.MetricDeps{Perf: &stubPerf{events: []domain.AgentScoreEvent{
		{AgentID: agentID, EventType: domain.ScoreEventTaskCompleted},
		{AgentID: agentID, EventType: domain.ScoreEventTaskReleased},
	}}}
	value, err := def.Resolve(context.Background(), deps, agentID, time.Now().Add(-24*time.Hour), time.Now())
	require.NoError(t, err)
	require.Equal(t, 2.0, value)
}

func TestTasksCompletedCountsDistinctTasksNotEvents(t *testing.T) {

	def, err := kpi.MetricByKey("tasks_completed")
	require.NoError(t, err)
	agentID := uuid.New()
	taskA := uuid.New()
	taskB := uuid.New()
	deps := kpi.MetricDeps{Perf: &stubPerf{events: []domain.AgentScoreEvent{
		{AgentID: agentID, TaskID: &taskA, EventType: domain.ScoreEventTaskCompleted},
		{AgentID: agentID, TaskID: &taskA, EventType: domain.ScoreEventTaskReleased},
		{AgentID: agentID, TaskID: &taskB, EventType: domain.ScoreEventTaskCompleted},

		{AgentID: agentID, EventType: domain.ScoreEventQATaskTested},
	}}}
	value, err := def.Resolve(context.Background(), deps, agentID, time.Now().Add(-24*time.Hour), time.Now())
	require.NoError(t, err)
	require.Equal(t, 3.0, value)
}

func TestFirstPassRateBelowMinSampleIsInsufficient(t *testing.T) {
	def, err := kpi.MetricByKey("first_pass_rate")
	require.NoError(t, err)
	agentID := uuid.New()
	taskA, taskB := uuid.New(), uuid.New()

	deps := kpi.MetricDeps{Perf: &stubPerf{events: []domain.AgentScoreEvent{
		{AgentID: agentID, TaskID: &taskA, EventType: domain.ScoreEventTaskCompleted},
		{AgentID: agentID, TaskID: &taskB, EventType: domain.ScoreEventTaskCompleted},
	}}}
	_, err = def.Resolve(context.Background(), deps, agentID, time.Now().Add(-24*time.Hour), time.Now())
	require.ErrorIs(t, err, kpi.ErrInsufficientData)
}

func TestFirstPassRateAtMinSampleScores(t *testing.T) {
	def, err := kpi.MetricByKey("first_pass_rate")
	require.NoError(t, err)
	agentID := uuid.New()
	taskA, taskB, taskC := uuid.New(), uuid.New(), uuid.New()
	deps := kpi.MetricDeps{Perf: &stubPerf{events: []domain.AgentScoreEvent{
		{AgentID: agentID, TaskID: &taskA, EventType: domain.ScoreEventTaskCompleted},
		{AgentID: agentID, TaskID: &taskB, EventType: domain.ScoreEventTaskCompleted},
		{AgentID: agentID, TaskID: &taskC, EventType: domain.ScoreEventTaskCompleted},
		{AgentID: agentID, TaskID: &taskC, EventType: domain.ScoreEventRevisionRequested},
	}}}
	value, err := def.Resolve(context.Background(), deps, agentID, time.Now().Add(-24*time.Hour), time.Now())
	require.NoError(t, err)
	require.InDelta(t, 200.0/3.0, value, 0.01)
}

func TestGateRejectedRuns_CountsOnlyGateRejectionsInWindow(t *testing.T) {
	def, err := kpi.MetricByKey("gate_rejected_runs")
	require.NoError(t, err)
	agentID := uuid.New()
	now := time.Now()
	deps := kpi.MetricDeps{Runs: &stubRuns{runs: []domain.TaskAgentRun{
		{AgentID: agentID, Status: domain.TaskAgentRunStatusFailed, Summary: "Analysis rejected: no repo reads", CreatedAt: now},
		{AgentID: agentID, Status: domain.TaskAgentRunStatusFailed, Summary: "QA round rejected: never ran the product", CreatedAt: now},

		{AgentID: agentID, Status: domain.TaskAgentRunStatusFailed, Summary: "session limit reached", CreatedAt: now},

		{AgentID: agentID, Status: domain.TaskAgentRunStatusFailed, Summary: "Analysis rejected: no repo reads", CreatedAt: now.Add(-48 * time.Hour)},

		{AgentID: uuid.New(), Status: domain.TaskAgentRunStatusFailed, Summary: "Analysis rejected: no repo reads", CreatedAt: now},
	}}}
	value, err := def.Resolve(context.Background(), deps, agentID, now.Add(-24*time.Hour), now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2.0, value)
}

func TestAllListedMetricsAreResolvable(t *testing.T) {
	listed := kpi.ListMetrics()
	require.NotEmpty(t, listed)
	for _, info := range listed {
		_, err := kpi.MetricByKey(info.Key)
		require.NoError(t, err, info.Key)
	}
}
