package release

import (
	"context"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	// impactReadTimeout keeps one slow data source from stalling a sweep
	// that has every other watched release still to advance.
	impactReadTimeout = 15 * time.Second
	// minImpactAfter and minImpactAfterPoints: a few minutes of a rate, its
	// first point still straddling the deploy, is not a regression yet.
	minImpactAfter       = 10 * time.Minute
	minImpactAfterPoints = 5
	// impactSettleGrace bounds how long past its window a soak keeps
	// verifying while a threshold's signal is still settling.
	impactSettleGrace = 15 * time.Minute
)

// impactThresholds is satisfied by deployimpact.Service.
type impactThresholds interface {
	Thresholds() domain.DeployImpactConfig
}

// freshImpact is satisfied by deployimpact.Service: a read past its
// per-minute cache.
type freshImpact interface {
	ForReleaseFresh(ctx context.Context, r domain.Release) (domain.DeployImpact, error)
}

// checkImpact records the release's deploy impact against the profile's
// impact thresholds and early-stops the soak on the first one breached; fresh
// reads past deploy impact's cache. An unreadable impact leaves the soak to
// its other checks.
func (s *Service) checkImpact(ctx context.Context, r *domain.Release, fresh bool) {
	reader := s.impactReader()
	if reader == nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, impactReadTimeout)
	defer cancel()
	read := reader.ForRelease
	if f, ok := reader.(freshImpact); ok && fresh {
		read = f.ForReleaseFresh
	}
	impact, err := read(rctx, *r)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: reading deploy impact failed")
		return
	}
	floors := domain.DeployImpactConfig{}.WithDefaults()
	if t, ok := reader.(impactThresholds); ok {
		floors = t.Thresholds().WithDefaults()
	}
	check, breach := impactCheck(impact, r.Profile.Verify, floors, afterWindow(impact, *r, s.now()))
	r.Checks.Impact = &check
	if breach != nil && r.Checks.EarlyStop == "" {
		r.Checks.EarlyStop = impactBreachKey.Render(*breach)
	}
}

// afterWindow is how much of the hour after the deploy the impact covers.
func afterWindow(impact domain.DeployImpact, r domain.Release, now time.Time) time.Duration {
	deployed := impact.DeployedAt
	if deployed == nil {
		deployed = r.DeployedAt
	}
	if deployed == nil {
		return 0
	}
	end := impact.ComputedAt
	if end.IsZero() {
		end = now
	}
	if limit := deployed.Add(domain.DefaultImpactWindow); end.After(limit) {
		end = limit
	}
	return max(end.Sub(*deployed), 0)
}

// impactCheck holds http.error_ratio's delta (percentage points) to
// max_error_rate_delta and http.latency's p95 delta_pct to max_p95_delta_pct.
// A threshold breaches only once the after-window is long and dense enough,
// when the signal moved past it from yesterday's same hour too (the time of
// day masks it the way it masks deploy impact's verdict), and, for latency,
// by at least the latency floor. A threshold whose signal is unbound or has
// no delta is listed unchecked, not passed.
func impactCheck(impact domain.DeployImpact, verify domain.DeliveryVerify, floors domain.DeployImpactConfig, after time.Duration) (domain.ReleaseImpactCheck, *impactBreachInput) {
	check := domain.ReleaseImpactCheck{
		CheckedAt: impact.ComputedAt,
		Pending:   impact.Pending,
		Verdict:   impact.Verdict,
		Signals:   make([]domain.ReleaseImpactSignal, 0, len(impact.Signals)),
	}
	var breach *impactBreachInput
	checked := map[string]bool{}
	for _, si := range impact.Signals {
		rs := domain.ReleaseImpactSignal{
			Signal: si.Signal, Before: si.Before, After: si.After,
			Delta: si.Delta, DeltaPct: si.DeltaPct, Verdict: si.Verdict,
		}
		settling := after < minImpactAfter || si.AfterPoints < minImpactAfterPoints
		switch {
		case si.Signal == domain.SignalHTTPErrorRatio && verify.MaxErrorRateDelta > 0 && si.Delta != nil:
			limit := verify.MaxErrorRateDelta
			rs.MaxDelta = &limit
			rs.Settling = settling
			rs.Breached = !settling && *si.Delta > limit && pastBaseline(si, func(baseline float64) bool { return *si.After-baseline > limit })
			checked[domain.VerifyMaxErrorRateDelta] = true
			if rs.Breached && breach == nil {
				breach = &impactBreachInput{ErrorRate: true, Before: number(si.Before), After: number(si.After), Delta: number(si.Delta), Limit: number(&limit)}
			}
		case si.Signal == domain.SignalHTTPLatency && verify.MaxP95DeltaPct > 0 && si.DeltaPct != nil:
			limit := verify.MaxP95DeltaPct
			rs.MaxDeltaPct = &limit
			rs.Settling = settling
			rs.Breached = !settling && *si.DeltaPct > limit && si.Delta != nil && *si.Delta >= floors.LatencyFloorMS &&
				pastBaseline(si, func(baseline float64) bool {
					return baseline == 0 || (*si.After-baseline)/math.Abs(baseline)*100 > limit
				})
			checked[domain.VerifyMaxP95DeltaPct] = true
			if rs.Breached && breach == nil {
				breach = &impactBreachInput{Before: number(si.Before), After: number(si.After), Delta: number(si.DeltaPct), Limit: number(&limit)}
			}
		}
		check.Signals = append(check.Signals, rs)
	}
	if verify.MaxErrorRateDelta > 0 && !checked[domain.VerifyMaxErrorRateDelta] {
		check.Unchecked = append(check.Unchecked, domain.VerifyMaxErrorRateDelta)
	}
	if verify.MaxP95DeltaPct > 0 && !checked[domain.VerifyMaxP95DeltaPct] {
		check.Unchecked = append(check.Unchecked, domain.VerifyMaxP95DeltaPct)
	}
	return check, breach
}

// impactSettling reports whether a threshold's signal was still too short or
// too sparse to judge at the last check.
func impactSettling(c *domain.ReleaseImpactCheck) bool {
	if c == nil {
		return false
	}
	for _, sig := range c.Signals {
		if sig.Settling && !sig.Breached {
			return true
		}
	}
	return false
}

// settledUnchecked closes a soak whose threshold signal never settled: that
// threshold is listed unchecked, not passed.
func settledUnchecked(c *domain.ReleaseImpactCheck) {
	if c == nil {
		return
	}
	for _, sig := range c.Signals {
		if !sig.Settling || sig.Breached {
			continue
		}
		key := ""
		switch {
		case sig.MaxDelta != nil:
			key = domain.VerifyMaxErrorRateDelta
		case sig.MaxDeltaPct != nil:
			key = domain.VerifyMaxP95DeltaPct
		}
		if key != "" && !slices.Contains(c.Unchecked, key) {
			c.Unchecked = append(c.Unchecked, key)
		}
	}
}

// pastBaseline: with yesterday's same hour unread, only the move from the
// hour before can be judged.
func pastBaseline(si domain.SignalImpact, past func(baseline float64) bool) bool {
	if si.Baseline == nil || si.After == nil {
		return true
	}
	return past(*si.Baseline)
}

func number(v *float64) string {
	if v == nil {
		return "?"
	}
	return strconv.FormatFloat(math.Round(*v*100)/100, 'f', -1, 64)
}
