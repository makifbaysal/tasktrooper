package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The mobile and crash reporting kinds read what the stores and Sentry
// already measure about an app, with nothing to deploy: Google Play's Android
// vitals, App Store Connect's Xcode Organizer metrics, and Sentry's sessions
// and issues. Each kind's query is one small JSON object.
const (
	DataSourceKindGooglePlayVitals DataSourceKind = "google_play_vitals"
	DataSourceKindAppStoreMetrics  DataSourceKind = "app_store_metrics"
	DataSourceKindSentry           DataSourceKind = "sentry"
)

// SignalErrorsIssues is the catalog signal the errors inbox reads through:
// a component's confirmed binding of it on a source whose kind lists error
// groups (port.ErrorsQuerier).
const SignalErrorsIssues = "errors.issues"

// Google Play vitals metric sets (Play Developer Reporting API v1beta1).
const (
	PlayCrashRateMetricSet     = "crashRateMetricSet"
	PlayANRRateMetricSet       = "anrRateMetricSet"
	PlaySlowStartRateMetricSet = "slowStartRateMetricSet"
	PlayErrorCountMetricSet    = "errorCountMetricSet"

	PlayPeriodDaily  = "daily"
	PlayPeriodHourly = "hourly"

	// PlayAsValue keeps the decimal Play answers; PlayAsPercent multiplies a
	// rate (a fraction of distinct users) by 100; PlayAsFreePercent is 100
	// minus that percent: the share of users without the event.
	PlayAsValue       = "value"
	PlayAsPercent     = "percent"
	PlayAsFreePercent = "free_percent"

	PlayIssuesAll      = "all"
	PlayIssuesCrash    = "crash"
	PlayIssuesANR      = "anr"
	PlayIssuesNonFatal = "non_fatal"

	DefaultPlayIssueDays = 7
	MaxPlayIssueDays     = 30
)

var (
	playMetricSets     = []string{PlayCrashRateMetricSet, PlayANRRateMetricSet, PlaySlowStartRateMetricSet, PlayErrorCountMetricSet}
	playHourlySets     = []string{PlayCrashRateMetricSet, PlayANRRateMetricSet}
	playAs             = []string{PlayAsValue, PlayAsPercent, PlayAsFreePercent}
	playIssueKinds     = []string{PlayIssuesAll, PlayIssuesCrash, PlayIssuesANR, PlayIssuesNonFatal}
	apiIdentifier      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)
	playPackagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)
)

// PlayVitalsQuery is the query language of the google_play_vitals kind:
//
//	{"package": "com.acme.app", "metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}
//
// A series names a metric set and one of its metrics. Dimension breaks it
// down (one series per value, labelled by the dimension: versionCode,
// deviceModel, countryCode, …), Filter is the API's own AIP-160 filter on
// the breakdown dimensions, Period is daily (the default; calendar days in
// America/Los_Angeles, Play's only daily time zone) or hourly (UTC; crash and
// ANR rates only, not their rolling averages), and As scales the decimal
// Play answers. A table instead names Issues: the app's top error issues
// (errorIssues:search) of the last Days, of one error type unless all.
// Package may be left out when the source's config names one.
type PlayVitalsQuery struct {
	Package   string `json:"package,omitempty"`
	MetricSet string `json:"metric_set,omitempty"`
	Metric    string `json:"metric,omitempty"`
	Dimension string `json:"dimension,omitempty"`
	Filter    string `json:"filter,omitempty"`
	Period    string `json:"period,omitempty"`
	As        string `json:"as,omitempty"`
	Issues    string `json:"issues,omitempty"`
	Days      int    `json:"days,omitempty"`
}

// IsTable reports whether the query reads error issues rather than a series.
func (q PlayVitalsQuery) IsTable() bool { return q.Issues != "" }

// ValidPlayPackage reports whether s is an Android package name, checked
// where a query's package (or the source's default) is read, not when the
// query is parsed, so a recipe validates with any sample value.
func ValidPlayPackage(s string) bool { return playPackagePattern.MatchString(s) }

func ParsePlayVitalsQuery(raw string) (PlayVitalsQuery, error) {
	var q PlayVitalsQuery
	if err := decodeBrokerQuery(raw, "Google Play vitals", &q); err != nil {
		return PlayVitalsQuery{}, err
	}
	if err := checkBrokerName("package", q.Package, false); err != nil {
		return PlayVitalsQuery{}, err
	}
	if err := checkBrokerName("filter", q.Filter, false); err != nil {
		return PlayVitalsQuery{}, err
	}
	switch {
	case q.Issues != "" && q.MetricSet != "":
		return PlayVitalsQuery{}, fmt.Errorf("give metric_set (a series) or issues (a table), not both")
	case q.Issues != "":
		return q, q.checkIssues()
	case q.MetricSet == "":
		return PlayVitalsQuery{}, fmt.Errorf("metric_set is required (one of %s), or issues for a table", strings.Join(playMetricSets, ", "))
	}
	return q, q.checkSeries()
}

func (q *PlayVitalsQuery) checkIssues() error {
	switch {
	case !slices.Contains(playIssueKinds, q.Issues):
		return fmt.Errorf("issues %q is not one of %s", q.Issues, strings.Join(playIssueKinds, ", "))
	case q.Metric != "" || q.Dimension != "" || q.Period != "" || q.As != "":
		return fmt.Errorf("an issues table takes no metric, dimension, period or as")
	case q.Days < 0 || q.Days > MaxPlayIssueDays:
		return fmt.Errorf("days must be 1 to %d", MaxPlayIssueDays)
	}
	if q.Days == 0 {
		q.Days = DefaultPlayIssueDays
	}
	return nil
}

func (q *PlayVitalsQuery) checkSeries() error {
	if q.Period == "" {
		q.Period = PlayPeriodDaily
	}
	if q.As == "" {
		q.As = PlayAsValue
	}
	switch {
	case !slices.Contains(playMetricSets, q.MetricSet):
		return fmt.Errorf("metric_set %q is not one of %s", q.MetricSet, strings.Join(playMetricSets, ", "))
	case !apiIdentifier.MatchString(q.Metric):
		return fmt.Errorf("metric %q is not a metric name of %s", q.Metric, q.MetricSet)
	case q.Dimension != "" && !apiIdentifier.MatchString(q.Dimension):
		return fmt.Errorf("dimension %q is not a dimension name", q.Dimension)
	case q.Period != PlayPeriodDaily && q.Period != PlayPeriodHourly:
		return fmt.Errorf("period %q is not daily or hourly", q.Period)
	case q.Period == PlayPeriodHourly && !slices.Contains(playHourlySets, q.MetricSet):
		return fmt.Errorf("%s has daily data only", q.MetricSet)
	case q.Period == PlayPeriodHourly && strings.HasSuffix(q.Metric, "UserWeighted"):
		return fmt.Errorf("%s is a rolling average, which Play answers daily only", q.Metric)
	case !slices.Contains(playAs, q.As):
		return fmt.Errorf("as %q is not one of %s", q.As, strings.Join(playAs, ", "))
	case q.Days != 0:
		return fmt.Errorf("days applies to an issues table only")
	}
	return nil
}

// App Store Connect Xcode metrics (perfPowerMetrics) and diagnostics.
const (
	ASCPercentileP50 = "p50"
	ASCPercentileP90 = "p90"
	DefaultASCDevice = "all_iphones"

	ASCDiagnosticsAll        = "all"
	ASCDiagnosticsHangs      = "hangs"
	ASCDiagnosticsLaunches   = "launches"
	ASCDiagnosticsDiskWrites = "disk_writes"
)

// ASCMetricCategories maps the lowercase category names a query may give as
// its metric to Apple's MetricCategory values.
var ASCMetricCategories = map[string]string{
	"launch": "LAUNCH", "hang": "HANG", "memory": "MEMORY", "disk": "DISK",
	"battery": "BATTERY", "termination": "TERMINATION", "terminations": "TERMINATION",
	"animation": "ANIMATION", "storage": "STORAGE",
}

var (
	ascDiagnostics   = []string{ASCDiagnosticsAll, ASCDiagnosticsHangs, ASCDiagnosticsLaunches, ASCDiagnosticsDiskWrites}
	ascAppIDPattern  = regexp.MustCompile(`^[0-9]{1,20}$`)
	ascDevicePattern = regexp.MustCompile(`^[A-Za-z0-9_,.-]{1,64}$`)
)

// AppStoreMetricsQuery is the query language of the app_store_metrics kind:
//
//	{"app": "1234567890", "metric": "launchTime", "percentile": "p90"}
//
// Metric is an Xcode Organizer metric identifier (launchTime, hangRate, …)
// or a whole category (launch, hang, memory, disk, battery, termination,
// animation, storage: every metric of it, one series each). Apple reports
// these per app version, not over time; Percentile keeps one of its two
// (p50, p90; absent: both), Device is its device filter (default
// all_iphones), Version keeps one app version. A table instead names
// Diagnostics (hangs, launches, disk_writes or all): one build's diagnostic
// signatures by weight — Build (an App Store Connect build id), else the
// newest build of Version, else the newest build. App may be left out when
// the source's config names one.
type AppStoreMetricsQuery struct {
	App         string `json:"app,omitempty"`
	Metric      string `json:"metric,omitempty"`
	Percentile  string `json:"percentile,omitempty"`
	Device      string `json:"device,omitempty"`
	Version     string `json:"version,omitempty"`
	Diagnostics string `json:"diagnostics,omitempty"`
	Build       string `json:"build,omitempty"`
}

func (q AppStoreMetricsQuery) IsTable() bool { return q.Diagnostics != "" }

// ValidASCAppID reports whether s is an App Store Connect app id (the number
// in the app's App Store URL), checked where it is read, as for
// ValidPlayPackage.
func ValidASCAppID(s string) bool { return ascAppIDPattern.MatchString(s) }

func ParseAppStoreMetricsQuery(raw string) (AppStoreMetricsQuery, error) {
	var q AppStoreMetricsQuery
	if err := decodeBrokerQuery(raw, "App Store Connect metrics", &q); err != nil {
		return AppStoreMetricsQuery{}, err
	}
	for _, f := range []struct{ key, v string }{{"app", q.App}, {"version", q.Version}, {"build", q.Build}} {
		if err := checkBrokerName(f.key, f.v, false); err != nil {
			return AppStoreMetricsQuery{}, err
		}
	}
	switch {
	case q.Diagnostics != "" && q.Metric != "":
		return AppStoreMetricsQuery{}, fmt.Errorf("give metric (a series) or diagnostics (a table), not both")
	case q.Diagnostics != "":
		if !slices.Contains(ascDiagnostics, q.Diagnostics) {
			return AppStoreMetricsQuery{}, fmt.Errorf("diagnostics %q is not one of %s", q.Diagnostics, strings.Join(ascDiagnostics, ", "))
		}
		if q.Percentile != "" || q.Device != "" {
			return AppStoreMetricsQuery{}, fmt.Errorf("a diagnostics table takes no percentile or device")
		}
		return q, nil
	case q.Metric == "":
		return AppStoreMetricsQuery{}, fmt.Errorf("metric is required (an Xcode metric such as launchTime, or a category such as hang), or diagnostics for a table")
	case !apiIdentifier.MatchString(q.Metric):
		return AppStoreMetricsQuery{}, fmt.Errorf("metric %q is not an Xcode metric or category name", q.Metric)
	case q.Percentile != "" && q.Percentile != ASCPercentileP50 && q.Percentile != ASCPercentileP90:
		return AppStoreMetricsQuery{}, fmt.Errorf("percentile %q is not p50 or p90", q.Percentile)
	case q.Device != "" && !ascDevicePattern.MatchString(q.Device):
		return AppStoreMetricsQuery{}, fmt.Errorf("device %q is not a device filter", q.Device)
	case q.Build != "":
		return AppStoreMetricsQuery{}, fmt.Errorf("build applies to a diagnostics table only")
	}
	if q.Device == "" {
		q.Device = DefaultASCDevice
	}
	return q, nil
}

// ASCCategory is the MetricCategory a query's metric names as a whole, or ""
// for a single metric identifier.
func (q AppStoreMetricsQuery) ASCCategory() string { return ASCMetricCategories[q.Metric] }

// Sentry metrics: the rates are percent (Sentry answers a fraction), counts
// are per interval, event_rate is error events per second.
const (
	SentryMetricCrashFreeUsers    = "crash_free_users"
	SentryMetricCrashFreeSessions = "crash_free_sessions"
	SentryMetricCrashRate         = "crash_rate"
	SentryMetricCrashRateUsers    = "crash_rate_users"
	SentryMetricSessions          = "sessions"
	SentryMetricUsers             = "users"
	SentryMetricEvents            = "events"
	SentryMetricEventRate         = "event_rate"

	DefaultSentryIssueSearch = "is:unresolved"
	DefaultSentryIssueDays   = 14
	MaxSentryIssueDays       = 90
)

var sentryMetrics = []string{
	SentryMetricCrashFreeUsers, SentryMetricCrashFreeSessions, SentryMetricCrashRate, SentryMetricCrashRateUsers,
	SentryMetricSessions, SentryMetricUsers, SentryMetricEvents, SentryMetricEventRate,
}

// SentryQuery is the query language of the sentry kind:
//
//	{"project": "ios-app", "metric": "crash_free_users", "environment": "production"}
//
// Metric (a series; default events) is crash_free_users, crash_free_sessions,
// crash_rate (sessions), crash_rate_users (percent), sessions, users (count
// per interval, from release health), events (error events per interval) or
// event_rate (per second). Environment and Release narrow every read; Search
// is Sentry search syntax added to an events read and to an issues read,
// whose own default is is:unresolved. An issues read (the errors inbox, a
// crashes table) ignores Metric; a table covers the last Days.
type SentryQuery struct {
	Project     string `json:"project"`
	Metric      string `json:"metric,omitempty"`
	Environment string `json:"environment,omitempty"`
	Release     string `json:"release,omitempty"`
	Search      string `json:"search,omitempty"`
	Days        int    `json:"days,omitempty"`
}

func ParseSentryQuery(raw string) (SentryQuery, error) {
	var q SentryQuery
	if err := decodeBrokerQuery(raw, "Sentry", &q); err != nil {
		return SentryQuery{}, err
	}
	for _, f := range []struct {
		key, v   string
		required bool
	}{{"project", q.Project, true}, {"environment", q.Environment, false}, {"release", q.Release, false}, {"search", q.Search, false}} {
		if err := checkBrokerName(f.key, f.v, f.required); err != nil {
			return SentryQuery{}, err
		}
	}
	if strings.ContainsFunc(q.Project, func(r rune) bool { return r == '/' || unicode.IsSpace(r) }) {
		return SentryQuery{}, fmt.Errorf("project %q is not a project slug or id", q.Project)
	}
	m, err := brokerMetric(q.Metric, SentryMetricEvents, sentryMetrics)
	if err != nil {
		return SentryQuery{}, err
	}
	q.Metric = m
	if q.Days < 0 || q.Days > MaxSentryIssueDays {
		return SentryQuery{}, fmt.Errorf("days must be 1 to %d", MaxSentryIssueDays)
	}
	if q.Days == 0 {
		q.Days = DefaultSentryIssueDays
	}
	return q, nil
}

// SentrySessionMetric reports whether metric is read from release health
// (sessions) rather than from error events.
func SentrySessionMetric(metric string) bool {
	return metric != SentryMetricEvents && metric != SentryMetricEventRate
}
