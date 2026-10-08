package catalog

import (
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// syncProgress is the live state of the one sync syncMu admits; its zero value
// is an idle tracker.
type syncProgress struct {
	mu    sync.Mutex
	state domain.CatalogSyncProgress
}

func (p *syncProgress) start(agentsTotal int, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = domain.CatalogSyncProgress{Running: true, StartedAt: &at, AgentsTotal: agentsTotal}
}

func (p *syncProgress) agent(slug string, isNew bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.Agent = slug
	p.state.NewAgent = isNew
	p.state.SkillsDone, p.state.SkillsTotal = 0, 0
}

func (p *syncProgress) skillsToAdd(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.SkillsDone, p.state.SkillsTotal = 0, n
}

func (p *syncProgress) skillAdded() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.SkillsDone++
}

func (p *syncProgress) agentDone(added string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.AgentsDone++
	if added != "" {
		p.state.AgentsAdded = append(p.state.AgentsAdded, added)
	}
}

func (p *syncProgress) finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = domain.CatalogSyncProgress{}
}

func (p *syncProgress) snapshot() domain.CatalogSyncProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.state
	out.AgentsAdded = append([]string(nil), p.state.AgentsAdded...)
	return out
}
