package alerting

import (
	"math"
	"sort"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type sample struct {
	at    time.Time
	value float64
}

// verdict is a condition judged at one instant; a nil value is no data.
type verdict struct {
	value *float64
	holds bool
}

// samplesOf merges what a read answered into one series, keeping at each
// instant the worst value for the op — the highest, or the lowest for an op
// a falling value trips — so a query answering several series is judged by
// its worst one.
func samplesOf(series []domain.Series, op domain.AlertOp) []sample {
	byTime := map[int64]sample{}
	for _, sr := range series {
		for _, p := range sr.Points {
			if p.Value == nil {
				continue
			}
			v := *p.Value
			k := p.Time.UnixNano()
			cur, seen := byTime[k]
			if !seen || (op.LowIsWorse() && v < cur.value) || (!op.LowIsWorse() && v > cur.value) {
				byTime[k] = sample{at: p.Time, value: v}
			}
		}
	}
	out := make([]sample, 0, len(byTime))
	for _, s := range byTime {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// effectiveStep is the spacing the samples really have: never finer than
// asked, nor than the source said it answered at (CloudWatch's period), nor
// than the closest two samples — a source that keeps one point every five
// minutes is not missing four of them.
func effectiveStep(samples []sample, asked, answered time.Duration) time.Duration {
	step := max(asked, answered)
	var closest time.Duration
	for i := 1; i < len(samples); i++ {
		if gap := samples[i].at.Sub(samples[i-1].at); gap > 0 && (closest == 0 || gap < closest) {
			closest = gap
		}
	}
	return max(step, closest)
}

// pick answers an instant's value and the one a window before it: the
// latest sample at most 1.5 steps old (a source may label a period by its
// start, so the newest whole period sits a step back), and the sample before
// that one when it is at most 1.5 steps older. step is the samples'
// effective step.
func pick(samples []sample, at time.Time, step time.Duration) (cur, prev *sample) {
	tolerance := step + step/2
	i := sort.Search(len(samples), func(i int) bool { return samples[i].at.After(at) }) - 1
	if i < 0 || at.Sub(samples[i].at) > tolerance {
		return nil, nil
	}
	cur = &samples[i]
	if i > 0 && cur.at.Sub(samples[i-1].at) <= tolerance {
		prev = &samples[i-1]
	}
	return cur, prev
}

// judge: gt and lt compare the value; rising holds while the value is above
// the threshold and higher than a window earlier, and with no earlier value
// is no data, not a fall; the change ops compare the percent change from a
// window earlier, which needs a non-zero earlier value; no_data holds while
// there is no value.
func judge(c domain.AlertCondition, cur, prev *sample) verdict {
	if c.Op == domain.AlertOpNoData {
		if cur == nil {
			return verdict{holds: true}
		}
		v := cur.value
		return verdict{value: &v}
	}
	if cur == nil {
		return verdict{}
	}
	v := cur.value
	switch c.Op {
	case domain.AlertOpAbove:
		return verdict{value: &v, holds: v > c.Threshold}
	case domain.AlertOpBelow:
		return verdict{value: &v, holds: v < c.Threshold}
	case domain.AlertOpRising:
		if prev == nil {
			return verdict{}
		}
		return verdict{value: &v, holds: v > prev.value && v > c.Threshold}
	case domain.AlertOpChangePctAbove, domain.AlertOpChangePctBelow:
		if prev == nil || prev.value == 0 {
			return verdict{}
		}
		pct := (v - prev.value) / math.Abs(prev.value) * 100
		holds := pct > c.Threshold
		if c.Op == domain.AlertOpChangePctBelow {
			holds = pct < c.Threshold
		}
		return verdict{value: &pct, holds: holds}
	}
	return verdict{}
}

const (
	// minClearEvaluations and minClearDuration: a firing rule resolves only
	// after its condition read false this many evaluations in a row, and for
	// its own for or minClearDuration, whichever is longer — so a rule opening
	// an incident does not open a new one every few minutes.
	minClearEvaluations = 2
	minClearDuration    = 5 * time.Minute
	// noDataGrace: a firing rule keeps firing through this many evaluations
	// in a row that read nothing; the next one moves it to no_data. A sparse
	// source skipping a period is not a recovery.
	noDataGrace = 3
)

// standing is where a rule's evaluation stands: its state, since when, and
// while firing how long it has disagreed with firing.
type standing struct {
	state  domain.AlertState
	since  time.Time
	streak domain.AlertStreak
}

func standingOf(r domain.AlertRule) standing {
	return standing{state: r.State, since: r.StateSince, streak: r.AlertStreak}
}

// stored is the streak kept on the rule: it counts only while firing.
func (s standing) stored() domain.AlertStreak {
	if s.state != domain.AlertStateFiring {
		return domain.AlertStreak{}
	}
	return s.streak
}

// advance moves a rule by one verdict at now. A holding condition is pending
// until it has held for the condition's for, then firing; a false one is ok;
// no value is no_data, except for no_data rules, where it is the condition
// itself. Leaving firing takes longer than entering it: the condition must
// read false for at least minClearEvaluations evaluations in a row and for
// its for (an evaluation reading nothing in between neither counts nor
// breaks the run), and reading nothing moves it to no_data only past
// noDataGrace evaluations in a row; the standing leaving firing carries the
// streak that led out, for a recovery that has to be tried again. since
// moves only when the state does.
func advance(st standing, c domain.AlertCondition, v verdict, now time.Time) standing {
	noData := v.value == nil && c.Op != domain.AlertOpNoData
	if st.state == domain.AlertStateFiring {
		switch {
		case v.holds:
			return standing{state: st.state, since: st.since}
		case noData:
			st.streak.NoDataCount++
			if st.streak.NoDataCount <= noDataGrace {
				return st
			}
			return standing{state: domain.AlertStateNoData, since: now, streak: st.streak}
		}
		if st.streak.ClearCount == 0 || st.streak.ClearSince == nil {
			at := now
			st.streak.ClearSince = &at
		}
		st.streak.ClearCount++
		st.streak.NoDataCount = 0
		if st.streak.ClearCount >= minClearEvaluations && now.Sub(*st.streak.ClearSince) >= max(c.For(), minClearDuration) {
			return standing{state: domain.AlertStateOK, since: now, streak: st.streak}
		}
		return st
	}
	to := domain.AlertStateOK
	switch {
	case noData:
		to = domain.AlertStateNoData
	case v.holds:
		switch {
		case st.state == domain.AlertStatePending && now.Sub(st.since) >= c.For(),
			st.state != domain.AlertStatePending && c.For() <= 0:
			to = domain.AlertStateFiring
		default:
			to = domain.AlertStatePending
		}
	}
	if to == st.state {
		return standing{state: st.state, since: st.since}
	}
	return standing{state: to, since: now}
}

// worse reports whether v is a worse peak than peak for the op.
func worse(op domain.AlertOp, v, peak float64) bool {
	if op.LowIsWorse() {
		return v < peak
	}
	return v > peak
}
