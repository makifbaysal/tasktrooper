package runtime

import (
	"github.com/rs/zerolog/log"

	appstoremetricsds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/appstoremetrics"
	cloudwatchds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/cloudwatch"
	datadogds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/datadog"
	elasticsearchds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/elasticsearch"
	ga4ds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/ga4"
	gcpmonitoringds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/gcpmonitoring"
	kafkads "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/kafka"
	lokids "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/loki"
	natsds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/nats"
	newrelicds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/newrelic"
	plausibleds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/plausible"
	playvitalsds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/playvitals"
	posthogds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/posthog"
	prometheusds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/prometheus"
	rabbitmqds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/rabbitmq"
	sentryds "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/sentry"
	tempods "github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/tempo"
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
// Google Cloud, CloudWatch and Google Analytics kinds read the connected cloud accounts' own
// credentials through their store, so a source of theirs holds no copy; the
// Google Play and App Store Connect kinds read the workspace's store
// credentials the same way, handed over once the store ops service exists
// (SetStoreCredentials).
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
		Kinds:         dataSourceKinds(),
		URLPolicy:     &policy,
	})
	if err != nil {
		log.Error().Err(err).Msg("data sources: disabled")
		return nil
	}
	return svc
}

func dataSourceKinds() []port.DataSourceKindAdapter {
	return []port.DataSourceKindAdapter{
		prometheusds.New(),
		gcpmonitoringds.New(),
		cloudwatchds.New(),
		kafkads.New(),
		rabbitmqds.New(),
		natsds.New(),
		lokids.New(),
		elasticsearchds.New(),
		tempods.New(),
		playvitalsds.New(),
		appstoremetricsds.New(),
		sentryds.New(),
		posthogds.New(),
		ga4ds.New(),
		plausibleds.New(),
		datadogds.New(),
		newrelicds.New(),
	}
}
