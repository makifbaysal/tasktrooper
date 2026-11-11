package runtime

import (
	"context"

	"github.com/google/uuid"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	audiencetools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/audience"
	audienceapp "github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type audienceDeps struct {
	repositories port.RepositoryStore
	agents       port.CatalogStore
	llm          port.LLMClient
	model        *projectmodel.Service
}

// wireAudience builds audience research: synthetic personas, studies and
// reports, all run on the person's own agent runtime (the router sends a
// coding-agent CLI to this machine, an API provider through the agent loop)
// and kept in the local database. Its tools go to the Product Manager.
func (e *engine) wireAudience(ctx context.Context, d audienceDeps) *audienceapp.Service {
	if e.pgDB == nil || e.agentRouter == nil || d.agents == nil {
		return nil
	}
	deps := audienceapp.Deps{
		Store:      pgstore.NewAudienceStore(e.pgDB),
		Agents:     d.agents,
		Runner:     e.agentRouter,
		LLM:        d.llm,
		Background: ctx,
	}
	if d.repositories != nil {
		deps.Repos = d.repositories
	}
	if d.model != nil {
		model := d.model
		deps.Brief = func(ctx context.Context, repositoryID uuid.UUID) (string, error) {
			return model.Brief(ctx, repositoryID, projectmodel.BriefScope{})
		}
	}
	svc := audienceapp.NewService(deps)
	// A process that died took the workers of whatever was running.
	svc.RecoverInterrupted(ctx)
	for _, tool := range audiencetools.NewExecutors(&audiencetools.ToolKit{Service: svc}) {
		e.reg.Register(tool)
	}
	return svc
}
