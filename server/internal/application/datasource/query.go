package datasource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	upstreamTimeout = 45 * time.Second

	DefaultDiscoverLimit = 1000
	MaxDiscoverLimit     = 5000

	maxQueryLen = 10000
	maxRange    = 90 * 24 * time.Hour
	// maxPoints is Prometheus' own per-series ceiling for query_range.
	maxPoints   = 11000
	minStep     = time.Second
	minAutoStep = 15 * time.Second
	autoPoints  = 600
	maxSeries   = 500
)

// Discover lists the metric names a source holds, filtered by a
// case-insensitive substring and capped at limit, plus every recipe of the
// source's kind whose detect metric is among them. Detection runs over the
// whole inventory, not the filtered page.
func (s *Service) Discover(ctx context.Context, id uuid.UUID, match string, limit int) (domain.DataSourceDiscovery, error) {
	if limit <= 0 {
		limit = DefaultDiscoverLimit
	}
	limit = min(limit, MaxDiscoverLimit)
	ds, adapter, secrets, err := s.load(ctx, id)
	if err != nil {
		return domain.DataSourceDiscovery{}, err
	}
	discoverer, ok := adapter.(port.Discoverer)
	if !ok {
		return domain.DataSourceDiscovery{}, fmt.Errorf("kind %q cannot list metrics: %w", ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	inv, err := discoverer.Discover(uctx, ds, secrets)
	if err != nil {
		return domain.DataSourceDiscovery{}, upstream(err, s.masks(ctx, adapter.Schema(), ds.Config, secrets))
	}
	recipes, err := s.Recipes(ctx)
	if err != nil {
		return domain.DataSourceDiscovery{}, err
	}
	present := make(map[string]bool, len(inv.Metrics))
	for _, m := range inv.Metrics {
		present[m] = true
	}
	matches := []domain.RecipeMatch{}
	for _, r := range recipes {
		if r.SourceKind == ds.Kind && r.Detect.Metric != "" && present[r.Detect.Metric] {
			matches = append(matches, domain.RecipeMatch{RecipeID: r.ID, MatchedMetric: r.Detect.Metric})
		}
	}

	needle := strings.ToLower(strings.TrimSpace(match))
	metrics := []string{}
	truncated := inv.Truncated
	for _, m := range inv.Metrics {
		if needle != "" && !strings.Contains(strings.ToLower(m), needle) {
			continue
		}
		if len(metrics) == limit {
			truncated = true
			break
		}
		metrics = append(metrics, m)
	}
	return domain.DataSourceDiscovery{Metrics: metrics, Truncated: truncated, Recipes: matches}, nil
}

// Query runs a range query on a source whose kind implements MetricsQuerier.
// A zero Step picks one giving about autoPoints points.
func (s *Service) Query(ctx context.Context, id uuid.UUID, q domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := normalizeSeriesQuery(q)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	ds, adapter, secrets, err := s.load(ctx, id)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	querier, ok := adapter.(port.MetricsQuerier)
	if !ok {
		return domain.SeriesResult{}, fmt.Errorf("kind %q cannot query metrics: %w", ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	res, err := querier.Series(uctx, ds, secrets, q)
	if err != nil {
		return domain.SeriesResult{}, upstream(err, s.masks(ctx, adapter.Schema(), ds.Config, secrets))
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
		masks := s.masks(ctx, adapter.Schema(), ds.Config, secrets)
		for i, w := range res.Warnings {
			res.Warnings[i] = redact(w, masks)
		}
	}
	return res, nil
}

func normalizeSeriesQuery(q domain.SeriesQuery) (domain.SeriesQuery, error) {
	q.Query = strings.TrimSpace(q.Query)
	switch {
	case q.Query == "":
		return q, FieldErrors{"query": FieldRequired}
	case len(q.Query) > maxQueryLen:
		return q, FieldErrors{"query": FieldTooLong}
	case q.From.IsZero():
		return q, FieldErrors{"from": FieldRequired}
	case q.To.IsZero():
		return q, FieldErrors{"to": FieldRequired}
	case !q.To.After(q.From):
		return q, FieldErrors{"to": FieldInvalid}
	case q.To.Sub(q.From) > maxRange:
		return q, FieldErrors{"from": FieldTooLong}
	case q.Step < 0:
		return q, FieldErrors{"step_seconds": FieldInvalid}
	}
	span := q.To.Sub(q.From)
	if q.Step == 0 {
		q.Step = max((span / autoPoints).Round(time.Second), minAutoStep)
	}
	if q.Step < minStep {
		return q, FieldErrors{"step_seconds": FieldInvalid}
	}
	if span/q.Step > maxPoints {
		return q, FieldErrors{"step_seconds": FieldTooLong}
	}
	return q, nil
}
