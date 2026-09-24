package cloud

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	maxFingerprintLen = 300
	maxSampleLines    = 20
)

var (
	uuidRe     = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	quotedRe   = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	hexRe      = regexp.MustCompile(`(?i)\b[0-9a-f]{8,}\b`)
	numberRe   = regexp.MustCompile(`\d+`)
	emailRe    = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	urlQueryRe = regexp.MustCompile(`(https?://[^\s?]+)\?[^\s]*`)
	spacesRe   = regexp.MustCompile(`\s+`)
)

// GroupErrors groups error-level log lines by a normalised message
// fingerprint, for providers with no native error-grouping API
// (port.CloudProvider.Errors returning port.ErrUnsupported). since is the
// start of the queried window: a group is New when it first appeared in the
// back half of that window rather than merely "inside" it, so a long-running
// error is not flagged as new just because the window happens to start
// before its next occurrence.
func GroupErrors(entries []domain.RuntimeLogEntry, since time.Time) []domain.RuntimeErrorGroup {
	if len(entries) == 0 {
		return nil
	}
	sorted := append([]domain.RuntimeLogEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })

	windowEnd := since
	for _, e := range sorted {
		if e.Timestamp.After(windowEnd) {
			windowEnd = e.Timestamp
		}
	}

	groups := map[string]*domain.RuntimeErrorGroup{}
	var order []string
	for _, e := range sorted {
		fp := fingerprint(e.Message)
		g, ok := groups[fp]
		if !ok {
			g = &domain.RuntimeErrorGroup{Fingerprint: fp, FirstSeen: e.Timestamp}
			groups[fp] = g
			order = append(order, fp)
		}
		g.Count++
		g.LastSeen = e.Timestamp
		g.Message = firstLineOf(e.Message)
		g.Sample = capLines(e.Message, maxSampleLines)
		g.Source = e.Source
	}

	mid := since.Add(windowEnd.Sub(since) / 2)
	out := make([]domain.RuntimeErrorGroup, 0, len(order))
	for _, fp := range order {
		g := *groups[fp]
		g.New = g.FirstSeen.After(mid)
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

func fingerprint(message string) string {
	normalized := normalizeMessage(firstLineOf(message))
	sum := sha1.Sum([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// normalizeMessage strips everything that makes two occurrences of the same
// error look different: ids, numbers, quoted values, query strings and
// emails. Order matters — a uuid or a quoted string must be swallowed whole
// before the looser hex/number patterns would otherwise fragment it.
func normalizeMessage(line string) string {
	s := strings.ToLower(line)
	s = urlQueryRe.ReplaceAllString(s, "$1?<query>")
	s = emailRe.ReplaceAllString(s, "<email>")
	s = uuidRe.ReplaceAllString(s, "<uuid>")
	s = quotedRe.ReplaceAllString(s, "<string>")
	s = hexRe.ReplaceAllString(s, "<hex>")
	s = numberRe.ReplaceAllString(s, "<num>")
	s = spacesRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if len(s) > maxFingerprintLen {
		s = s[:maxFingerprintLen]
	}
	return s
}

func firstLineOf(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}

func capLines(message string, n int) string {
	lines := strings.Split(message, "\n")
	if len(lines) <= n {
		return message
	}
	return strings.Join(lines[:n], "\n")
}
