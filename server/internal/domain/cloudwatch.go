package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// CloudWatchQuery is the query language of the aws_cloudwatch kind: a JSON
// object naming either one metric
//
//	{"namespace": "AWS/SQS", "metric": "ApproximateNumberOfMessagesVisible",
//	 "stat": "Maximum", "dimensions": {"QueueName": "orders"}, "period": 60}
//
// or one expression over named metrics
//
//	{"expression": "100 * errors / invocations",
//	 "metrics": {"errors": {"namespace": …}, "invocations": {"namespace": …}}}
//
// The expression is anything GetMetricData evaluates as one: metric math over
// the metric ids, SEARCH(…), or a Metrics Insights SELECT (which needs no
// metrics). Period is in seconds; zero derives it from the step.
type CloudWatchQuery struct {
	CloudWatchMetric
	Expression string                      `json:"expression,omitempty"`
	Metrics    map[string]CloudWatchMetric `json:"metrics,omitempty"`
	Period     int                         `json:"period,omitempty"`
}

// CloudWatchMetric is one MetricStat: Stat is a statistic (Sum, Average,
// Maximum, …) or an extended one (p95, tm99, …).
type CloudWatchMetric struct {
	Namespace  string            `json:"namespace,omitempty"`
	Metric     string            `json:"metric,omitempty"`
	Stat       string            `json:"stat,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// MaxCloudWatchQueries is GetMetricData's limit on the queries of one
// request, the expression included.
const MaxCloudWatchQueries = 500

const (
	maxCloudWatchDimensions = 30
	maxCloudWatchName       = 255
	maxCloudWatchDimValue   = 1024
)

var (
	cloudWatchIDPattern   = regexp.MustCompile(`^[a-z][a-zA-Z0-9_]{0,254}$`)
	cloudWatchStatPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.:()%]{0,99}$`)
)

// ValidCloudWatchPeriod: GetMetricData takes 1, 5, 10 or 30 seconds for a
// high-resolution metric and any multiple of 60 otherwise.
func ValidCloudWatchPeriod(seconds int) bool {
	switch seconds {
	case 1, 5, 10, 30:
		return true
	}
	return seconds > 0 && seconds%60 == 0
}

// ParseCloudWatchQuery reads and checks one query; unknown keys are refused.
func ParseCloudWatchQuery(raw string) (CloudWatchQuery, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var q CloudWatchQuery
	if err := dec.Decode(&q); err != nil {
		return CloudWatchQuery{}, fmt.Errorf("not a CloudWatch query: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return CloudWatchQuery{}, errors.New("not a CloudWatch query: more than one JSON value")
	}
	if q.Period != 0 && !ValidCloudWatchPeriod(q.Period) {
		return CloudWatchQuery{}, fmt.Errorf("period %d is not 1, 5, 10, 30 or a multiple of 60", q.Period)
	}
	if strings.TrimSpace(q.Expression) == "" {
		if q.Expression != "" {
			return CloudWatchQuery{}, errors.New("expression is blank")
		}
		if len(q.Metrics) > 0 {
			return CloudWatchQuery{}, errors.New("metrics are named for an expression; this query has none")
		}
		if err := q.CloudWatchMetric.check(); err != nil {
			return CloudWatchQuery{}, err
		}
		return q, nil
	}
	if q.Namespace != "" || q.Metric != "" || q.Stat != "" || len(q.Dimensions) > 0 {
		return CloudWatchQuery{}, errors.New("a query with an expression names its metrics under metrics")
	}
	if len(q.Metrics)+1 > MaxCloudWatchQueries {
		return CloudWatchQuery{}, fmt.Errorf("more than %d metrics", MaxCloudWatchQueries-1)
	}
	for id, m := range q.Metrics {
		if !cloudWatchIDPattern.MatchString(id) {
			return CloudWatchQuery{}, fmt.Errorf("metric id %q must start with a lowercase letter and hold only letters, digits and _", id)
		}
		if err := m.check(); err != nil {
			return CloudWatchQuery{}, fmt.Errorf("metric %q: %w", id, err)
		}
	}
	return q, nil
}

func (m CloudWatchMetric) check() error {
	switch {
	case m.Namespace == "" || len(m.Namespace) > maxCloudWatchName:
		return errors.New("namespace is required")
	case m.Metric == "" || len(m.Metric) > maxCloudWatchName:
		return errors.New("metric is required")
	case !cloudWatchStatPattern.MatchString(m.Stat):
		return fmt.Errorf("stat %q is not a CloudWatch statistic", m.Stat)
	case len(m.Dimensions) > maxCloudWatchDimensions:
		return fmt.Errorf("more than %d dimensions", maxCloudWatchDimensions)
	}
	for name, value := range m.Dimensions {
		if name == "" || len(name) > maxCloudWatchName {
			return errors.New("a dimension has no name")
		}
		if value == "" || len(value) > maxCloudWatchDimValue {
			return fmt.Errorf("dimension %s has no value", name)
		}
	}
	return nil
}
