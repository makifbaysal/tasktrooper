package catalog

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

var ErrCatalogPendingNotFound = errors.New("pending catalog change not found")

// Resolves one parked item by taking the catalog's version of it: a local edit is overwritten, a skill the catalog dropped is deleted.
// The agent's auto_pull_agent_updates and keep_skills_updated toggles are left as they are.
func (s *Service) ApplyCatalogPending(ctx context.Context, reader port.CatalogRepoReader, syncStore port.CatalogSyncStore, id uuid.UUID) error {
	syncMu.Lock()
	defer syncMu.Unlock()
	ctx = WithVersionSource(ctx, VersionSource{Source: domain.CatalogVersionSourceUpstream})

	items, err := syncStore.ListCatalogPending(ctx)
	if err != nil {
		return err
	}
	item, found := pendingByID(items, id)
	if !found {
		return ErrCatalogPendingNotFound
	}
	defs, _, err := reader.ReadCatalog(ctx)
	if err != nil {
		return err
	}
	def, found := upstreamBySlug(defs, item.AgentSlug)
	if !found {
		return invalidInput("the catalog no longer has agent %q; dismiss this item instead", item.AgentSlug)
	}
	agent, installed, err := s.agentByCatalogSlug(ctx, item.AgentSlug)
	if err != nil {
		return err
	}

	res := &domain.CatalogSyncResult{}
	switch {
	case item.Kind == domain.CatalogPendingKindSkill && !installed:
		return invalidInput("agent %q is not installed yet; sync the catalog first", item.AgentSlug)
	case item.Kind == domain.CatalogPendingKindSkill:
		err = s.takeUpstreamSkill(ctx, agent, def, item.Name)
	case installed:
		if _, err = s.applyUpstreamAgent(ctx, agent, def, syncStore, res); err == nil {
			err = s.stampAgentEtag(ctx, agent.ID, def.Etag)
		}
	default:
		var created domain.Agent
		if created, err = s.CreateAgent(ctx, newAgentRequest(def)); err == nil {
			err = s.wireNewAgent(ctx, created, def, syncStore, res)
		}
	}
	if err != nil {
		return err
	}
	s.clearPending(ctx, syncStore, item.AgentSlug, item.Kind, item.Name)
	log.Info().Str("agent", item.AgentSlug).Str("kind", item.Kind).Str("name", item.Name).
		Msg("catalog: pending change resolved with the catalog version")
	return nil
}

func (s *Service) takeUpstreamSkill(ctx context.Context, agent domain.Agent, def domain.UpstreamAgent, name string) error {
	skills, err := s.store.ListSkillsByAgent(ctx, agent.ID)
	if err != nil {
		return err
	}
	var local domain.Skill
	exists := false
	for _, sk := range skills {
		if sk.Name == name {
			local, exists = sk, true
			break
		}
	}
	usk, inCatalog := skillByName(def.Skills, name)
	switch {
	case inCatalog && exists:
		return s.applyUpstreamSkill(ctx, agent, local, usk)
	case inCatalog:
		stackIDs, err := s.ensureTechStacks(ctx, agent.ID, def)
		if err != nil {
			return err
		}
		stackID := stackIDs[stackKey(usk.TechStack)]
		return s.ingestSkill(ctx, agent.ID, usk, &stackID)
	case exists:
		return s.DeleteSkillForAgent(ctx, agent.ID, local.ID)
	}
	return nil
}

func (s *Service) agentByCatalogSlug(ctx context.Context, slug string) (domain.Agent, bool, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return domain.Agent{}, false, err
	}
	for _, a := range agents {
		if a.CatalogSlug == slug {
			return a, true, nil
		}
	}
	return domain.Agent{}, false, nil
}

func pendingByID(items []domain.CatalogPending, id uuid.UUID) (domain.CatalogPending, bool) {
	for _, p := range items {
		if p.ID == id {
			return p, true
		}
	}
	return domain.CatalogPending{}, false
}

func upstreamBySlug(defs []domain.UpstreamAgent, slug string) (domain.UpstreamAgent, bool) {
	for _, d := range defs {
		if d.Slug == slug {
			return d, true
		}
	}
	return domain.UpstreamAgent{}, false
}
