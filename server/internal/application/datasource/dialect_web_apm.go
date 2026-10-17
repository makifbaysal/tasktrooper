package datasource

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The web analytics kinds speak domain.WebQuery JSON, held to the broker
// kinds' rules: a param may sit only in a value naming what is read (the
// host, the event, GA4's property, Plausible's site), never in the metric, a
// table name or a key, and no built-in applies, since each kind picks the
// interval its API answers at. A detect metric is an event name, as the
// kinds' Discover lists them ("$pageview", GA4's "page_view").
var (
	eventInventoryPattern = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,200}$`)

	webNoBuiltins = "the kind picks the interval from the step"

	postHogDialect = brokerDialect{
		label: "PostHog", nameKeys: []string{"host", "event"}, inventory: eventInventoryPattern,
		noBuiltins: webNoBuiltins,
		parse:      func(q string) error { _, err := domain.ParseWebQuery(domain.DataSourceKindPostHog, q); return err },
	}
	ga4Dialect = brokerDialect{
		label: "Google Analytics", nameKeys: []string{"property", "host", "event"}, inventory: eventInventoryPattern,
		noBuiltins: webNoBuiltins,
		parse:      func(q string) error { _, err := domain.ParseWebQuery(domain.DataSourceKindGA4, q); return err },
	}
	plausibleDialect = brokerDialect{
		label: "Plausible", nameKeys: []string{"site", "host"}, inventory: eventInventoryPattern,
		noBuiltins: webNoBuiltins,
		parse:      func(q string) error { _, err := domain.ParseWebQuery(domain.DataSourceKindPlausible, q); return err },
	}
)

var (
	datadogMetricPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{0,199}$`)
	datadogTagValuePattern = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,200}$`)
	datadogFragmentPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$`)
	nrqlEventTypePattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,254}$`)
)

const maxDatadogFragment = 200

// datadogDialect is a Datadog metric query (avg:system.cpu.user{service:x}
// by {host}). Datadog has no string quoting a value could be escaped for, so
// a param's value is checked instead of escaped: as a whole tag value inside
// {...} (key:$param) it must be a tag value (letters, digits, _ . : / -);
// outside {...} it may only be part of a metric name ($prefix.hits,
// trace.$span.errors), where it must be dotted name segments. $__quantile is
// the percentile aggregator, right before a ':' (p95:trace.http.request);
// $__interval is the step in whole seconds, where a number goes
// (.rollup(sum, $__interval)); $__rate is refused, as .as_rate() is
// Datadog's own. A detect metric is a metric name, as Discover lists them.
type datadogDialect struct{}

func (datadogDialect) validMetricName(name string) bool {
	return datadogMetricPattern.MatchString(name)
}

type datadogUse struct {
	name       string
	start, end int
	inBraces   bool
}

func datadogUses(q string) []datadogUse {
	var uses []datadogUse
	braces := 0
	for i := 0; i < len(q); i++ {
		switch q[i] {
		case '{':
			braces++
		case '}':
			braces = max(braces-1, 0)
		case '$':
			if name := identAt(q, i+1); name != "" {
				uses = append(uses, datadogUse{name: name, start: i, end: i + 1 + len(name), inBraces: braces > 0})
				i += len(name)
			}
		}
	}
	return uses
}

func charAt(q string, i int) byte {
	if i < 0 || i >= len(q) {
		return 0
	}
	return q[i]
}

func (u datadogUse) isTagValue(q string) bool {
	next := charAt(q, u.end)
	return u.inBraces && charAt(q, u.start-1) == ':' && (next == ',' || next == '}' || next == ' ' || next == 0)
}

func (u datadogUse) inMetricName(q string) bool {
	next := charAt(q, u.end)
	return !u.inBraces && (next == '.' || next == '{' || charAt(q, u.start-1) == '.')
}

func (u datadogUse) asNumber(q string) bool {
	prev, next := charAt(q, u.start-1), charAt(q, u.end)
	return !u.inBraces && strings.IndexByte("(, ", prev) >= 0 && strings.IndexByte("), ", next) >= 0
}

func (datadogDialect) check(subject, query string, declared map[string]bool) []string {
	var problems []string
	seen := map[string]bool{}
	add := func(format string, args ...any) {
		p := fmt.Sprintf(format, args...)
		if !seen[p] {
			seen[p] = true
			problems = append(problems, p)
		}
	}
	for _, u := range datadogUses(query) {
		switch {
		case u.name == builtinQuantile:
			if u.inBraces || charAt(query, u.end) != ':' {
				add("%s: $%s may appear only as the aggregator, right before a ':' ($__quantile:trace.http.request{…})", subject, u.name)
			}
		case u.name == builtinInterval:
			if !u.asNumber(query) {
				add("%s: $%s may appear only where a number goes, outside {...}", subject, u.name)
			}
		case u.name == builtinRate:
			add("%s: $%s has no meaning in a Datadog query; use .as_rate()", subject, u.name)
		case !declared[u.name]:
			add("%s uses $%s, which is neither a declared param nor a built-in", subject, u.name)
		case !u.isTagValue(query) && !u.inMetricName(query):
			add("%s: $%s may appear only as a whole tag value inside {...} (service:$%s) or as part of a metric name", subject, u.name, u.name)
		}
	}
	return problems
}

func (datadogDialect) render(query string, params map[string]string, step time.Duration, quantile float64) (string, error) {
	var b strings.Builder
	last := 0
	var missing, refused []string
	for _, u := range datadogUses(query) {
		b.WriteString(query[last:u.start])
		last = u.end
		switch u.name {
		case builtinQuantile:
			b.WriteString(percentileAggregator(quantile))
			continue
		case builtinInterval:
			b.WriteString(strconv.FormatInt(max(int64(step/time.Second), 1), 10))
			continue
		}
		v, ok := params[u.name]
		switch {
		case !ok:
			missing = append(missing, u.name)
		case u.isTagValue(query) && !datadogTagValuePattern.MatchString(v):
			refused = append(refused, fmt.Sprintf("the value of %s is not a Datadog tag value (letters, digits, _ . : / -)", u.name))
		case !u.isTagValue(query) && (len(v) > maxDatadogFragment || !datadogFragmentPattern.MatchString(v)):
			refused = append(refused, fmt.Sprintf("the value of %s is not part of a Datadog metric name (letters, digits, _ and dots)", u.name))
		}
		b.WriteString(v)
	}
	b.WriteString(query[last:])
	switch {
	case len(missing) > 0:
		return "", fmt.Errorf("%w: no value for %s", ErrInvalidInput, strings.Join(missing, ", "))
	case len(refused) > 0:
		return "", fmt.Errorf("%w: %s", ErrInvalidInput, strings.Join(refused, "; "))
	}
	return b.String(), nil
}

// percentileAggregator is the quantile as Datadog's pNN aggregator; a
// distribution answers p50, p75, p90, p95, p99 and the like.
func percentileAggregator(q float64) string {
	return "p" + strconv.FormatFloat(math.Round(q*10000)/100, 'f', -1, 64)
}

// nrqlDialect is NRQL. A param may sit only inside a single-quoted string
// compared with = or != (appName = '$app'). NRQL's escaping inside a string
// is not documented, so a value holding a quote, a backslash or a control
// character is refused rather than escaped. $__quantile is a percent where a
// number goes, outside strings (percentile(duration, $__quantile) → 95);
// $__rate and $__interval are refused: the kind appends SINCE, UNTIL and
// TIMESERIES itself. A detect metric is an event type (Transaction), as
// Discover lists them.
type nrqlDialect struct{}

func (nrqlDialect) validMetricName(name string) bool { return nrqlEventTypePattern.MatchString(name) }

type nrqlUse struct {
	name       string
	start, end int
	quote      byte
	op         string
}

func nrqlUses(q string) []nrqlUse {
	var uses []nrqlUse
	for i := 0; i < len(q); i++ {
		switch c := q[i]; c {
		case '\'', '"', '`':
			op := matcherOpBefore(q, i)
			j := i + 1
			for ; j < len(q) && q[j] != c; j++ {
				if q[j] == '\\' && c != '`' {
					j++
					continue
				}
				if q[j] == '$' {
					if name := identAt(q, j+1); name != "" {
						uses = append(uses, nrqlUse{name: name, start: j, end: j + 1 + len(name), quote: c, op: op})
						j += len(name)
					}
				}
			}
			i = j
		case '$':
			if name := identAt(q, i+1); name != "" {
				uses = append(uses, nrqlUse{name: name, start: i, end: i + 1 + len(name)})
				i += len(name)
			}
		}
	}
	return uses
}

func (nrqlDialect) check(subject, query string, declared map[string]bool) []string {
	var problems []string
	seen := map[string]bool{}
	add := func(format string, args ...any) {
		p := fmt.Sprintf(format, args...)
		if !seen[p] {
			seen[p] = true
			problems = append(problems, p)
		}
	}
	for _, u := range nrqlUses(query) {
		switch {
		case u.name == builtinQuantile:
			if u.quote != 0 || isIdentByte(charAt(query, u.start-1)) || isIdentByte(charAt(query, u.end)) {
				add("%s: $%s may appear only where a number goes, outside strings", subject, u.name)
			}
		case builtinPlaceholders[u.name]:
			add("%s: $%s has no meaning in NRQL; the kind adds SINCE, UNTIL and TIMESERIES", subject, u.name)
		case !declared[u.name]:
			add("%s uses $%s, which is neither a declared param nor a built-in", subject, u.name)
		case u.quote != '\'' || (u.op != "=" && u.op != "!="):
			add("%s: $%s must sit inside a single-quoted string compared with = or !=", subject, u.name)
		}
	}
	return problems
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (nrqlDialect) render(query string, params map[string]string, _ time.Duration, quantile float64) (string, error) {
	var b strings.Builder
	last := 0
	var missing, refused []string
	for _, u := range nrqlUses(query) {
		b.WriteString(query[last:u.start])
		last = u.end
		if u.name == builtinQuantile && u.quote == 0 {
			b.WriteString(strconv.FormatFloat(math.Round(quantile*10000)/100, 'f', -1, 64))
			continue
		}
		v, ok := params[u.name]
		switch {
		case !ok:
			missing = append(missing, u.name)
		case strings.ContainsAny(v, `'\`) || strings.ContainsFunc(v, unicode.IsControl):
			refused = append(refused, fmt.Sprintf("the value of %s holds a quote, a backslash or a control character, which NRQL cannot take", u.name))
		}
		b.WriteString(v)
	}
	b.WriteString(query[last:])
	switch {
	case len(missing) > 0:
		return "", fmt.Errorf("%w: no value for %s", ErrInvalidInput, strings.Join(missing, ", "))
	case len(refused) > 0:
		return "", fmt.Errorf("%w: %s", ErrInvalidInput, strings.Join(refused, "; "))
	}
	return b.String(), nil
}
