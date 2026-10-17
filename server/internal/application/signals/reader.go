package signals

import (
	"context"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// reader is what one request reads bindings through: the sources and
// recipes it resolved them against, and one data source batch, so a source's
// row and secrets are loaded once however many signals it answers.
type reader struct {
	l     lookup
	batch *datasource.Batch
}

func (s *Service) reader(l lookup) *reader {
	return &reader{l: l, batch: s.sources.Batch(l.order...)}
}

// renderIn expands a binding at a step from the request's lookup, refusing
// what datasource.RenderSignal and RenderRawQuery refuse; quantile is what
// $__quantile becomes, zero for the renderer's default.
func renderIn(l lookup, b domain.SignalBinding, step time.Duration, quantile float64) (string, error) {
	ds, ok := l.sources[b.DataSourceID]
	if !ok {
		return "", fmt.Errorf("data source %s: %w", b.DataSourceID, port.ErrNotFound)
	}
	opts := datasource.RenderOptions{Step: step, Quantile: quantile}
	if b.Query != "" {
		return datasource.RenderKindQuery(ds.Kind, b.Query, b.Params, opts)
	}
	r, ok := l.recipes[b.RecipeID]
	switch {
	case !ok:
		return "", fmt.Errorf("unknown recipe %q: %w", b.RecipeID, datasource.ErrInvalidInput)
	case r.SourceKind != ds.Kind:
		return "", fmt.Errorf("recipe %q is for %s sources, not %s: %w", b.RecipeID, r.SourceKind, ds.Kind, datasource.ErrInvalidInput)
	}
	query, ok := r.Signals[b.Signal]
	if !ok {
		return "", fmt.Errorf("recipe %q has no query for %s: %w", b.RecipeID, b.Signal, datasource.ErrInvalidInput)
	}
	return datasource.RenderKindQuery(ds.Kind, query, r.WithDefaults(b.Params), opts)
}

func (rd *reader) read(ctx context.Context, b domain.SignalBinding, quantile float64, from, to time.Time, step time.Duration) (domain.SeriesResult, string, error) {
	query, err := renderIn(rd.l, b, step, quantile)
	if err != nil {
		return domain.SeriesResult{}, "", err
	}
	res, err := rd.batch.Query(ctx, b.DataSourceID, domain.SeriesQuery{Query: query, From: from, To: to, Step: step})
	if err != nil {
		return domain.SeriesResult{}, query, err
	}
	return res, query, nil
}

// read is one binding read on its own, outside any request's reader.
func (s *Service) read(ctx context.Context, b domain.SignalBinding, quantile float64, from, to time.Time, step time.Duration) ([]domain.Series, string, error) {
	l, err := s.lookup(ctx)
	if err != nil {
		return nil, "", err
	}
	res, query, err := s.reader(l).read(ctx, b, quantile, from, to, step)
	return res.Series, query, err
}
