package prodops

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var ErrAlertRateLimited = errors.New("alert ingest rate limited")

const (
	DefaultAlertRequestsPerMinute   = 60
	DefaultAlertBurst               = 10
	DefaultAlertNewIncidentsPerHour = 20

	newIncidentWindow = time.Hour
)

// alertIngestGuard is in memory on purpose: the local server is one process,
// and a restart forgetting the counts is a far smaller risk than a token
// that can open incidents without bound. Its maps only ever hold tokens that
// resolved, so a caller guessing tokens cannot grow them.
type alertIngestGuard struct {
	limits domain.AlertIngestLimits

	mu       sync.Mutex
	requests map[string]*rate.Limiter
	opened   map[uuid.UUID][]time.Time
}

func newAlertIngestGuard(limits domain.AlertIngestLimits) *alertIngestGuard {
	if limits.RequestsPerMinute <= 0 {
		limits.RequestsPerMinute = DefaultAlertRequestsPerMinute
	}
	if limits.Burst <= 0 {
		limits.Burst = DefaultAlertBurst
	}
	if limits.NewIncidentsPerHour <= 0 {
		limits.NewIncidentsPerHour = DefaultAlertNewIncidentsPerHour
	}
	return &alertIngestGuard{
		limits:   limits,
		requests: make(map[string]*rate.Limiter),
		opened:   make(map[uuid.UUID][]time.Time),
	}
}

func (g *alertIngestGuard) allowRequest(tokenHash string, now time.Time) bool {
	g.mu.Lock()
	lim, ok := g.requests[tokenHash]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(float64(g.limits.RequestsPerMinute)/60), g.limits.Burst)
		g.requests[tokenHash] = lim
	}
	g.mu.Unlock()
	return lim.AllowN(now, 1)
}

func (g *alertIngestGuard) reserveNewIncident(tokenID uuid.UUID, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := now.Add(-newIncidentWindow)
	kept := g.opened[tokenID][:0]
	for _, at := range g.opened[tokenID] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= g.limits.NewIncidentsPerHour {
		g.opened[tokenID] = kept
		return false
	}
	g.opened[tokenID] = append(kept, now)
	return true
}

func (g *alertIngestGuard) releaseNewIncident(tokenID uuid.UUID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if n := len(g.opened[tokenID]); n > 0 {
		g.opened[tokenID] = g.opened[tokenID][:n-1]
	}
}
