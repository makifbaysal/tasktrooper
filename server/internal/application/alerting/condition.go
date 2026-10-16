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

// pick answers an instant's value and the one a window before it: the
// latest sample at most 1.5 steps old (a source may label a period by its
// start, so the newest whole period sits a step back), and the sample before
// that one when it is at most 1.5 steps older.
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
// the threshold and higher than a window earlier; the change ops compare the
// percent change from a window earlier, which needs a non-zero earlier
// value; no_data holds while there is no value.
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
		return verdict{value: &v, holds: prev != nil && v > prev.value && v > c.Threshold}
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

// advance moves a rule's state by one verdict at now: a holding condition is
// pending until it has held for the condition's for, then firing; a false
// one is ok; no value is no_data, except for no_data rules, where it is the
// condition itself. since moves only when the state does.
func advance(state domain.AlertState, since time.Time, c domain.AlertCondition, v verdict, now time.Time) (domain.AlertState, time.Time) {
	to := domain.AlertStateOK
	switch {
	case v.value == nil && c.Op != domain.AlertOpNoData:
		to = domain.AlertStateNoData
	case v.holds:
		switch {
		case state == domain.AlertStateFiring,
			state == domain.AlertStatePending && now.Sub(since) >= c.For(),
			state != domain.AlertStatePending && c.For() <= 0:
			to = domain.AlertStateFiring
		default:
			to = domain.AlertStatePending
		}
	}
	if to == state {
		return state, since
	}
	return to, now
}

// worse reports whether v is a worse peak than peak for the op.
func worse(op domain.AlertOp, v, peak float64) bool {
	if op.LowIsWorse() {
		return v < peak
	}
	return v > peak
}
