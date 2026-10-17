package signals

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	MaxGoldenComponents = 200
	MaxGoldenWindow     = 30 * 24 * time.Hour

	sparkPoints = 30
)

// GoldenQuery: zero From/To default as in SeriesQuery; both are rounded down
// to the minute. No ComponentIDs means every component.
type GoldenQuery struct {
	Environment  domain.DeployEnvironment
	From         time.Time
	To           time.Time
	ComponentIDs []uuid.UUID
}

// Golden answers the explorer: for every active component with a confirmed
// binding in the environment (at most MaxGoldenComponents, by name), its
// role's golden signals. One query per bound signal covers this window and
// the one before, so Previous costs nothing extra. A signal that cannot be
// read, or is still unread when the read's budget runs out, answers nulls.
// Answers are kept for a minute per cache scope, window, component set and
// source/recipe version, dropped whenever a binding or a source changes, and
// polls asking the same at once share one read.
func (s *Service) Golden(ctx context.Context, q GoldenQuery) (domain.GoldenBoard, error) {
	if !q.Environment.Valid() {
		return domain.GoldenBoard{}, fmt.Errorf("unknown environment %q: %w", q.Environment, ErrInvalidInput)
	}
	to := q.To
	if to.IsZero() {
		to = s.now()
	}
	from := q.From
	if from.IsZero() {
		from = to.Add(-DefaultWindow)
	}
	from, to = from.UTC().Truncate(time.Minute), to.UTC().Truncate(time.Minute)
	span := to.Sub(from)
	_, _, step, err := s.window(from.Add(-span), to, 0, 2*sparkPoints, 2*MaxGoldenWindow)
	if err != nil {
		return domain.GoldenBoard{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.GoldenBoard{}, err
	}

	key := s.cacheScope(ctx) + "\x00" + goldenKey(q.Environment, from, to, q.ComponentIDs) + "\x00" + l.version()
	if board, ok := s.goldenCache.get(key, s.now()); ok {
		return board, nil
	}
	scope := s.cacheScope(ctx)
	gen := s.goldenCache.generation(scope)
	// The shared read outlives the request that started it (the others wait
	// on it too), so it keeps that request's values but not its cancel; the
	// budget bounds it.
	shared := context.WithoutCancel(ctx)
	ch := s.goldenFlight.DoChan(fmt.Sprintf("%s\x00%d", key, gen), func() (v any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("signals: a golden read panicked")
				err = errPanicked
			}
		}()
		board, err := s.readGolden(shared, l, q, from, to, step)
		if err != nil {
			return nil, err
		}
		s.goldenCache.put(scope, key, board, s.now(), gen)
		return board, nil
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return domain.GoldenBoard{}, r.Err
		}
		return r.Val.(domain.GoldenBoard), nil
	case <-ctx.Done():
		return domain.GoldenBoard{}, ctx.Err()
	}
}

func (s *Service) readGolden(ctx context.Context, l lookup, q GoldenQuery, from, to time.Time, step time.Duration) (domain.GoldenBoard, error) {
	ctx, cancel := context.WithTimeout(ctx, s.goldenBudget)
	defer cancel()
	span := to.Sub(from)
	confirmed, err := s.bindings.ListConfirmedSignalBindings(ctx, q.Environment)
	if err != nil {
		return domain.GoldenBoard{}, err
	}
	bound := map[uuid.UUID]map[string]domain.SignalBinding{}
	for _, b := range confirmed {
		if bound[b.ComponentID] == nil {
			bound[b.ComponentID] = map[string]domain.SignalBinding{}
		}
		bound[b.ComponentID][b.Signal] = b
	}
	all, err := s.components.ListAllComponents(ctx)
	if err != nil {
		return domain.GoldenBoard{}, err
	}
	wanted := map[uuid.UUID]bool{}
	for _, id := range q.ComponentIDs {
		wanted[id] = true
	}
	var comps []domain.Component
	for _, c := range all {
		if c.Status == domain.ComponentStatusActive && len(bound[c.ID]) > 0 && (len(wanted) == 0 || wanted[c.ID]) {
			comps = append(comps, c)
		}
	}
	sort.Slice(comps, func(i, j int) bool {
		a, b := strings.ToLower(comps[i].DisplayName()), strings.ToLower(comps[j].DisplayName())
		if a != b {
			return a < b
		}
		return comps[i].ID.String() < comps[j].ID.String()
	})
	if len(comps) > MaxGoldenComponents {
		comps = comps[:MaxGoldenComponents]
	}

	board := domain.GoldenBoard{GeneratedAt: s.now(), Components: make([]domain.GoldenComponent, len(comps))}
	type job struct {
		component, signal int
		binding           domain.SignalBinding
		quantile          float64
	}
	var jobs []job
	for ci, c := range comps {
		ids := s.golden[c.Role.Get()]
		gc := domain.GoldenComponent{ComponentID: c.ID, Signals: make([]domain.GoldenSignal, len(ids))}
		for si, id := range ids {
			sig := s.signalIndex[id]
			b, ok := bound[c.ID][id]
			gc.Signals[si] = domain.GoldenSignal{
				Signal: id, Unit: sig.Unit, Bound: ok, Live: ok && domain.LiveOnlyKind(l.sources[b.DataSourceID].Kind), Spark: []domain.SeriesPoint{},
			}
			if !ok || sig.Kind != domain.SignalMetric || len(needsParams(l, b)) > 0 {
				continue
			}
			quantile := 0.0
			if sig.Aggregation == domain.AggregationQuantile && takesQuantile(template(l, b)) {
				quantile = headlineQuantile
			}
			jobs = append(jobs, job{component: ci, signal: si, binding: b, quantile: quantile})
		}
		board.Components[ci] = gc
	}

	rd := s.reader(l)
	results := fanOut(ctx, len(jobs), s.goldenConcurrency, s.queryTimeout,
		func(i int) uuid.UUID { return jobs[i].binding.DataSourceID },
		func(ctx context.Context, i int) ([]domain.Series, error) {
			j := jobs[i]
			res, _, err := rd.read(ctx, j.binding, j.quantile, from.Add(-span), to, step)
			return res.Series, err
		})
	for i, r := range results {
		j := jobs[i]
		if r.err != nil {
			log.Debug().Err(r.err).Str("signal", j.binding.Signal).Str("component", j.binding.ComponentID.String()).
				Msg("signals: golden read failed")
			continue
		}
		g := &board.Components[j.component].Signals[j.signal]
		if len(r.value) > 0 {
			g.Spark, g.Value, g.Previous = summarize(r.value[0].Points, from, to, span)
		}
	}
	return board, nil
}

// summarize splits one read of [from-span, to] into the spark of [from, to],
// its last non-null value, and the last non-null value at or before that
// instant one span earlier.
func summarize(points []domain.SeriesPoint, from, to time.Time, span time.Duration) ([]domain.SeriesPoint, *float64, *float64) {
	spark := []domain.SeriesPoint{}
	for _, p := range points {
		if !p.Time.Before(from) && !p.Time.After(to) {
			spark = append(spark, p)
		}
	}
	var value *float64
	anchor := to
	for i := len(spark) - 1; i >= 0; i-- {
		if spark[i].Value != nil {
			value, anchor = spark[i].Value, spark[i].Time
			break
		}
	}
	var previous *float64
	limit := anchor.Add(-span)
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		if p.Time.After(limit) || p.Value == nil {
			continue
		}
		if p.Time.Before(from.Add(-span)) {
			break
		}
		previous = p.Value
		break
	}
	return spark, value, previous
}

func goldenKey(env domain.DeployEnvironment, from, to time.Time, ids []uuid.UUID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id.String())
	}
	slices.Sort(parts)
	parts = slices.Compact(parts)
	return fmt.Sprintf("%s|%d|%d|%s", env, from.Unix(), to.Unix(), strings.Join(parts, ","))
}
