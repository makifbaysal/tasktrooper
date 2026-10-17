package datasource

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxDefaultLen = 256

// checkDefaults holds each default to a declared param and renders every
// query with them, so a default that cannot sit where its param goes (a
// Datadog metric name fragment with a space) fails the recipe, not a read.
func checkDefaults(r domain.Recipe, lang dialect, declared map[string]bool) []string {
	if len(r.Defaults) == 0 {
		return nil
	}
	var problems []string
	for _, p := range slices.Sorted(maps.Keys(r.Defaults)) {
		v := r.Defaults[p]
		switch {
		case !declared[p]:
			problems = append(problems, fmt.Sprintf("defaults.%s is not a declared param", p))
		case strings.TrimSpace(v) == "":
			problems = append(problems, fmt.Sprintf("defaults.%s is empty", p))
		case len(v) > maxDefaultLen || strings.ContainsFunc(v, unicode.IsControl):
			problems = append(problems, fmt.Sprintf("defaults.%s is not a usable value", p))
		}
	}
	if len(problems) > 0 {
		return problems
	}
	params := map[string]string{}
	for p := range declared {
		params[p] = "sample"
	}
	maps.Copy(params, r.Defaults)
	queries := map[string]string{}
	for id, q := range r.Signals {
		queries[fmt.Sprintf("signal %q", id)] = q
	}
	for name, t := range r.Tables {
		for col, q := range t.Queries {
			queries[fmt.Sprintf("table %q column %q", name, col)] = q
		}
	}
	for _, subject := range slices.Sorted(maps.Keys(queries)) {
		if _, err := lang.render(queries[subject], params, time.Minute, DefaultQuantile); err != nil {
			problems = append(problems, fmt.Sprintf("%s with its defaults: %v", subject, err))
		}
	}
	return problems
}
