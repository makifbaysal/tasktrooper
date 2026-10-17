package datasource

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ParamField is one param a query compares a label, field or tag to for
// equality, and that label: what the detector lists the values of.
type ParamField struct {
	Param string
	Field string
}

// paramFielder is a dialect whose equality comparisons the detector can probe.
type paramFielder interface {
	paramFields(query string) []ParamField
}

// ParamFields lists, in query order, the params a query of kind compares a
// label, field or tag to with equality. A language the detector has no probe
// for (CloudWatch, the broker JSON) answers none.
func ParamFields(kind domain.DataSourceKind, query string) []ParamField {
	if f, ok := dialectFor(kind).(paramFielder); ok {
		return f.paramFields(query)
	}
	return nil
}

var (
	promEqualityMatcher = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"\$([A-Za-z_][A-Za-z0-9_]*)"`)

	lokiLabelPattern       = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	traceQLTagPattern      = regexp.MustCompile(`^(\.|(resource|span|event|link|instrumentation)\.)[A-Za-z0-9_.:/-]+$`)
	traceQLEqualityMatcher = regexp.MustCompile(`((?:resource|span|event|link|instrumentation)?\.[A-Za-z0-9_.:/-]+)\s*=\s*"\$([A-Za-z_][A-Za-z0-9_]*)"`)
)

func (promQLDialect) paramFields(query string) []ParamField {
	return matchedFields(promEqualityMatcher, query)
}

func matchedFields(re *regexp.Regexp, query string) []ParamField {
	var out []ParamField
	for _, m := range re.FindAllStringSubmatch(query, -1) {
		out = append(out, ParamField{Param: m[2], Field: m[1]})
	}
	return out
}

// logQLDialect is LogQL, lexically PromQL's: a param sits only inside a
// double-quoted stream selector value matched with = or !=, never in a line
// filter or a label filter stage, where it would be a pattern or an
// expression; $__rate, $__interval and $__quantile go where PromQL takes
// them, for a metric query. A detect metric is a label name, which is what
// the loki kind's Discover lists.
type logQLDialect struct{ promQLDialect }

func (logQLDialect) validMetricName(name string) bool { return lokiLabelPattern.MatchString(name) }

// traceQLDialect is TraceQL: a param is escaped for a double-quoted string
// and may sit only as the value an attribute is compared to with = or !=
// inside {...}. No built-in means anything in a trace search. A detect metric
// is a scoped tag ("resource.service.name"), as the tempo kind lists them.
type traceQLDialect struct{}

func (traceQLDialect) validMetricName(name string) bool { return traceQLTagPattern.MatchString(name) }

func (traceQLDialect) check(subject, query string, declared map[string]bool) []string {
	var problems []string
	seen := map[string]bool{}
	for _, use := range placeholderUses(query) {
		if seen[use.name] || use.harmless(declared) {
			continue
		}
		seen[use.name] = true
		switch {
		case builtinPlaceholders[use.name]:
			problems = append(problems, fmt.Sprintf("%s: $%s has no meaning in a TraceQL search", subject, use.name))
		case !declared[use.name]:
			problems = append(problems, fmt.Sprintf("%s uses $%s, which is neither a declared param nor a built-in", subject, use.name))
		case !use.inEqualityMatcher():
			problems = append(problems, fmt.Sprintf("%s: $%s must sit inside a double-quoted value compared with = or != inside {...}", subject, use.name))
		}
	}
	return problems
}

func (traceQLDialect) render(query string, params map[string]string, _ time.Duration, _ float64) (string, error) {
	return expand(query, params, func(string) (string, bool) { return "", false }, escapeQuoted)
}

func (traceQLDialect) paramFields(query string) []ParamField {
	return matchedFields(traceQLEqualityMatcher, query)
}

// elasticsearchDialect is domain.ElasticsearchQuery JSON. A param is escaped
// for a JSON string and may sit only in a filter's value: in a key it would
// name the field. No built-in means anything in a log search.
type elasticsearchDialect struct{}

const elasticsearchFilters = "filters"

func (elasticsearchDialect) validMetricName(name string) bool {
	return domain.ValidElasticsearchField(name)
}

func (d elasticsearchDialect) check(subject, query string, declared map[string]bool) []string {
	var problems []string
	seen := map[string]bool{}
	add := func(format string, args ...any) {
		p := fmt.Sprintf(format, args...)
		if !seen[p] {
			seen[p] = true
			problems = append(problems, p)
		}
	}
	for _, name := range placeholdersOutsideJSONStrings(query) {
		add("%s: $%s must sit inside a JSON string", subject, name)
	}
	if len(problems) > 0 {
		return problems
	}
	var doc any
	dec := json.NewDecoder(strings.NewReader(query))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return []string{fmt.Sprintf("%s is not JSON: %v", subject, err)}
	}
	walkJSONStrings(doc, nil, func(path []string, s string, isKey bool) {
		for _, m := range placeholderRegex.FindAllStringSubmatch(s, -1) {
			name := m[1]
			switch {
			case isKey:
				add("%s: $%s may not sit in a JSON key", subject, name)
			case builtinPlaceholders[name]:
				add("%s: $%s has no meaning in an Elasticsearch query", subject, name)
			case !declared[name]:
				add("%s uses $%s, which is neither a declared param nor a built-in", subject, name)
			case len(path) != 2 || path[0] != elasticsearchFilters:
				add("%s: $%s may appear only in a filter value", subject, name)
			}
		}
	})
	if len(problems) > 0 {
		return problems
	}
	sample := map[string]string{}
	for name := range declared {
		sample[name] = "sample"
	}
	rendered, err := d.render(query, sample, time.Minute, DefaultQuantile)
	if err == nil {
		_, err = domain.ParseElasticsearchQuery(rendered)
	}
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", subject, err)}
	}
	return nil
}

func (elasticsearchDialect) render(query string, params map[string]string, _ time.Duration, _ float64) (string, error) {
	return expand(query, params, func(string) (string, bool) { return "", false }, escapeJSONString)
}

// paramFields reads only a filter whose whole value is one param: the probe
// lists a field's values, which a value with more around the param is not.
func (elasticsearchDialect) paramFields(query string) []ParamField {
	var doc struct {
		Filters map[string]string `json:"filters"`
	}
	if json.Unmarshal([]byte(query), &doc) != nil {
		return nil
	}
	var out []ParamField
	for _, field := range sortedKeys(doc.Filters) {
		if m := placeholderRegex.FindStringSubmatch(doc.Filters[field]); m != nil && m[0] == doc.Filters[field] {
			out = append(out, ParamField{Param: m[1], Field: field})
		}
	}
	return out
}
