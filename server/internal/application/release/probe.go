package release

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

const (
	probeTimeout      = 10 * time.Second
	maxSmokeBodyBytes = 1 << 20
)

// RunSmoke runs the frozen profile's smoke checks against the release's
// verify target right now, appends the results, and returns them; it never
// changes the release's status.
func (s *Service) RunSmoke(ctx context.Context, releaseID uuid.UUID) ([]domain.SmokeResult, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	results := s.runSmokeChecks(ctx, r.Profile.Verify.Smoke, r.Checks.BaseURL)

	expect := r.Status
	r.Checks.Smoke = appendSmokeCapped(r.Checks.Smoke, results, maxStoredSmoke)
	if _, err := s.store.Update(ctx, r, expect); err != nil {
		if !errors.Is(err, domain.ErrReleaseWrongStatus) {
			log.Warn().Err(err).Str("release_id", releaseID.String()).Msg("release: recording an on-demand smoke run failed")
		}
	}
	return results, nil
}

func (s *Service) runSmokeChecks(ctx context.Context, checks []domain.SmokeCheck, baseURL string) []domain.SmokeResult {
	if len(checks) == 0 {
		return nil
	}
	out := make([]domain.SmokeResult, 0, len(checks))
	for _, c := range checks {
		out = append(out, s.runSmokeCheck(ctx, c.Normalized(), baseURL))
	}
	return out
}

func (s *Service) runSmokeCheck(ctx context.Context, c domain.SmokeCheck, baseURL string) domain.SmokeResult {
	res := domain.SmokeResult{Check: c, At: s.now()}

	target := c.Path
	if !strings.Contains(target, "://") {
		baseURL = strings.TrimSpace(baseURL)
		if baseURL == "" {
			res.Error = "no base URL — this smoke check's relative path was skipped"
			return res
		}
		target = joinURL(baseURL, c.Path)
	}
	res.URL = target

	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	dest, err := s.policy.Validate(reqCtx, target)
	if err != nil {
		log.Warn().Err(err).Str("url", urlguard.LogRaw(target)).Msg("release: smoke check destination refused")
		res.Error = "destination not allowed"
		return res
	}
	req, err := http.NewRequestWithContext(reqCtx, c.Method, dest.URL.String(), nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	start := s.now()
	resp, err := s.policy.ClientFor(dest, probeTimeout).Do(req)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.LatencyMS = s.now().Sub(start).Milliseconds()
	res.Status = resp.StatusCode
	res.OK = c.Passes(resp.StatusCode)
	if res.OK && c.Contains != "" {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSmokeBodyBytes))
		if !strings.Contains(string(body), c.Contains) {
			res.OK = false
			res.Error = "response did not contain the expected text"
		}
	}
	return res
}

func (s *Service) probeHealth(ctx context.Context, healthURL string) (domain.HealthSample, bool) {
	healthURL = strings.TrimSpace(healthURL)
	if healthURL == "" {
		return domain.HealthSample{}, false
	}
	sample := domain.HealthSample{At: s.now()}

	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	dest, err := s.policy.Validate(reqCtx, healthURL)
	if err != nil {
		log.Warn().Err(err).Str("url", urlguard.LogRaw(healthURL)).Msg("release: health check destination refused")
		sample.Error = "destination not allowed"
		return sample, true
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, dest.URL.String(), nil)
	if err != nil {
		sample.Error = err.Error()
		return sample, true
	}
	start := s.now()
	resp, err := s.policy.ClientFor(dest, probeTimeout).Do(req)
	if err != nil {
		sample.Error = err.Error()
		return sample, true
	}
	defer resp.Body.Close()
	sample.LatencyMS = s.now().Sub(start).Milliseconds()
	sample.Status = resp.StatusCode
	sample.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	return sample, true
}

func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

const (
	maxStoredHealth = 60
	maxStoredSmoke  = 40
)

func appendSmokeCapped(existing, add []domain.SmokeResult, max int) []domain.SmokeResult {
	out := append(existing, add...)
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func appendHealthCapped(existing []domain.HealthSample, add domain.HealthSample, max int) []domain.HealthSample {
	out := append(existing, add)
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}
