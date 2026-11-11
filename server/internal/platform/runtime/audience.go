package runtime

import (
	"context"

	"github.com/google/uuid"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	audiencetools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	audienceapp "github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type audienceDeps struct {
	repositories port.RepositoryStore
	agents       port.CatalogStore
	llm          port.LLMClient
	model        *projectmodel.Service
	sessions     *session.Service
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
		Pages:      web.NewReader(0),
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
	kit := &audiencetools.ToolKit{Service: svc}
	if d.sessions != nil {
		kit.Waker = d.sessions
		d.sessions.SetWakes(pgstore.NewSessionWakeStore(e.pgDB), audienceWakeCheckers(svc))
		session.NewWakeSweeper(d.sessions).Start(ctx, session.WakeSweeperInterval)
	}
	for _, tool := range audiencetools.NewExecutors(kit) {
		e.reg.Register(tool)
	}
	return svc
}

func audienceWakeCheckers(svc *audienceapp.Service) map[string]session.WakeChecker {
	return map[string]session.WakeChecker{
		domain.WakeKindAudienceBuild: func(ctx context.Context, id uuid.UUID) (session.WakeState, error) {
			a, err := svc.Audience(ctx, id)
			if err != nil {
				return session.WakeState{}, err
			}
			st := session.WakeState{Status: string(a.Status), Detail: a.Error}
			st.Done = a.Status != domain.AudienceBuilding
			return st, nil
		},
		domain.WakeKindStudy: func(ctx context.Context, id uuid.UUID) (session.WakeState, error) {
			d, err := svc.Study(ctx, id)
			if err != nil {
				return session.WakeState{}, err
			}
			st := session.WakeState{Status: string(d.Status), Detail: d.Error}
			st.Done = d.Status.Terminal() || d.Status == domain.StudyInterviewed
			return st, nil
		},
	}
}
