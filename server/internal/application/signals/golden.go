package signals

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	MaxGoldenComponents = 200
	MaxGoldenWindow     = 30 * 24 * time.Hour

	sparkPoints     = 30
	goldenCacheTTL  = time.Minute
	goldenCacheSize = 64
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
// read answers nulls; answers are kept for a minute per window and
// component set, and dropped whenever a binding changes.
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

	key := goldenKey(q.Environment, from, to, q.ComponentIDs)
	if board, ok := s.goldenCache.get(key, s.now()); ok {
		return board, nil
	}

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
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.GoldenBoard{}, err
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
			gc.Signals[si] = domain.GoldenSignal{Signal: id, Unit: sig.Unit, Spark: []domain.SeriesPoint{}}
			b, ok := bound[c.ID][id]
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

	results := fanOut(ctx, len(jobs), s.goldenConcurrency, s.queryTimeout, func(ctx context.Context, i int) ([]domain.Series, error) {
		j := jobs[i]
		series, _, err := s.read(ctx, j.binding, j.quantile, from.Add(-span), to, step)
		return series, err
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
	s.goldenCache.put(key, board, s.now())
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

type goldenCache struct {
	mu      sync.Mutex
	entries map[string]goldenEntry
}

type goldenEntry struct {
	at    time.Time
	board domain.GoldenBoard
}

func newGoldenCache() *goldenCache { return &goldenCache{entries: map[string]goldenEntry{}} }

func (c *goldenCache) get(key string, now time.Time) (domain.GoldenBoard, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || now.Sub(e.at) >= goldenCacheTTL {
		return domain.GoldenBoard{}, false
	}
	return e.board, true
}

func (c *goldenCache) put(key string, board domain.GoldenBoard, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.Sub(e.at) >= goldenCacheTTL {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= goldenCacheSize {
		oldest := ""
		for k, e := range c.entries {
			if oldest == "" || e.at.Before(c.entries[oldest].at) {
				oldest = k
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = goldenEntry{at: now, board: board}
}

func (c *goldenCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}
