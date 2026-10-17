package datasource

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
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
// whole inventory, not the filtered page; where the source's own listing
// stopped short (Truncated) and its kind can look one metric up
// (port.MetricDiscoverer), each detect metric missing from it is looked up.
func (s *Service) Discover(ctx context.Context, id uuid.UUID, match string, limit int) (domain.DataSourceDiscovery, error) {
	return s.Batch().Discover(ctx, id, match, limit)
}

func discovery(inv domain.Inventory, recipes []domain.Recipe, present map[string]bool, kind domain.DataSourceKind, match string, limit int) domain.DataSourceDiscovery {
	matches := []domain.RecipeMatch{}
	for _, r := range recipes {
		if r.SourceKind == kind && r.Detect.Metric != "" && present[r.Detect.Metric] {
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
	return domain.DataSourceDiscovery{Metrics: metrics, Truncated: truncated, Recipes: matches}
}

// Query runs a range query on a source whose kind implements MetricsQuerier.
// A zero Step picks one giving about autoPoints points. A caller running
// several queries for one request uses a Batch instead.
func (s *Service) Query(ctx context.Context, id uuid.UUID, q domain.SeriesQuery) (domain.SeriesResult, error) {
	return s.Batch().Query(ctx, id, q)
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
