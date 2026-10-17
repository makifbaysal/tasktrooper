package alerting

import (
	"context"
	"errors"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	DefaultBacktestDays = 7
	MaxBacktestDays     = 14
	// maxBacktestPoints caps one replay: past it the step widens from the
	// rule's window to the minute that fits.
	maxBacktestPoints = 2000

	changeSeriesName = "change_pct"
	querySeriesName  = "query"
)

// BacktestRequest is a rule without an id plus how many days back to
// replay it (1–14, zero means 7).
type BacktestRequest struct {
	RuleRequest
	Days int `json:"days"`
}

// Backtest replays a rule's condition over the last days at its window's
// resolution, exactly as the evaluator would have judged each step: fires
// are the spans it would have been firing (End nil when still firing now),
// each with its worst value. The read failing is the answer's error.
func (s *Service) Backtest(ctx context.Context, req BacktestRequest) (domain.AlertBacktest, error) {
	spec, err := s.specFrom(ctx, defaultSpec(), req.RuleRequest, false)
	fe := FieldErrors{}
	if err != nil && !errors.As(err, &fe) {
		return domain.AlertBacktest{}, err
	}
	days := req.Days
	if days == 0 {
		days = DefaultBacktestDays
	}
	if days < 1 || days > MaxBacktestDays {
		fe["days"] = datasource.FieldInvalid
	}
	if len(fe) > 0 {
		return domain.AlertBacktest{}, fe
	}

	to := s.now().Truncate(time.Minute)
	span := time.Duration(days) * 24 * time.Hour
	step := backtestStep(spec.Condition.Window(), span)
	from := to.Add(-span)
	samples, effective, err := s.read(ctx, spec, from.Add(-step), to, step)
	if err != nil {
		return domain.AlertBacktest{}, err
	}
	fires, change := replay(spec.Condition, samples, from, to, step, effective)

	name := spec.Signal
	if name == "" {
		name = querySeriesName
	}
	points := []domain.SeriesPoint{}
	for _, smp := range samples {
		if !smp.at.Before(from) {
			points = append(points, domain.FinitePoint(smp.at, smp.value))
		}
	}
	out := domain.AlertBacktest{
		Fires:       fires,
		Series:      []domain.AlertSeries{{Name: name, Points: points}},
		StepSeconds: int64(step / time.Second),
	}
	if change != nil {
		out.Series = append(out.Series, domain.AlertSeries{Name: changeSeriesName, Points: change})
	}
	return out, nil
}

func backtestStep(window, span time.Duration) time.Duration {
	step := window
	if span/step > maxBacktestPoints {
		step = (span / maxBacktestPoints).Truncate(time.Minute) + time.Minute
	}
	return step
}

// replay runs the state machine over every step of (from, to], each step an
// evaluation, picking samples at their effective step; for a change op it
// also answers the percent change it judged at each step.
func replay(c domain.AlertCondition, samples []sample, from, to time.Time, step, effective time.Duration) ([]domain.AlertFire, []domain.SeriesPoint) {
	fires := []domain.AlertFire{}
	var change []domain.SeriesPoint
	isChange := c.Op == domain.AlertOpChangePctAbove || c.Op == domain.AlertOpChangePctBelow
	if isChange {
		change = []domain.SeriesPoint{}
	}
	st := standing{state: domain.AlertStateOK, since: from}
	var open *domain.AlertFire
	for at := from.Add(step); !at.After(to); at = at.Add(step) {
		cur, prev := pick(samples, at, max(step, effective))
		v := judge(c, cur, prev)
		if isChange {
			change = append(change, domain.SeriesPoint{Time: at, Value: v.value})
		}
		next := advance(st, c, v, at)
		switch {
		case next.state == domain.AlertStateFiring && st.state != domain.AlertStateFiring:
			open = &domain.AlertFire{Start: at}
		case next.state != domain.AlertStateFiring && st.state == domain.AlertStateFiring:
			end := at
			open.End = &end
			fires = append(fires, *open)
			open = nil
		}
		if open != nil && v.value != nil && (open.Peak == nil || worse(c.Op, *v.value, *open.Peak)) {
			peak := *v.value
			open.Peak = &peak
		}
		st = next
	}
	if open != nil {
		fires = append(fires, *open)
	}
	return fires, change
}
