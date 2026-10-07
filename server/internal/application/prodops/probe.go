package prodops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultProbeInterval = 3 * time.Minute

	probeFailureThreshold = 2
	probeTimeout          = 10 * time.Second
)

type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

type Monitor struct {
	targets  port.DeployTargetStore
	ingester IncidentIngester

	policy urlguard.Policy

	mu       sync.Mutex
	failures map[uuid.UUID]int
	opened   map[uuid.UUID]bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewMonitor(targets port.DeployTargetStore, ingester IncidentIngester) *Monitor {
	return &Monitor{
		targets:  targets,
		ingester: ingester,
		policy:   urlguard.Default(),
		failures: map[uuid.UUID]int{},
		opened:   map[uuid.UUID]bool{},
	}
}

func (m *Monitor) SetURLPolicy(p urlguard.Policy) { m.policy = p }

func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultProbeInterval
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		m.Sweep(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("production health monitor started")
}

func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Monitor) Sweep(ctx context.Context) {
	targets, err := m.targets.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("list deploy targets for health probe failed")
		return
	}
	for _, target := range targets {
		if target.HealthURL == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.check(ctx, target)
	}
}

func (m *Monitor) check(ctx context.Context, target domain.DeployTarget) {
	ok, detail := m.probe(ctx, target.HealthURL)

	m.mu.Lock()
	if ok {
		wasOpen := m.opened[target.ID]
		m.failures[target.ID] = 0
		m.mu.Unlock()
		if wasOpen {

			if err := m.ingest(ctx, target, "recovered", true, detail); err == nil {
				m.mu.Lock()
				m.opened[target.ID] = false
				m.mu.Unlock()
			}
		}
		return
	}
	m.failures[target.ID]++
	failures := m.failures[target.ID]
	if failures >= probeFailureThreshold {
		m.opened[target.ID] = true
	}
	m.mu.Unlock()

	if failures >= probeFailureThreshold {
		m.ingest(ctx, target, detail, false, detail)
	}
}

func (m *Monitor) ingest(ctx context.Context, target domain.DeployTarget, title string, resolved bool, detail string) error {
	severity := domain.IncidentSeverityHigh
	if target.Env == domain.DeployEnvProd {
		severity = domain.IncidentSeverityCritical
	}

	safeURL := urlguard.LogRaw(target.HealthURL)
	in := domain.IncidentInput{
		RepositoryID: target.RepositoryID,
		Env:          target.Env,
		Source:       domain.IncidentSourceProbe,
		Severity:     severity,
		Title:        fmt.Sprintf("%s health check failing: %s", target.Env, title),
		Detail:       fmt.Sprintf("Health URL %s did not answer successfully.\n%s", safeURL, detail),

		Fingerprint: domain.IncidentFingerprint("health-probe", target.Env, target.HealthURL),
		Payload: map[string]any{
			"health_url": safeURL,
			"provider":   target.Provider,
			"env":        target.Env,
		},
		Resolved: resolved,
	}
	if _, err := m.ingester.Ingest(ctx, in); err != nil {
		log.Warn().Err(err).Str("health_url", safeURL).Msg("probe incident ingest failed")
		return err
	}
	return nil
}

func (m *Monitor) probe(ctx context.Context, url string) (bool, string) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	target, err := m.policy.Validate(reqCtx, url)
	if err != nil {
		log.Warn().Err(err).Str("health_url", urlguard.LogRaw(url)).Msg("health probe destination refused")

		return false, "health URL is not an allowed destination"
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target.URL.String(), nil)
	if err != nil {
		return false, "health URL is not an allowed destination"
	}
	started := time.Now()
	resp, err := m.policy.ClientFor(target, probeTimeout).Do(req)
	if err != nil {
		if errors.Is(err, urlguard.ErrBlocked) {

			log.Warn().Err(err).Str("health_url", urlguard.LogValue(target.URL)).Msg("health probe destination refused")
			return false, "health URL is not an allowed destination"
		}
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Sprintf("HTTP %d after %s", resp.StatusCode, time.Since(started).Round(time.Millisecond))
	}
	return true, fmt.Sprintf("HTTP %d in %s", resp.StatusCode, time.Since(started).Round(time.Millisecond))
}
