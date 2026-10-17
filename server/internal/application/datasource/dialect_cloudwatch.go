package datasource

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// cloudWatchMetricPattern is how the aws_cloudwatch kind lists a metric:
// "<namespace>/<metric name>", e.g. AWS/SQS/ApproximateNumberOfMessagesVisible.
var cloudWatchMetricPattern = regexp.MustCompile(`^[A-Za-z0-9_.#:/-]+/[A-Za-z0-9_.#:-]+$`)

// cloudWatchDialect is domain.CloudWatchQuery JSON. A param is escaped for a
// JSON string and may sit only in a dimension value: anywhere else (a
// namespace, a stat, the expression) it would be a name or metric math of its
// own. $__quantile may only be a whole stat, which it turns into the
// percentile statistic ("p95"). $__rate and $__interval mean nothing here: the
// kind derives the period from the step.
type cloudWatchDialect struct{}

func (cloudWatchDialect) validMetricName(name string) bool {
	return cloudWatchMetricPattern.MatchString(name)
}

func (d cloudWatchDialect) check(subject, query string, declared map[string]bool) []string {
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
				if !isStatPath(path) || s != "$"+builtinQuantile {
					add("%s: $%s may appear only as a whole stat value", subject, name)
				}
			case builtinPlaceholders[name]:
				add("%s: $%s has no meaning in a CloudWatch query; the period follows the step", subject, name)
			case !declared[name]:
				add("%s uses $%s, which is neither a declared param nor a built-in", subject, name)
			case !isDimensionValuePath(path):
				add("%s: $%s may appear only in a dimension value", subject, name)
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
		_, err = domain.ParseCloudWatchQuery(rendered)
	}
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", subject, err)}
	}
	return nil
}

func (cloudWatchDialect) render(query string, params map[string]string, _ time.Duration, quantile float64) (string, error) {
	return expand(query, params, func(name string) (string, bool) {
		if name == builtinQuantile {
			return percentileStat(quantile), true
		}
		return "", false
	}, escapeJSONString)
}

// percentileStat rounds to two decimals of a percent, CloudWatch's precision
// for pNN.NN.
func percentileStat(q float64) string {
	return "p" + strconv.FormatFloat(math.Round(q*10000)/100, 'f', -1, 64)
}

func escapeJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func isStatPath(path []string) bool {
	return (len(path) == 1 && path[0] == "stat") ||
		(len(path) == 3 && path[0] == "metrics" && path[2] == "stat")
}

func isDimensionValuePath(path []string) bool {
	return (len(path) == 2 && path[0] == "dimensions") ||
		(len(path) == 4 && path[0] == "metrics" && path[2] == "dimensions")
}

func walkJSONStrings(v any, path []string, fn func(path []string, s string, isKey bool)) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			fn(path, k, true)
			walkJSONStrings(child, append(append([]string(nil), path...), k), fn)
		}
	case []any:
		for i, child := range t {
			walkJSONStrings(child, append(append([]string(nil), path...), strconv.Itoa(i)), fn)
		}
	case string:
		fn(path, t, false)
	}
}

func placeholdersOutsideJSONStrings(q string) []string {
	var names []string
	inString := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case !inString && c == '$':
			if name := identAt(q, i+1); name != "" {
				names = append(names, name)
				i += len(name)
			}
		}
	}
	return names
}
