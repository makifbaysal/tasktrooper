package datasource

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// RenderSignal expands one signal of a recipe for a source in its kind's
// query language, the query a signal binding runs. The recipe must be of the
// source's kind and map the signal; a param it references without a value is
// ErrInvalidInput.
func (s *Service) RenderSignal(ctx context.Context, dataSourceID uuid.UUID, recipeID, signal string, params map[string]string, opts RenderOptions) (string, error) {
	ds, err := s.store.GetDataSource(ctx, dataSourceID)
	if err != nil {
		return "", err
	}
	recipes, err := s.Recipes(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range recipes {
		if r.ID != recipeID {
			continue
		}
		if r.SourceKind != ds.Kind {
			return "", fmt.Errorf("recipe %q is for %s sources, not %s: %w", recipeID, r.SourceKind, ds.Kind, ErrInvalidInput)
		}
		query, ok := r.Signals[signal]
		if !ok {
			return "", fmt.Errorf("recipe %q has no query for %s: %w", recipeID, signal, ErrInvalidInput)
		}
		return RenderKindQuery(ds.Kind, query, params, opts)
	}
	return "", fmt.Errorf("unknown recipe %q: %w", recipeID, ErrInvalidInput)
}

// RenderRawQuery expands a query a person wrote for a source the way
// RenderSignal expands a recipe's, so the built-ins work there too.
func (s *Service) RenderRawQuery(ctx context.Context, dataSourceID uuid.UUID, query string, params map[string]string, opts RenderOptions) (string, error) {
	ds, err := s.store.GetDataSource(ctx, dataSourceID)
	if err != nil {
		return "", err
	}
	return RenderKindQuery(ds.Kind, query, params, opts)
}
