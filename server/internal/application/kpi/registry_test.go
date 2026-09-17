package kpi_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

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
	// Median, not mean: one monster task must not define the week.
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
	// Two data points cannot be trusted, and one fast task must not buy a
	// full score.
	_, err := resolveMetric(t, "clean_time_in_progress", []float64{1, 2})
	require.ErrorIs(t, err, kpi.ErrInsufficientData)
}

func TestCleanTimeWithNoDataIsInsufficientNotZero(t *testing.T) {
	// A zero here would score 1.0 on a lower-better metric: doing nothing
	// would look like maximum speed.
	_, err := resolveMetric(t, "clean_time_pm_uat", nil)
	require.ErrorIs(t, err, kpi.ErrInsufficientData)
	require.Equal(t, domain.KPIDirectionLowerBetter, mustInfo(t, "clean_time_pm_uat").Direction)
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

func TestTasksCompletedCountsPMUATCompleted(t *testing.T) {
	def, err := kpi.MetricByKey("tasks_completed")
	require.NoError(t, err)
	agentID := uuid.New()
	deps := kpi.MetricDeps{Perf: &stubPerf{events: []domain.AgentScoreEvent{
		{AgentID: agentID, EventType: domain.ScoreEventPMUATCompleted},
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

func TestAllListedMetricsAreResolvable(t *testing.T) {
	listed := kpi.ListMetrics()
	require.NotEmpty(t, listed)
	for _, info := range listed {
		_, err := kpi.MetricByKey(info.Key)
		require.NoError(t, err, info.Key)
	}
}
