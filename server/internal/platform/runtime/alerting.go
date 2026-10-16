package runtime

import (
	"context"

	"github.com/rs/zerolog/log"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	alertingapp "github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	datasourceapp "github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	notifyapp "github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	signalsapp "github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// newAlertingService wires the alert rules over the signal bindings and the
// data sources; without them there is nothing to read, so the routes stay
// unmounted. Firing goes through prodops when it is wired and the shared
// notifier otherwise; the evaluator starts only when config enables it.
func (e *engine) newAlertingService(ctx context.Context, cfg domain.AlertingConfig, sigs *signalsapp.Service, sources *datasourceapp.Service) *alertingapp.Service {
	if e.pgDB == nil || sigs == nil || sources == nil {
		return nil
	}
	deps := alertingapp.Deps{
		Rules:      pgstore.NewAlertRuleStore(e.pgDB),
		Signals:    sigs,
		Sources:    sources,
		Components: pgstore.NewProjectModelStore(e.pgDB),
	}
	if e.prodOpsSvc != nil {
		deps.Incidents = e.prodOpsSvc
	}
	notifier := e.notifier
	if notifier == nil {
		notifier = notifyapp.NewService()
	}
	deps.Notifier = notifier
	svc, err := alertingapp.NewService(deps,
		alertingapp.WithInterval(cfg.Interval),
		alertingapp.WithMaxRulesPerSweep(cfg.MaxRulesPerSweep))
	if err != nil {
		log.Error().Err(err).Msg("alert rules: disabled")
		return nil
	}
	if cfg.Enabled {
		svc.Start(ctx)
		e.alertEvaluator = svc
	}
	return svc
}
