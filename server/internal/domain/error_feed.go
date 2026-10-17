package domain

import "time"

// ErrorsQuery is one read of a kind's error groups (Sentry issues). Query is
// the kind's own query text, already rendered (an errors.issues binding's);
// the groups are those seen in [Since, Until], at most Limit, the most
// frequent first. A group is New when its first event falls in the window.
type ErrorsQuery struct {
	Query string
	Since time.Time
	Until time.Time
	Limit int
}
