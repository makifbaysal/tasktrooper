package domain

import "strings"

// RunGateRejectionPrefixes are the sentence-openers board.Runner writes to a
// failed run's Summary when a grounding gate rejects it for skipping required
// verification rather than for an infra failure. Must stay in sync with the
// reasons runner.go actually writes.
var RunGateRejectionPrefixes = []string{
	"Analysis rejected:",
	"QA round rejected:",
}

// IsRunGateRejection reports whether a failed run's summary names a
// grounding-gate rejection, as opposed to an infra failure (session limit,
// process restart) that happens to also leave the run failed.
func IsRunGateRejection(summary string) bool {
	for _, prefix := range RunGateRejectionPrefixes {
		if strings.HasPrefix(summary, prefix) {
			return true
		}
	}
	return false
}
