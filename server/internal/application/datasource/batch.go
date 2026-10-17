package datasource

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// maxMetricLookups bounds the per-metric checks one truncated discovery
// makes.
const maxMetricLookups = 32

// Batch is one request's reads across sources: each source's row and
// decrypted secrets are loaded once however many queries it answers, and the
// masks an error or a warning needs are worked out at most once per source.
// Safe for concurrent use; meant to live no longer than the request.
type Batch struct {
	svc   *Service
	mu    sync.Mutex
	known map[uuid.UUID]domain.DataSource
	loads map[uuid.UUID]*loaded
}

type loaded struct {
	mu      sync.Mutex
	ok      bool
	ds      domain.DataSource
	adapter port.DataSourceKindAdapter
	secrets map[string]string
	masks   map[string]string
}

// Batch starts one. known are rows the caller already read this request (a
// List); they are not read again.
func (s *Service) Batch(known ...domain.DataSource) *Batch {
	b := &Batch{svc: s, known: make(map[uuid.UUID]domain.DataSource, len(known)), loads: map[uuid.UUID]*loaded{}}
	for _, ds := range known {
		b.known[ds.ID] = ds
	}
	return b
}

// load keeps only a success: a load that failed (its request's deadline, say)
// is tried again by the next query of the source.
func (b *Batch) load(ctx context.Context, id uuid.UUID) (*loaded, error) {
	b.mu.Lock()
	l := b.loads[id]
	if l == nil {
		l = &loaded{}
		b.loads[id] = l
	}
	ds, known := b.known[id]
	b.mu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ok {
		return l, nil
	}
	if !known {
		var err error
		if ds, err = b.svc.store.GetDataSource(ctx, id); err != nil {
			return nil, err
		}
	}
	adapter, ok := b.svc.kinds[ds.Kind]
	if !ok {
		return nil, fmt.Errorf("kind %q: %w", ds.Kind, ErrUnsupported)
	}
	secrets, err := b.svc.store.DataSourceSecrets(ctx, id)
	if err != nil {
		return nil, err
	}
	l.ds, l.adapter, l.secrets, l.ok = ds, adapter, secrets, true
	return l, nil
}

// maskSet is kept only when worked out in full: masks skips a cloud account
// it cannot read, which a cancelled context also causes.
func (l *loaded) maskSet(ctx context.Context, s *Service) map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.masks != nil {
		return l.masks
	}
	masks := s.masks(ctx, l.adapter.Schema(), l.ds.Config, l.secrets)
	if ctx.Err() == nil {
		l.masks = masks
	}
	return masks
}

// Query is Service.Query within the batch.
func (b *Batch) Query(ctx context.Context, id uuid.UUID, q domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := normalizeSeriesQuery(q)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	l, err := b.load(ctx, id)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	querier, ok := l.adapter.(port.MetricsQuerier)
	if !ok {
		return domain.SeriesResult{}, fmt.Errorf("kind %q cannot query metrics: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	res, err := querier.Series(uctx, l.ds, l.secrets, q)
	if err != nil {
		return domain.SeriesResult{}, upstream(err, l.maskSet(ctx, b.svc))
	}
	res.Warnings = nonNilSlice(res.Warnings)
	if len(res.Series) > maxSeries {
		res.Warnings = append(res.Warnings, fmt.Sprintf("showing the first %d of %d series", maxSeries, len(res.Series)))
		res.Series = res.Series[:maxSeries]
	}
	res.Series = nonNilSlice(res.Series)
	for i := range res.Series {
		if res.Series[i].Labels == nil {
			res.Series[i].Labels = map[string]string{}
		}
		res.Series[i].Points = nonNilSlice(res.Series[i].Points)
	}
	if len(res.Warnings) > 0 {
		masks := l.maskSet(ctx, b.svc)
		for i, w := range res.Warnings {
			res.Warnings[i] = redact(w, masks)
		}
	}
	return res, nil
}

// Discover is Service.Discover within the batch.
func (b *Batch) Discover(ctx context.Context, id uuid.UUID, match string, limit int) (domain.DataSourceDiscovery, error) {
	if limit <= 0 {
		limit = DefaultDiscoverLimit
	}
	limit = min(limit, MaxDiscoverLimit)
	l, err := b.load(ctx, id)
	if err != nil {
		return domain.DataSourceDiscovery{}, err
	}
	discoverer, ok := l.adapter.(port.Discoverer)
	if !ok {
		return domain.DataSourceDiscovery{}, fmt.Errorf("kind %q cannot list metrics: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	inv, err := discoverer.Discover(uctx, l.ds, l.secrets)
	if err != nil {
		return domain.DataSourceDiscovery{}, upstream(err, l.maskSet(ctx, b.svc))
	}
	recipes, err := b.svc.Recipes(ctx)
	if err != nil {
		return domain.DataSourceDiscovery{}, err
	}
	present := make(map[string]bool, len(inv.Metrics))
	for _, m := range inv.Metrics {
		present[m] = true
	}
	if finder, ok := l.adapter.(port.MetricDiscoverer); ok && inv.Truncated {
		lookUpMissing(uctx, finder, l, recipes, present)
	}
	return discovery(inv, recipes, present, l.ds.Kind, match, limit), nil
}

// lookUpMissing asks a source whose inventory stopped short about each detect
// metric of its kind the inventory lacks, marking the ones it holds. A check
// that fails only leaves that recipe undetected.
func lookUpMissing(ctx context.Context, finder port.MetricDiscoverer, l *loaded, recipes []domain.Recipe, present map[string]bool) {
	asked := map[string]bool{}
	for _, r := range recipes {
		m := r.Detect.Metric
		if r.SourceKind != l.ds.Kind || m == "" || present[m] || asked[m] {
			continue
		}
		if len(asked) == maxMetricLookups || ctx.Err() != nil {
			return
		}
		asked[m] = true
		found, err := finder.DiscoverMetric(ctx, l.ds, l.secrets, m)
		if err != nil {
			log.Debug().Err(err).Str("data_source", l.ds.ID.String()).Str("metric", m).Msg("data sources: checking a detect metric failed")
			continue
		}
		if found {
			present[m] = true
		}
	}
}
