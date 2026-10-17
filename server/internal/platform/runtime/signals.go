package runtime

import (
	"github.com/rs/zerolog/log"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	datasourceapp "github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	signalsapp "github.com/makifbaysal/tasktrooper/server/internal/application/signals"
)

// newSignalsService wires signal bindings over the data sources; without
// them (no Postgres, or a catalog that did not validate) there is nothing to
// bind to, so the routes stay unmounted. A golden list that does not
// validate does the same rather than failing boot.
func newSignalsService(db *pgstore.DB, sources *datasourceapp.Service) *signalsapp.Service {
	if db == nil || sources == nil {
		return nil
	}
	model := pgstore.NewProjectModelStore(db)
	svc, err := signalsapp.NewService(signalsapp.Deps{
		Bindings:     pgstore.NewSignalBindingStore(db),
		Sources:      sources,
		Components:   model,
		Environments: pgstore.NewEnvironmentStore(db),
		Links:        model,
		StoreApps:    pgstore.NewMobileStoreAppStore(db),
	})
	if err != nil {
		log.Error().Err(err).Msg("signal bindings: disabled")
		return nil
	}
	return svc
}
