package runtime

import (
	"github.com/rs/zerolog/log"

	cloudwatchds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/cloudwatch"
	gcpmonitoringds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/gcpmonitoring"
	prometheusds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/prometheus"
	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	datasourceapp "github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// newDataSourceService wires the observability data sources. The store gets
// the boot-time cipher like every other secret-bearing store: its first real
// use is a later HTTP request, after scrubProcessSecrets wiped the key. The
// URL policy is urlguard.HumanConfigured because this is one person's own
// computer: they typed the URL, and a local Prometheus is on this machine or
// its network. A catalog that does not validate leaves the routes unmounted
// rather than failing boot; the catalog tests keep that from shipping. The
// Google Cloud and CloudWatch kinds read the connected cloud accounts' own
// credentials through their store, so a source of theirs holds no copy.
func newDataSourceService(db *pgstore.DB, cipher *secrets.Cipher, cipherErr error) *datasourceapp.Service {
	store := pgstore.NewDataSourceStore(db)
	store.SetCipher(cipher, cipherErr)
	cloudAccounts := pgstore.NewCloudAccountStore(db)
	cloudAccounts.SetCipher(cipher, cipherErr)
	policy := urlguard.HumanConfigured()
	svc, err := datasourceapp.NewService(datasourceapp.Deps{
		Store:         store,
		Recipes:       store,
		CloudAccounts: cloudAccounts,
		Kinds: []port.DataSourceKindAdapter{
			prometheusds.New(),
			gcpmonitoringds.New(),
			cloudwatchds.New(),
		},
		URLPolicy: &policy,
	})
	if err != nil {
		log.Error().Err(err).Msg("data sources: disabled")
		return nil
	}
	return svc
}
