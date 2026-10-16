package release

import (
	"context"
	"math"
	"strconv"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// checkImpact records the release's deploy impact against the profile's
// impact thresholds and early-stops the soak on the first one breached. An
// unreadable impact leaves the soak to its other checks.
func (s *Service) checkImpact(ctx context.Context, r *domain.Release) {
	reader := s.impactReader()
	if reader == nil {
		return
	}
	impact, err := reader.ForRelease(ctx, *r)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: reading deploy impact failed")
		return
	}
	check, breach := impactCheck(impact, r.Profile.Verify)
	r.Checks.Impact = &check
	if breach != nil && r.Checks.EarlyStop == "" {
		r.Checks.EarlyStop = impactBreachKey.Render(*breach)
	}
}

// impactCheck holds http.error_ratio's delta (percentage points) to
// max_error_rate_delta and http.latency's p95 delta_pct to max_p95_delta_pct.
// A threshold whose signal is unbound or has no delta is listed unchecked,
// not passed.
func impactCheck(impact domain.DeployImpact, verify domain.DeliveryVerify) (domain.ReleaseImpactCheck, *impactBreachInput) {
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
		switch {
		case si.Signal == domain.SignalHTTPErrorRatio && verify.MaxErrorRateDelta > 0 && si.Delta != nil:
			limit := verify.MaxErrorRateDelta
			rs.MaxDelta = &limit
			rs.Breached = *si.Delta > limit
			checked[domain.VerifyMaxErrorRateDelta] = true
			if rs.Breached && breach == nil {
				breach = &impactBreachInput{ErrorRate: true, Before: number(si.Before), After: number(si.After), Delta: number(si.Delta), Limit: number(&limit)}
			}
		case si.Signal == domain.SignalHTTPLatency && verify.MaxP95DeltaPct > 0 && si.DeltaPct != nil:
			limit := verify.MaxP95DeltaPct
			rs.MaxDeltaPct = &limit
			rs.Breached = *si.DeltaPct > limit
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

func number(v *float64) string {
	if v == nil {
		return "?"
	}
	return strconv.FormatFloat(math.Round(*v*100)/100, 'f', -1, 64)
}
