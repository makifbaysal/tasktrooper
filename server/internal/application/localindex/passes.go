package localindex

import (
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func passKey(ref port.LocalIndexRef) string {
	return ref.RepoKey + "\x00" + ref.Branch
}

// passes is what only this process knows about the passes over each index:
// whether one runs now (a row a crash left "running" says it does), how far
// it has got before the row hears of it (waiting for another pass, seeding),
// and why the last one failed when it failed before reaching the row.
type passes struct {
	mu      sync.Mutex
	live    map[string]*livePass
	failure map[string]string
}

type livePass struct {
	count    int
	progress port.LocalIndexProgress
}

type passState struct {
	running  bool
	progress port.LocalIndexProgress
	failure  string
}

type trackedPass struct {
	owner *passes
	key   string
}

func (p *passes) begin(key string) trackedPass {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.live == nil {
		p.live = make(map[string]*livePass)
	}
	pass, ok := p.live[key]
	if !ok {
		pass = &livePass{}
		p.live[key] = pass
	}
	pass.count++
	return trackedPass{owner: p, key: key}
}

func (t trackedPass) report(progress port.LocalIndexProgress) {
	t.owner.mu.Lock()
	defer t.owner.mu.Unlock()
	if pass, ok := t.owner.live[t.key]; ok {
		pass.progress = progress
	}
}

func (t trackedPass) end(err error) {
	t.owner.mu.Lock()
	defer t.owner.mu.Unlock()
	if pass, ok := t.owner.live[t.key]; ok {
		pass.count--
		if pass.count <= 0 {
			delete(t.owner.live, t.key)
		}
	}
	if err == nil {
		delete(t.owner.failure, t.key)
		return
	}
	if t.owner.failure == nil {
		t.owner.failure = make(map[string]string)
	}
	t.owner.failure[t.key] = err.Error()
}

func (p *passes) state(key string) passState {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := passState{failure: p.failure[key]}
	if pass, ok := p.live[key]; ok {
		st.running = true
		st.progress = pass.progress
	}
	return st
}
