package live

import (
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Staleness is how far before now a window may end and still be answered with
// the live value: a caller that rounds its window down to the minute, or an
// alert reading [now − 2·window, now], still gets it.
const Staleness = 5 * time.Minute

// Instant is when a read ending at to is answered: now, or to when that is
// earlier. ok is false for a window that ended more than Staleness ago, which
// only a store of history could answer (the window before, in a comparison).
func Instant(to, now time.Time) (at time.Time, ok bool) {
	if to.IsZero() || to.After(now) {
		return now, true
	}
	if now.Sub(to) > Staleness {
		return time.Time{}, false
	}
	return to, true
}

// PastWindow is the warning for a window Instant cannot answer.
func PastWindow(kind string) string {
	return "a " + kind + " source reads only the live value, so a window that ended earlier has no data; chart history needs an exporter-backed source"
}

// Point is one sample at t; a nil v is a gap (the broker reported no value).
func Point(t time.Time, v *float64) []domain.SeriesPoint {
	if v == nil {
		return []domain.SeriesPoint{{Time: t}}
	}
	return []domain.SeriesPoint{domain.FinitePoint(t, *v)}
}

func Float(v int64) *float64 {
	f := float64(v)
	return &f
}
