package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// The web analytics kinds (PostHog, Google Analytics 4, Plausible) read what
// a site's own analytics already counts; the APM kinds (Datadog, New Relic)
// read a hosted APM's metrics in its own query language.
const (
	DataSourceKindPostHog   DataSourceKind = "posthog"
	DataSourceKindGA4       DataSourceKind = "ga4"
	DataSourceKindPlausible DataSourceKind = "plausible"
	DataSourceKindDatadog   DataSourceKind = "datadog"
	DataSourceKindNewRelic  DataSourceKind = "newrelic"
	DataSourceKindVercel    DataSourceKind = "vercel"
)

// The per-entity tables a frontend's web view reads (RecipeTablePages one row
// per path, RecipeTableReferrers per referring source, RecipeTableCountries
// per country, RecipeTableDevices per device, browser and OS) and the one a
// backend's endpoints view reads (RecipeTableEndpoints one row per route or
// transaction: endpoint, rpm, error_pct, p95_ms).
const (
	RecipeTablePages     = "pages"
	RecipeTableReferrers = "referrers"
	RecipeTableCountries = "countries"
	RecipeTableDevices   = "devices"
	RecipeTableEndpoints = "endpoints"
)

func WebTables() []string {
	return []string{RecipeTablePages, RecipeTableReferrers, RecipeTableCountries, RecipeTableDevices}
}

// Web analytics metrics. page_views and js_errors are per second (the
// signals' views/s and errors/s); sessions, visitors and events are counts
// per interval; the vitals are a percentile of the interval's measurements,
// in ms (cls a score).
const (
	WebMetricPageViews = "page_views"
	WebMetricSessions  = "sessions"
	WebMetricVisitors  = "visitors"
	WebMetricJSErrors  = "js_errors"
	WebMetricEvents    = "events"
	WebMetricLCP       = "lcp"
	WebMetricINP       = "inp"
	WebMetricCLS       = "cls"
	WebMetricFCP       = "fcp"

	DefaultWebVitalQuantile = 0.75
	DefaultWebTableDays     = 7
	MaxWebTableDays         = 90
	DefaultWebTableLimit    = 100
	MaxWebTableLimit        = 500
)

var (
	webVitals    = []string{WebMetricLCP, WebMetricINP, WebMetricCLS, WebMetricFCP}
	webMetricsOf = map[DataSourceKind][]string{
		DataSourceKindPostHog: {
			WebMetricPageViews, WebMetricSessions, WebMetricVisitors, WebMetricJSErrors, WebMetricEvents,
			WebMetricLCP, WebMetricINP, WebMetricCLS, WebMetricFCP,
		},
		DataSourceKindGA4:       {WebMetricPageViews, WebMetricSessions, WebMetricVisitors, WebMetricEvents},
		DataSourceKindPlausible: {WebMetricPageViews, WebMetricSessions, WebMetricVisitors},
		// Vercel's Web Analytics counts page views and visitors (it has no
		// session); Speed Insights measures the vitals.
		DataSourceKindVercel: {
			WebMetricPageViews, WebMetricVisitors, WebMetricLCP, WebMetricINP, WebMetricCLS, WebMetricFCP,
		},
	}
	ga4PropertyPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	// vercelProjectPattern is a project id (prj_...) or, in a pinch, a name.
	vercelProjectPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

// IsWebVital reports whether metric is a percentile of a web vital.
func IsWebVital(metric string) bool { return slices.Contains(webVitals, metric) }

// WebQuery is the query language of the web analytics kinds:
//
//	{"metric": "page_views", "host": "www.example.com"}
//	{"table": "pages", "days": 7}
//
// A series names a Metric (default page_views; which ones a kind reads is
// its own: GA4 and Plausible have no vitals or errors). Event names the
// event counted by metric events (GA4's eventName, a PostHog event). Quantile
// is a vital's percentile, 0 < q < 1 (default 0.75, as Core Web Vitals are
// judged). A table instead names Table (pages, referrers, countries,
// devices): its rows over the last Days (default 7), the most viewed first,
// at most Limit. Host keeps one hostname. Property (GA4) and Site
// (Plausible) override the source's own property id or site; Project names
// the Vercel project (its id, prj_...) a team-wide Vercel source reads.
type WebQuery struct {
	Metric   string  `json:"metric,omitempty"`
	Event    string  `json:"event,omitempty"`
	Quantile float64 `json:"quantile,omitempty"`
	Table    string  `json:"table,omitempty"`
	Host     string  `json:"host,omitempty"`
	Property string  `json:"property,omitempty"`
	Site     string  `json:"site,omitempty"`
	Project  string  `json:"project,omitempty"`
	Days     int     `json:"days,omitempty"`
	Limit    int     `json:"limit,omitempty"`
}

func (q WebQuery) IsTable() bool { return q.Table != "" }

// ValidGA4Property reports whether s is a GA4 property id (the number, not
// "properties/<n>"), checked where it is read so a recipe validates with any
// sample value.
func ValidGA4Property(s string) bool { return ga4PropertyPattern.MatchString(s) }

// ParseWebQuery reads a query of one web analytics kind.
func ParseWebQuery(kind DataSourceKind, raw string) (WebQuery, error) {
	known, ok := webMetricsOf[kind]
	if !ok {
		return WebQuery{}, fmt.Errorf("%s is not a web analytics kind", kind)
	}
	var q WebQuery
	if err := decodeBrokerQuery(raw, string(kind), &q); err != nil {
		return WebQuery{}, err
	}
	for _, f := range []struct{ key, v string }{{"event", q.Event}, {"host", q.Host}, {"property", q.Property}, {"site", q.Site}, {"project", q.Project}} {
		if err := checkBrokerName(f.key, f.v, false); err != nil {
			return WebQuery{}, err
		}
	}
	switch {
	case q.Property != "" && kind != DataSourceKindGA4:
		return WebQuery{}, fmt.Errorf("property applies to Google Analytics only")
	case q.Site != "" && kind != DataSourceKindPlausible:
		return WebQuery{}, fmt.Errorf("site applies to Plausible only")
	case q.Project != "" && kind != DataSourceKindVercel:
		return WebQuery{}, fmt.Errorf("project applies to Vercel only")
	case q.Project != "" && !vercelProjectPattern.MatchString(q.Project):
		return WebQuery{}, fmt.Errorf("project %q is not a Vercel project id", q.Project)
	case strings.ContainsFunc(q.Host, unicode.IsSpace):
		return WebQuery{}, fmt.Errorf("host %q is not a hostname", q.Host)
	case strings.ContainsFunc(q.Site, unicode.IsSpace):
		return WebQuery{}, fmt.Errorf("site %q is not a Plausible site", q.Site)
	}
	if q.Table != "" {
		return q, q.checkTable()
	}
	return q, q.checkSeries(known)
}

func (q *WebQuery) checkTable() error {
	switch {
	case !slices.Contains(WebTables(), q.Table):
		return fmt.Errorf("table %q is not one of %s", q.Table, strings.Join(WebTables(), ", "))
	case q.Metric != "" || q.Event != "" || q.Quantile != 0:
		return fmt.Errorf("a table takes no metric, event or quantile")
	case q.Days < 0 || q.Days > MaxWebTableDays:
		return fmt.Errorf("days must be 1 to %d", MaxWebTableDays)
	case q.Limit < 0 || q.Limit > MaxWebTableLimit:
		return fmt.Errorf("limit must be 1 to %d", MaxWebTableLimit)
	}
	if q.Days == 0 {
		q.Days = DefaultWebTableDays
	}
	if q.Limit == 0 {
		q.Limit = DefaultWebTableLimit
	}
	return nil
}

func (q *WebQuery) checkSeries(known []string) error {
	if q.Metric == "" {
		q.Metric = WebMetricPageViews
	}
	switch {
	case !slices.Contains(known, q.Metric):
		return fmt.Errorf("metric %q is not one of %s", q.Metric, strings.Join(known, ", "))
	case q.Days != 0 || q.Limit != 0:
		return fmt.Errorf("days and limit apply to a table only")
	case q.Metric == WebMetricEvents && q.Event == "":
		return fmt.Errorf("metric events needs the event it counts")
	case q.Metric != WebMetricEvents && q.Event != "":
		return fmt.Errorf("event applies to metric events only")
	case q.Quantile != 0 && !IsWebVital(q.Metric):
		return fmt.Errorf("quantile applies to a web vital only (%s)", strings.Join(webVitals, ", "))
	case q.Quantile < 0 || q.Quantile >= 1:
		return fmt.Errorf("quantile must be between 0 and 1")
	}
	if IsWebVital(q.Metric) && q.Quantile == 0 {
		q.Quantile = DefaultWebVitalQuantile
	}
	return nil
}

// ComponentTable is one table of a component's endpoints, web or crashes
// view: what one data source answers for the recipe table Table of a
// confirmed binding's recipe. SignalBindingID is the first such binding (in
// catalog order) of the ones that read this same table; Params are the ones
// the table used. Signal is that binding's signal. KeyColumns are the
// columns that name a row (the recipe table's key labels), the others hold
// values. A table that could not be read carries Error and no rows; the
// others stand.
type ComponentTable struct {
	SignalBindingID uuid.UUID         `json:"signal_binding_id"`
	Signal          string            `json:"signal"`
	DataSourceID    uuid.UUID         `json:"data_source_id"`
	DataSourceName  string            `json:"data_source_name"`
	RecipeID        string            `json:"recipe_id"`
	Table           string            `json:"table"`
	Params          map[string]string `json:"params"`
	Columns         []TableColumn     `json:"columns"`
	KeyColumns      []string          `json:"key_columns,omitempty"`
	Rows            []map[string]any  `json:"rows"`
	Truncated       bool              `json:"truncated"`
	Error           *SignalReadError  `json:"error"`
}

// ComponentTables is GET /v1/components/:id/endpoints, …/web and
// …/crashes. ReadAt is when its tables were read: an answer is kept a
// while, so it may be older than the request.
type ComponentTables struct {
	ComponentID uuid.UUID         `json:"component_id"`
	Environment DeployEnvironment `json:"environment"`
	ReadAt      time.Time         `json:"read_at"`
	Tables      []ComponentTable  `json:"tables"`
}

// WithDefaults fills each param of r that params leaves empty from r's
// Defaults; the answer is a new map.
func (r Recipe) WithDefaults(params map[string]string) map[string]string {
	out := make(map[string]string, len(params)+len(r.Defaults))
	for k, v := range params {
		out[k] = v
	}
	for k, v := range r.Defaults {
		if strings.TrimSpace(out[k]) == "" {
			out[k] = v
		}
	}
	return out
}
