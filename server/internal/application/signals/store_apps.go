package signals

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// StoreApps lists the store apps a repository is linked to (one per
// platform: its App Store Connect app, its Google Play package).
type StoreApps interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error)
}

// detectFromStoreApps offers, for a mobile component whose repository has a
// store app linked, every recipe whose applies_to.store_platform is that
// app's platform, with params from the app (its package name, its App Store
// Connect id), on every source of the recipe's kind. Where no source of that
// kind exists yet, it answers an offer to create one instead. A listing that
// fails offers nothing.
func (s *Service) detectFromStoreApps(ctx context.Context, l lookup, comp domain.Component, relevant map[string]bool) ([]domain.SignalBinding, []domain.DataSourceOffer) {
	offers := []domain.DataSourceOffer{}
	if s.storeApps == nil || comp.Role.Get() != domain.ComponentRoleMobile {
		return nil, offers
	}
	apps, err := s.storeApps.ListByRepository(ctx, comp.RepositoryID)
	if err != nil {
		log.Debug().Err(err).Str("component", comp.ID.String()).Msg("signals: listing the repository's store apps failed")
		return nil, offers
	}
	slices.SortFunc(apps, func(a, b domain.MobileStoreApp) int { return strings.Compare(a.Platform, b.Platform) })
	var found []domain.SignalBinding
	for _, app := range apps {
		for _, id := range slices.Sorted(maps.Keys(l.recipes)) {
			r := l.recipes[id]
			if r.AppliesTo.StorePlatform == "" || r.AppliesTo.StorePlatform != app.Platform {
				continue
			}
			sigs := recipeSignals(r, relevant)
			if len(sigs) == 0 {
				continue
			}
			params := r.ParamsFromStoreApp(app)
			sources := 0
			for _, ds := range l.order {
				if ds.Kind != r.SourceKind {
					continue
				}
				sources++
				for _, sig := range sigs {
					found = append(found, suggestion(sig, ds.ID, r.ID, params))
				}
			}
			if sources > 0 {
				continue
			}
			if config, provider, ok := s.sources.StoreCredentialConfig(r.SourceKind); ok {
				offers = append(offers, domain.DataSourceOffer{
					Kind: r.SourceKind, Config: config, RecipeID: r.ID, Params: params,
					Signals: sigs, StoreCredential: provider,
				})
			}
		}
	}
	return found, offers
}
