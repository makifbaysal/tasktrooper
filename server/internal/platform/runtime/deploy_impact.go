package runtime

import (
	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deployimpact"
	releaseapp "github.com/makifbaysal/tasktrooper/server/internal/application/release"
	signalsapp "github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// wireDeployImpact hands the release service deploy impact once the signals
// it reads exist; they are built after the release sweeper starts, so until
// then a soak skips its impact thresholds and the impact endpoint answers
// unknown.
func wireDeployImpact(release *releaseapp.Service, db *pgstore.DB, signals *signalsapp.Service, thresholds domain.DeployImpactConfig) {
	if release == nil || db == nil || signals == nil {
		return
	}
	release.SetImpact(deployimpact.New(deployimpact.Deps{
		Signals:    signals,
		Components: pgstore.NewProjectModelStore(db),
		Thresholds: thresholds,
	}))
}
