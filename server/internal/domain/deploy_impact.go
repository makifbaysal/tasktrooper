package domain

import (
	"time"

	"github.com/google/uuid"
)

// ImpactVerdict is how a signal, or a whole release, moved across a deploy.
type ImpactVerdict string

const (
	ImpactImproved ImpactVerdict = "improved"
	ImpactNeutral  ImpactVerdict = "neutral"
	ImpactWorse    ImpactVerdict = "worse"
	ImpactUnknown  ImpactVerdict = "unknown"
)

// WorstImpact is the overall verdict of several signals: the worst of the
// ones that could be judged, unknown only when none could.
func WorstImpact(verdicts []ImpactVerdict) ImpactVerdict {
	rank := map[ImpactVerdict]int{ImpactImproved: 1, ImpactNeutral: 2, ImpactWorse: 3}
	out, best := ImpactUnknown, 0
	for _, v := range verdicts {
		if r := rank[v]; r > best {
			out, best = v, r
		}
	}
	return out
}

// DefaultImpactWindow is how long before and after a deploy deploy impact
// compares.
const DefaultImpactWindow = 60 * time.Minute

// DeployImpact compares a release's component's golden signals in the window
// before its deploy with the window after it, and with the same after-window
// a day earlier. Pending: the after-window has not closed yet, so it is the
// part of it that has passed.
type DeployImpact struct {
	ReleaseID     uuid.UUID         `json:"release_id"`
	ComponentID   *uuid.UUID        `json:"component_id"`
	Environment   DeployEnvironment `json:"environment"`
	DeployedAt    *time.Time        `json:"deployed_at"`
	WindowMinutes int               `json:"window_minutes"`
	Pending       bool              `json:"pending"`
	Verdict       ImpactVerdict     `json:"verdict"`
	ComputedAt    time.Time         `json:"computed_at"`
	Signals       []SignalImpact    `json:"signals"`
}

// SignalImpact is one bound golden signal across the deploy: window means,
// the p95 for a quantile signal, of its worst series where it answers
// several. After leaves out the samples at or before the deploy (a rate's
// first one after it still straddles it); AfterPoints is how many it
// averaged. DeltaPct is nil when Before is zero.
type SignalImpact struct {
	Signal         string        `json:"signal"`
	Unit           string        `json:"unit"`
	Before         *float64      `json:"before"`
	After          *float64      `json:"after"`
	AfterPoints    int           `json:"after_points"`
	Baseline       *float64      `json:"baseline"`
	Delta          *float64      `json:"delta"`
	DeltaPct       *float64      `json:"delta_pct"`
	HigherIsBetter bool          `json:"higher_is_better"`
	Verdict        ImpactVerdict `json:"verdict"`
}

// NewDeployImpact is a release's deploy impact before any signal is read.
// The deploy is DeployedAt, or FinishedAt for a release that never deployed
// on its own (mode none); neither leaves it unanchored and unknown.
func NewDeployImpact(r Release, now time.Time) DeployImpact {
	out := DeployImpact{
		ReleaseID:     r.ID,
		ComponentID:   r.ComponentID,
		Environment:   EnvironmentProduction,
		WindowMinutes: int(DefaultImpactWindow / time.Minute),
		Verdict:       ImpactUnknown,
		ComputedAt:    now,
		Signals:       []SignalImpact{},
	}
	at := r.DeployedAt
	if at == nil {
		at = r.FinishedAt
	}
	if at != nil {
		t := at.UTC()
		out.DeployedAt = &t
		out.Pending = t.Add(DefaultImpactWindow).After(now)
	}
	return out
}

func (d DeployImpact) Signal(id string) (SignalImpact, bool) {
	for _, s := range d.Signals {
		if s.Signal == id {
			return s, true
		}
	}
	return SignalImpact{}, false
}

// ReleaseImpactCheck is what a release's soak read from its deploy impact
// against the delivery profile's impact thresholds. Unchecked names a
// threshold whose signal was not bound or not readable.
type ReleaseImpactCheck struct {
	CheckedAt time.Time             `json:"checked_at"`
	Pending   bool                  `json:"pending"`
	Verdict   ImpactVerdict         `json:"verdict"`
	Signals   []ReleaseImpactSignal `json:"signals"`
	Unchecked []string              `json:"unchecked,omitempty"`
}

// ReleaseImpactSignal: MaxDelta (percentage points) or MaxDeltaPct (percent)
// is the threshold this signal was held to, when one was. Settling: the
// after-window is still too short or too sparse to hold it to the threshold,
// so it cannot breach yet.
type ReleaseImpactSignal struct {
	Signal      string        `json:"signal"`
	Before      *float64      `json:"before"`
	After       *float64      `json:"after"`
	Delta       *float64      `json:"delta,omitempty"`
	DeltaPct    *float64      `json:"delta_pct,omitempty"`
	Verdict     ImpactVerdict `json:"verdict"`
	MaxDelta    *float64      `json:"max_delta,omitempty"`
	MaxDeltaPct *float64      `json:"max_delta_pct,omitempty"`
	Breached    bool          `json:"breached,omitempty"`
	Settling    bool          `json:"settling,omitempty"`
}

// Delivery profile thresholds the release soak holds a deploy's impact to.
const (
	VerifyMaxErrorRateDelta = "max_error_rate_delta"
	VerifyMaxP95DeltaPct    = "max_p95_delta_pct"
)
