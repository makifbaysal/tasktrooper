package runtime

import (
	"context"

	"github.com/rs/zerolog/log"

	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	observabilitytools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/observability"
	alertingapp "github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	datasourceapp "github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsai"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	signalsapp "github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	workflowapp "github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type observabilityAIDeps struct {
	repositories port.RepositoryStore
	tasks        *repository.Service
	workflows    *workflowapp.Service
	agents       port.CatalogStore
	sessions     *session.Service
	signals      *signalsapp.Service
	sources      *datasourceapp.Service
	alerting     *alertingapp.Service
}

// wireObservabilityAI builds error definitions and the release engineer's
// suggestion jobs, and registers the release engineer's observability tools.
// Every optional dependency is handed on only when it exists, so no typed
// nil reaches an interface. The error definition sweep runs only when alert
// evaluation is enabled; the inbox annotation does not need it.
func (e *engine) wireObservabilityAI(ctx context.Context, cfg *domain.Config, d observabilityAIDeps) (httpadapter.ErrorDefinitionService, httpadapter.OpsAIService) {
	if e.pgDB == nil || d.repositories == nil || d.workflows == nil {
		return nil, nil
	}
	model := pgstore.NewProjectModelStore(e.pgDB)
	defDeps := errordef.Deps{
		Store:        pgstore.NewErrorDefinitionStore(e.pgDB),
		Components:   model,
		Repositories: d.repositories,
		Roles:        d.workflows,
		DefectTypes:  d.workflows,
	}
	if e.opsQuerySvc != nil {
		defDeps.Errors = e.opsQuerySvc
	}
	if e.prodOpsSvc != nil {
		defDeps.Incidents = e.prodOpsSvc
	}
	if d.tasks != nil {
		defDeps.Tasks = d.tasks
	}
	defs, err := errordef.NewService(defDeps)
	if err != nil {
		log.Error().Err(err).Msg("error definitions: disabled")
		return nil, nil
	}
	if cfg.Alerting.Enabled {
		defs.Start(ctx)
	}

	aiDeps := opsai.Deps{
		Jobs:         pgstore.NewAIJobStore(e.pgDB),
		Roles:        d.workflows,
		Components:   model,
		Repositories: d.repositories,
		Environments: pgstore.NewEnvironmentStore(e.pgDB),
		Links:        model,
		Policy:       cfg.Tools.DefaultPolicy,
	}
	if d.sessions != nil {
		aiDeps.Sessions = d.sessions
	}
	if d.agents != nil {
		aiDeps.Agents = d.agents
	}
	if d.signals != nil {
		aiDeps.Signals = d.signals
	}
	if d.tasks != nil {
		aiDeps.Tasks = d.tasks
	}
	if e.cloudSvc != nil {
		aiDeps.Analytics = e.cloudSvc
	}
	ai, err := opsai.NewService(aiDeps)
	if err != nil {
		log.Error().Err(err).Msg("observability AI jobs: disabled")
		return defs, nil
	}
	ai.Start(ctx)

	kit := &observabilitytools.ToolKit{Components: model, Definitions: defs, Jobs: ai, SourceUse: pgstore.NewSignalBindingStore(e.pgDB)}
	if e.opsQuerySvc != nil {
		kit.Errors = e.opsQuerySvc
	}
	if d.sources != nil {
		kit.DataSources = d.sources
	}
	if d.signals != nil {
		kit.Bindings = d.signals
	}
	if d.alerting != nil {
		kit.Alerts = d.alerting
	}
	for _, tool := range observabilitytools.NewExecutors(kit) {
		e.reg.Register(tool)
	}
	return defs, ai
}
