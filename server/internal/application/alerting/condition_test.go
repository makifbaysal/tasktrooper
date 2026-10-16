package alerting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var t0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func f(v float64) *float64 { return &v }

func at(minutes int, v float64) sample {
	return sample{at: t0.Add(time.Duration(minutes) * time.Minute), value: v}
}

func cond(op domain.AlertOp, threshold float64, forSeconds int64) domain.AlertCondition {
	return domain.AlertCondition{Op: op, Threshold: threshold, ForSeconds: forSeconds, WindowSeconds: 300}
}

func TestJudge(t *testing.T) {
	cur, prev := &sample{value: 12}, &sample{value: 8}
	zero := &sample{value: 0}
	tests := []struct {
		name  string
		c     domain.AlertCondition
		cur   *sample
		prev  *sample
		value *float64
		holds bool
	}{
		{"gt above", cond(domain.AlertOpAbove, 10, 0), cur, prev, f(12), true},
		{"gt at the threshold", cond(domain.AlertOpAbove, 12, 0), cur, prev, f(12), false},
		{"lt below", cond(domain.AlertOpBelow, 99.5, 0), &sample{value: 99}, nil, f(99), true},
		{"no value is no data", cond(domain.AlertOpAbove, 10, 0), nil, nil, nil, false},
		{"rising above the floor", cond(domain.AlertOpRising, 10, 0), cur, prev, f(12), true},
		{"rising under the floor", cond(domain.AlertOpRising, 20, 0), cur, prev, f(12), false},
		{"falling is not rising", cond(domain.AlertOpRising, 0, 0), prev, cur, f(8), false},
		{"rising without an earlier value", cond(domain.AlertOpRising, 0, 0), cur, nil, f(12), false},
		{"change up 50%", cond(domain.AlertOpChangePctAbove, 40, 0), cur, prev, f(50), true},
		{"change down 33%", cond(domain.AlertOpChangePctBelow, -30, 0), prev, cur, f(-100.0 / 3), true},
		{"change against zero is no data", cond(domain.AlertOpChangePctAbove, 10, 0), cur, zero, nil, false},
		{"change without an earlier value is no data", cond(domain.AlertOpChangePctAbove, 10, 0), cur, nil, nil, false},
		{"no_data holds without a value", cond(domain.AlertOpNoData, 0, 0), nil, nil, nil, true},
		{"no_data with a value", cond(domain.AlertOpNoData, 0, 0), cur, nil, f(12), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := judge(tt.c, tt.cur, tt.prev)
			assert.Equal(t, tt.holds, v.holds)
			if tt.value == nil {
				assert.Nil(t, v.value)
				return
			}
			require.NotNil(t, v.value)
			assert.InDelta(t, *tt.value, *v.value, 1e-9)
		})
	}
}

func TestAdvance(t *testing.T) {
	since := t0
	tests := []struct {
		name      string
		state     domain.AlertState
		c         domain.AlertCondition
		v         verdict
		now       time.Time
		want      domain.AlertState
		wantSince time.Time
	}{
		{"ok to pending", domain.AlertStateOK, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(2), holds: true}, t0.Add(time.Minute), domain.AlertStatePending, t0.Add(time.Minute)},
		{"pending until for has passed", domain.AlertStatePending, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(2), holds: true}, t0.Add(4 * time.Minute), domain.AlertStatePending, since},
		{"pending to firing once for has passed", domain.AlertStatePending, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(2), holds: true}, t0.Add(5 * time.Minute), domain.AlertStateFiring, t0.Add(5 * time.Minute)},
		{"no for fires at once", domain.AlertStateOK, cond(domain.AlertOpAbove, 1, 0), verdict{value: f(2), holds: true}, t0.Add(time.Minute), domain.AlertStateFiring, t0.Add(time.Minute)},
		{"firing stays firing", domain.AlertStateFiring, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(2), holds: true}, t0.Add(time.Hour), domain.AlertStateFiring, since},
		{"firing resolves to ok", domain.AlertStateFiring, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(0)}, t0.Add(time.Hour), domain.AlertStateOK, t0.Add(time.Hour)},
		{"pending falls back to ok", domain.AlertStatePending, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(0)}, t0.Add(time.Minute), domain.AlertStateOK, t0.Add(time.Minute)},
		{"ok keeps its since", domain.AlertStateOK, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(0)}, t0.Add(time.Minute), domain.AlertStateOK, since},
		{"no value is no_data", domain.AlertStateFiring, cond(domain.AlertOpAbove, 1, 300), verdict{}, t0.Add(time.Minute), domain.AlertStateNoData, t0.Add(time.Minute)},
		{"no_data to pending when it holds again", domain.AlertStateNoData, cond(domain.AlertOpAbove, 1, 300), verdict{value: f(2), holds: true}, t0.Add(time.Minute), domain.AlertStatePending, t0.Add(time.Minute)},
		{"a no_data rule goes pending without a value", domain.AlertStateOK, cond(domain.AlertOpNoData, 0, 300), verdict{holds: true}, t0.Add(time.Minute), domain.AlertStatePending, t0.Add(time.Minute)},
		{"a no_data rule fires after for", domain.AlertStatePending, cond(domain.AlertOpNoData, 0, 300), verdict{holds: true}, t0.Add(5 * time.Minute), domain.AlertStateFiring, t0.Add(5 * time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, gotSince := advance(tt.state, since, tt.c, tt.v, tt.now)
			assert.Equal(t, tt.want, state)
			assert.Equal(t, tt.wantSince, gotSince)
		})
	}
}

func TestPickAllowsAPeriodLabelledByItsStart(t *testing.T) {
	samples := []sample{at(0, 1), at(5, 2), at(10, 3)}
	step := 5 * time.Minute

	cur, prev := pick(samples, t0.Add(10*time.Minute), step)
	require.NotNil(t, cur)
	require.NotNil(t, prev)
	assert.Equal(t, 3.0, cur.value)
	assert.Equal(t, 2.0, prev.value)

	cur, prev = pick(samples, t0.Add(15*time.Minute), step)
	require.NotNil(t, cur)
	assert.Equal(t, 3.0, cur.value)
	assert.Equal(t, 2.0, prev.value)

	cur, _ = pick(samples, t0.Add(18*time.Minute), step)
	assert.Nil(t, cur)
}

func TestPickHasNoEarlierValueAcrossAGap(t *testing.T) {
	cur, prev := pick([]sample{at(0, 1), at(20, 2)}, t0.Add(20*time.Minute), 5*time.Minute)
	require.NotNil(t, cur)
	assert.Nil(t, prev)
}

func TestSamplesOfKeepsTheWorstSeriesAtEachInstant(t *testing.T) {
	series := []domain.Series{
		{Points: []domain.SeriesPoint{{Time: t0, Value: f(1)}, {Time: t0.Add(time.Minute), Value: f(9)}}},
		{Points: []domain.SeriesPoint{{Time: t0, Value: f(4)}, {Time: t0.Add(time.Minute)}}},
	}
	high := samplesOf(series, domain.AlertOpAbove)
	require.Len(t, high, 2)
	assert.Equal(t, []float64{4, 9}, []float64{high[0].value, high[1].value})

	low := samplesOf(series, domain.AlertOpBelow)
	assert.Equal(t, []float64{1, 9}, []float64{low[0].value, low[1].value})
}

func TestReplayFindsEachFiringWithItsPeak(t *testing.T) {
	step := 5 * time.Minute
	values := []float64{1, 1, 5, 7, 6, 1, 1, 8, 9}
	samples := make([]sample, len(values))
	for i, v := range values {
		samples[i] = at(5*i, v)
	}
	c := domain.AlertCondition{Op: domain.AlertOpAbove, Threshold: 4, ForSeconds: 300, WindowSeconds: 300}

	fires, change := replay(c, samples, t0, t0.Add(40*time.Minute), step)

	assert.Nil(t, change)
	require.Len(t, fires, 2)
	assert.Equal(t, t0.Add(15*time.Minute), fires[0].Start)
	require.NotNil(t, fires[0].End)
	assert.Equal(t, t0.Add(25*time.Minute), *fires[0].End)
	assert.Equal(t, 7.0, *fires[0].Peak)
	assert.Equal(t, t0.Add(40*time.Minute), fires[1].Start)
	assert.Nil(t, fires[1].End)
	assert.Equal(t, 9.0, *fires[1].Peak)
}

func TestReplayAnswersTheChangeItJudged(t *testing.T) {
	samples := []sample{at(0, 10), at(5, 10), at(10, 20)}
	c := domain.AlertCondition{Op: domain.AlertOpChangePctAbove, Threshold: 50, WindowSeconds: 300}

	fires, change := replay(c, samples, t0, t0.Add(10*time.Minute), 5*time.Minute)

	require.Len(t, change, 2)
	assert.Equal(t, 0.0, *change[0].Value)
	assert.Equal(t, 100.0, *change[1].Value)
	require.Len(t, fires, 1)
	assert.Equal(t, t0.Add(10*time.Minute), fires[0].Start)
}

func TestBacktestStepIsTheWindowUntilThePointCap(t *testing.T) {
	assert.Equal(t, 5*time.Minute, backtestStep(5*time.Minute, 24*time.Hour))
	step := backtestStep(time.Minute, 14*24*time.Hour)
	assert.Equal(t, 11*time.Minute, step)
	assert.LessOrEqual(t, int(14*24*time.Hour/step), maxBacktestPoints)
}

func TestRoundSignificant(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{2.2542, 2.25}, {99.5, 99.5}, {1000, 1000}, {1234.5, 1230}, {0.012345, 0.0123}, {0, 0},
	}
	for _, tt := range tests {
		assert.InDelta(t, tt.want, roundSignificant(tt.in, 3), 1e-9, "%v", tt.in)
	}
}

func TestSummarizeHistoryNeedsTwoHours(t *testing.T) {
	assert.False(t, summarizeHistory(make([]sample, minHistory-1)).ok)

	var samples []sample
	for i := 1; i <= 100; i++ {
		samples = append(samples, at(i, float64(i)))
	}
	h := summarizeHistory(samples)
	assert.True(t, h.ok)
	assert.Equal(t, 50.0, h.median)
	assert.Equal(t, 99.0, h.p99)
}

func TestIncidentEnvUsesTheLegacyLabels(t *testing.T) {
	assert.Equal(t, "prod", incidentEnv(domain.EnvironmentProduction))
	assert.Equal(t, "stage", incidentEnv(domain.EnvironmentStaging))
	assert.Equal(t, "local", incidentEnv(domain.EnvironmentDevelopment))
	assert.Equal(t, "preview", incidentEnv(domain.EnvironmentPreview))
}

func TestFormatValue(t *testing.T) {
	assert.Equal(t, "no data", formatValue(nil, "ms"))
	assert.Equal(t, "2513 ms", formatValue(f(2512.7), "ms"))
	assert.Equal(t, "5.27%", formatValue(f(5.2666), "%"))
	assert.Equal(t, "0.0123", formatValue(f(0.012345), ""))
}
