package runtime

import (
	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/readiness"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	workflowapp "github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
)

// wireReadiness builds the repository setup checklist over the services it
// reads. Each optional source is handed on only when it exists, so no typed
// nil reaches an interface; without the project model there is nothing to
// derive a checklist from.
func (e *engine) wireReadiness(repositorySvc *repository.Service, workflowSvc *workflowapp.Service, opsAI httpadapter.OpsAIService) *readiness.Service {
	if e.pgDB == nil || repositorySvc == nil || e.projectModelSvc == nil {
		return nil
	}
	deps := readiness.Deps{
		Store:     pgstore.NewReadinessStore(e.pgDB),
		Repos:     repositorySvc,
		Model:     e.projectModelSvc,
		StoreApps: pgstore.NewMobileStoreAppStore(e.pgDB),
	}
	if workflowSvc != nil {
		deps.Roles = workflowSvc
	}
	if opsAI != nil {
		deps.Observability = opsAI
	}
	if e.designSystemSvc != nil {
		deps.Designs = e.designSystemSvc
	}
	return readiness.New(deps)
}
