package datasource

import (
	"fmt"
	"strings"
	"time"
)

const (
	builtinRate     = "__rate"
	builtinInterval = "__interval"
)

var builtinPlaceholders = map[string]bool{builtinRate: true, builtinInterval: true}

// minRateWindow keeps two samples inside a rate() window at a 60 s scrape
// interval (Grafana Cloud's default); a shorter window returns no data there.
const minRateWindow = 2 * time.Minute

// RateWindow is what $__rate expands to for a step.
func RateWindow(step time.Duration) time.Duration {
	return max(2*step, minRateWindow)
}

// RenderQuery expands a recipe query for one step. Param values are escaped
// for the inside of a double-quoted string literal, which is where recipes put
// them (PromQL label matchers).
func RenderQuery(query string, params map[string]string, step time.Duration) (string, error) {
	var missing []string
	out := placeholderRegex.ReplaceAllStringFunc(query, func(m string) string {
		name := m[1:]
		switch name {
		case builtinRate:
			return promDuration(RateWindow(step))
		case builtinInterval:
			return promDuration(step)
		}
		v, ok := params[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return escapeQuoted(v)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%w: no value for %s", ErrInvalidInput, strings.Join(missing, ", "))
	}
	return out, nil
}

func promDuration(d time.Duration) string {
	return fmt.Sprintf("%ds", max(int64(d/time.Second), 1))
}

var quotedEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)

func escapeQuoted(s string) string { return quotedEscaper.Replace(s) }
