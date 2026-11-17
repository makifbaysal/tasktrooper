package datasource

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// telemetryDialect is domain.TelemetryQuery JSON, held to the broker kinds'
// rules: a param is escaped for a JSON string and may sit only in a value
// naming what is read (service, env, endpoint). $__quantile is the one
// built-in, and only as the whole quantile value ("quantile":
// "$__quantile"); telemetry picks its own buckets, so $__rate and
// $__interval mean nothing. A detect metric is a label telemetry lists the
// values of (service.name, deployment.environment), as Discover lists them.
type telemetryDialect struct{}

const telemetryQuantileKey = "quantile"

var (
	telemetryNameKeys = []string{"service", "env", "endpoint"}
	// telemetryParamLabels: the label each name key's value is compared to,
	// for the detector's probe.
	telemetryParamLabels = map[string]string{
		"service": domain.TelemetryLabelService,
		"env":     domain.TelemetryLabelEnvironment,
	}
)

func (telemetryDialect) validMetricName(name string) bool {
	return name == domain.TelemetryLabelService || name == domain.TelemetryLabelEnvironment
}

func (d telemetryDialect) check(subject, query string, declared map[string]bool) []string {
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
			case name == builtinQuantile:
				if len(path) != 1 || path[0] != telemetryQuantileKey || s != m[0] {
					add("%s: $%s may appear only as the whole quantile value", subject, name)
				}
			case builtinPlaceholders[name]:
				add("%s: $%s has no meaning in a TaskTrooper Telemetry query; telemetry picks its own buckets", subject, name)
			case !declared[name]:
				add("%s uses $%s, which is neither a declared param nor a built-in", subject, name)
			case len(path) != 1 || !slices.Contains(telemetryNameKeys, path[0]):
				add("%s: $%s may appear only in the %s value", subject, name, strings.Join(telemetryNameKeys, ", "))
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
		_, err = domain.ParseTelemetryQuery(rendered)
	}
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", subject, err)}
	}
	return nil
}

func (telemetryDialect) render(query string, params map[string]string, _ time.Duration, quantile float64) (string, error) {
	return expand(query, params, func(name string) (string, bool) {
		if name == builtinQuantile {
			return strconv.FormatFloat(math.Round(quantile*10000)/10000, 'f', -1, 64), true
		}
		return "", false
	}, escapeJSONString)
}

// paramFields reads only a top-level value that is one whole param.
func (telemetryDialect) paramFields(query string) []ParamField {
	var doc map[string]any
	if json.Unmarshal([]byte(query), &doc) != nil {
		return nil
	}
	var out []ParamField
	for _, key := range telemetryNameKeys {
		label, ok := telemetryParamLabels[key]
		s, isString := doc[key].(string)
		if !ok || !isString {
			continue
		}
		if m := placeholderRegex.FindStringSubmatch(s); m != nil && m[0] == s {
			out = append(out, ParamField{Param: m[1], Field: label})
		}
	}
	return out
}
