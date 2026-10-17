// Package sentry is the data source kind for Sentry (sentry.io or
// self-hosted), read through its Web API with an auth token: release health
// (crash-free users and sessions) and error event counts as series, and
// issues as error groups for the errors inbox and as a crashes table. Its
// query language is domain.SentryQuery.
package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyURL   = "url"
	keyOrg   = "org"
	keyToken = "token"

	DefaultURL = "https://sentry.io"
	api        = "Sentry"

	defaultTimeout = 30 * time.Second
	maxBody        = 32 << 20
	pageSize       = 100
	maxProjects    = 2000
	maxPages       = 20
	maxIssueRows   = 500
	// maxBuckets stays under the 10 000 data points one Sentry stats answer
	// carries.
	maxBuckets = 10000
)

var orgPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// sessionFields maps a release health metric to the field Sentry computes
// it as; scaled are the ones Sentry answers as a fraction.
var (
	sessionFields = map[string]string{
		domain.SentryMetricCrashFreeUsers:    "crash_free_rate(user)",
		domain.SentryMetricCrashFreeSessions: "crash_free_rate(session)",
		domain.SentryMetricCrashRate:         "crash_rate(session)",
		domain.SentryMetricCrashRateUsers:    "crash_rate(user)",
		domain.SentryMetricSessions:          "sum(session)",
		domain.SentryMetricUsers:             "count_unique(user)",
	}
	scaled = map[string]bool{
		domain.SentryMetricCrashFreeUsers: true, domain.SentryMetricCrashFreeSessions: true,
		domain.SentryMetricCrashRate: true, domain.SentryMetricCrashRateUsers: true,
	}
	// sessionIntervals are what release health takes: at least an hour, at
	// most a day, dividing a day evenly.
	sessionIntervals = []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour, 4 * time.Hour, 6 * time.Hour, 8 * time.Hour, 12 * time.Hour, 24 * time.Hour}
)

type Adapter struct {
	policy  urlguard.Policy
	timeout time.Duration
	now     func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.ErrorsQuerier         = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy; datasource.NewService
// sets it.
func New() *Adapter {
	return &Adapter{timeout: defaultTimeout, now: time.Now}
}

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindSentry }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityErrors, domain.CapabilityCrashes}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Sentry",
		Description: "Crash-free users and sessions, error counts, and issues from Sentry — for mobile, web and backend projects alike. Issues join the errors inbox.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "Sentry URL", Type: domain.FieldURL, Required: true, Default: DefaultURL,
				Placeholder: DefaultURL,
				Help:        "https://sentry.io for Sentry's cloud (https://de.sentry.io for an organization in the EU region), or your self-hosted Sentry's address.",
			},
			{
				Key: keyOrg, Label: "Organization", Type: domain.FieldText, Required: true,
				Placeholder: "acme",
				Help:        "The organization slug, as in sentry.io/organizations/<slug>/.",
			},
			{
				Key: keyToken, Label: "Auth token", Type: domain.FieldSecret, Required: true, Secret: true,
				Help: "A token with the org:read, project:read and event:read scopes (an organization token or an internal integration's).",
			},
		},
	}
}

type conn struct {
	*httpapi.Conn
	org  string
	base *url.URL
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string) (*conn, error) {
	org := strings.TrimSpace(ds.Config[keyOrg])
	if !orgPattern.MatchString(org) {
		return nil, fmt.Errorf("%q is not a Sentry organization slug", org)
	}
	tok := secrets[keyToken]
	if tok == "" {
		return nil, errors.New("no auth token is set")
	}
	base := strings.TrimSpace(ds.Config[keyURL])
	if base == "" {
		base = DefaultURL
	}
	c, err := httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    base,
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + tok}},
		Masks:   []string{tok},
		API:     api,
	}, a.timeout)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(base)
	return &conn{Conn: c, org: org, base: u}, nil
}

// link keeps an issue's permalink only when it points at this Sentry: its
// configured host over https (or the scheme it is configured with), or, for
// Sentry's cloud, an https page of sentry.io, where each organization has a
// subdomain. It is shown as a link, and the answer is not this server's to
// trust.
func (c *conn) link(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || c.base == nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	sameHost := strings.EqualFold(u.Host, c.base.Host) && (u.Scheme == "https" || u.Scheme == c.base.Scheme)
	cloud := u.Scheme == "https" && u.Port() == "" && sentryCloud(c.base.Hostname()) && sentryCloud(u.Hostname())
	if !sameHost && !cloud {
		return ""
	}
	return u.String()
}

func sentryCloud(host string) bool {
	host = strings.ToLower(host)
	return host == "sentry.io" || strings.HasSuffix(host, ".sentry.io")
}

func (c *conn) orgPath(rest string) string {
	return "/api/0/organizations/" + url.PathEscape(c.org) + "/" + rest
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	var org struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if _, err := a.get(ctx, c, "read the organization", c.orgPath(""), nil, &org); err != nil {
		return domain.DataSourceInfo{}, err
	}
	name := org.Name
	if name == "" {
		name = org.Slug
	}
	return domain.DataSourceInfo{Detail: "Sentry · " + name}, nil
}

// Discover lists the organization's projects as project/<slug>.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	inv := domain.Inventory{Metrics: []string{}}
	params := url.Values{"per_page": {strconv.Itoa(pageSize)}}
	for page := 0; ; page++ {
		var projects []struct {
			Slug string `json:"slug"`
		}
		h, err := a.get(ctx, c, "list projects", c.orgPath("projects/"), params, &projects)
		if err != nil {
			return domain.Inventory{}, err
		}
		for _, p := range projects {
			if len(inv.Metrics) == maxProjects {
				inv.Truncated = true
				return inv, nil
			}
			inv.Metrics = append(inv.Metrics, "project/"+p.Slug)
		}
		cursor, more := nextCursor(h)
		if !more {
			return inv, nil
		}
		if page+1 >= maxPages {
			inv.Truncated = true
			return inv, nil
		}
		params.Set("cursor", cursor)
	}
}

func parse(raw string) (domain.SentryQuery, error) {
	q, err := domain.ParseSentryQuery(raw)
	if err != nil {
		return q, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	return q, nil
}

// projectID: the stats endpoints take a project's numeric id, so a slug is
// looked up first.
func (a *Adapter) projectID(ctx context.Context, c *conn, project string) (string, error) {
	if _, err := strconv.ParseUint(project, 10, 64); err == nil {
		return project, nil
	}
	var p struct {
		ID string `json:"id"`
	}
	path := "/api/0/projects/" + url.PathEscape(c.org) + "/" + url.PathEscape(project) + "/"
	if _, err := a.get(ctx, c, "read project "+project, path, nil, &p); err != nil {
		return "", err
	}
	if p.ID == "" {
		return "", fmt.Errorf("%s: project %q has no id", api, project)
	}
	return p.ID, nil
}

// releaseFilter adds a release to Sentry search syntax, quoted so the value
// cannot widen the search.
func releaseFilter(search, release string) string {
	if release == "" {
		return search
	}
	clause := "release:" + strconv.Quote(release)
	if search == "" {
		return clause
	}
	return search + " " + clause
}

// Series reads a release health metric from the sessions endpoint, or error
// event counts from events-stats. Release health answers at whole hours (at
// most a day), so Step is that interval; the crash rates are percent.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := parse(sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	id, err := a.projectID(ctx, c, q.Project)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if domain.SentrySessionMetric(q.Metric) {
		return a.sessions(ctx, c, id, q, sq)
	}
	return a.events(ctx, c, id, q, sq)
}

func sessionInterval(step time.Duration) time.Duration {
	for _, iv := range sessionIntervals {
		if iv >= step {
			return iv
		}
	}
	return sessionIntervals[len(sessionIntervals)-1]
}

func (a *Adapter) sessions(ctx context.Context, c *conn, projectID string, q domain.SentryQuery, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	field := sessionFields[q.Metric]
	iv := sessionInterval(sq.Step)
	for sq.To.Sub(sq.From)/iv > maxBuckets && iv < sessionIntervals[len(sessionIntervals)-1] {
		iv = sessionInterval(iv + time.Second)
	}
	params := url.Values{
		"field":         {field},
		"project":       {projectID},
		"interval":      {strconv.Itoa(int(iv/time.Hour)) + "h"},
		"start":         {sq.From.UTC().Format(time.RFC3339)},
		"end":           {sq.To.UTC().Format(time.RFC3339)},
		"includeTotals": {"0"},
	}
	if q.Environment != "" {
		params.Set("environment", q.Environment)
	}
	if search := releaseFilter("", q.Release); search != "" {
		params.Set("query", search)
	}
	var out struct {
		Intervals []string `json:"intervals"`
		Groups    []struct {
			Series map[string][]*float64 `json:"series"`
		} `json:"groups"`
	}
	if _, err := a.get(ctx, c, "read release health", c.orgPath("sessions/"), params, &out); err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{}, Step: iv}
	if len(out.Groups) == 0 {
		return res, nil
	}
	values := out.Groups[0].Series[field]
	s := domain.Series{Name: q.Metric, Labels: map[string]string{"project": q.Project}, Points: []domain.SeriesPoint{}}
	for i, raw := range out.Intervals {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		var v *float64
		if i < len(values) {
			v = values[i]
		}
		if v == nil {
			s.Points = append(s.Points, domain.SeriesPoint{Time: t.UTC()})
			continue
		}
		val := *v
		if scaled[q.Metric] {
			val *= 100
		}
		s.Points = append(s.Points, domain.FinitePoint(t.UTC(), val))
	}
	res.Series = append(res.Series, s)
	return res, nil
}

func (a *Adapter) events(ctx context.Context, c *conn, projectID string, q domain.SentryQuery, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	iv := max(sq.Step, time.Minute)
	if span := sq.To.Sub(sq.From); span/iv > maxBuckets {
		iv = span / maxBuckets
	}
	minutes := int(math.Ceil(iv.Minutes()))
	iv = time.Duration(minutes) * time.Minute
	params := url.Values{
		"project":  {projectID},
		"yAxis":    {"count()"},
		"interval": {strconv.Itoa(minutes) + "m"},
		"start":    {sq.From.UTC().Format(time.RFC3339)},
		"end":      {sq.To.UTC().Format(time.RFC3339)},
		"dataset":  {"errors"},
	}
	if q.Environment != "" {
		params.Set("environment", q.Environment)
	}
	if search := releaseFilter(q.Search, q.Release); search != "" {
		params.Set("query", search)
	}
	var out struct {
		Data [][2]json.RawMessage `json:"data"`
	}
	if _, err := a.get(ctx, c, "read event counts", c.orgPath("events-stats/"), params, &out); err != nil {
		return domain.SeriesResult{}, err
	}
	s := domain.Series{Name: q.Metric, Labels: map[string]string{"project": q.Project}, Points: []domain.SeriesPoint{}}
	for _, bucket := range out.Data {
		var ts float64
		var counts []struct {
			Count *float64 `json:"count"`
		}
		if json.Unmarshal(bucket[0], &ts) != nil || json.Unmarshal(bucket[1], &counts) != nil {
			continue
		}
		t := time.Unix(int64(ts), 0).UTC()
		if len(counts) == 0 || counts[0].Count == nil {
			s.Points = append(s.Points, domain.SeriesPoint{Time: t})
			continue
		}
		v := *counts[0].Count
		if q.Metric == domain.SentryMetricEventRate {
			v /= iv.Seconds()
		}
		s.Points = append(s.Points, domain.FinitePoint(t, v))
	}
	return domain.SeriesResult{Series: []domain.Series{s}, Warnings: []string{}, Step: iv}, nil
}

// issue: Sentry answers count and userCount over the window asked for (start,
// end, environment), filtered the same over the search's conditions too when
// it has any, and lifetime over all time.
type issue struct {
	ID          string       `json:"id"`
	ShortID     string       `json:"shortId"`
	Title       string       `json:"title"`
	Culprit     string       `json:"culprit"`
	Permalink   string       `json:"permalink"`
	Count       string       `json:"count"`
	UserCount   int64        `json:"userCount"`
	Filtered    *issueCounts `json:"filtered"`
	FirstSeen   string       `json:"firstSeen"`
	LastSeen    string       `json:"lastSeen"`
	Level       string       `json:"level"`
	IsUnhandled bool         `json:"isUnhandled"`
	Metadata    struct {
		Value string `json:"value"`
	} `json:"metadata"`
}

type issueCounts struct {
	Count     string `json:"count"`
	UserCount int64  `json:"userCount"`
}

// events and users are the issue's in the window, of the events the search
// matches when Sentry counted those.
func (is issue) events() float64 {
	if is.Filtered != nil && is.Filtered.Count != "" {
		return countOf(is.Filtered.Count)
	}
	return countOf(is.Count)
}

func (is issue) users() int64 {
	if is.Filtered != nil && is.Filtered.Count != "" {
		return is.Filtered.UserCount
	}
	return is.UserCount
}

const (
	sortFrequent = "freq"
	sortNewest   = "new"
)

// issues lists the project's issues with events in [since, until] in order
// (the most frequent first, or the newest), at most limit.
func (a *Adapter) issues(ctx context.Context, c *conn, q domain.SentryQuery, since, until time.Time, limit int, order string) ([]issue, bool, error) {
	if limit <= 0 {
		limit = pageSize
	}
	id, err := a.projectID(ctx, c, q.Project)
	if err != nil {
		return nil, false, err
	}
	search := q.Search
	if search == "" {
		search = domain.DefaultSentryIssueSearch
	}
	params := url.Values{
		"project": {id},
		"query":   {releaseFilter(search, q.Release)},
		"start":   {since.UTC().Format(time.RFC3339)},
		"end":     {until.UTC().Format(time.RFC3339)},
		"sort":    {order},
		"limit":   {strconv.Itoa(min(pageSize, limit))},
	}
	if q.Environment != "" {
		params.Set("environment", q.Environment)
	}
	var out []issue
	for page := 0; page < maxPages; page++ {
		var batch []issue
		h, err := a.get(ctx, c, "list issues", c.orgPath("issues/"), params, &batch)
		if err != nil {
			return nil, false, err
		}
		for _, is := range batch {
			if len(out) == limit {
				return out, true, nil
			}
			out = append(out, is)
		}
		cursor, more := nextCursor(h)
		if !more {
			return out, false, nil
		}
		if len(out) == limit {
			return out, true, nil
		}
		params.Set("cursor", cursor)
	}
	return out, true, nil
}

// Errors answers the project's issues seen in the window as error groups:
// the fingerprint is the issue id, the message its title, the source its
// culprit, the count its events in the window. They are the most frequent;
// when there are more than the limit, a fifth of it goes to the newest ones
// those left out, so a new rare issue still reaches the inbox.
func (a *Adapter) Errors(ctx context.Context, ds domain.DataSource, secrets map[string]string, eq domain.ErrorsQuery) ([]domain.RuntimeErrorGroup, error) {
	q, err := parse(eq.Query)
	if err != nil {
		return nil, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	limit := eq.Limit
	if limit <= 0 {
		limit = pageSize
	}
	found, more, err := a.issues(ctx, c, q, eq.Since, eq.Until, limit, sortFrequent)
	if err != nil {
		return nil, err
	}
	if more {
		reserve := max(1, limit/5)
		newest, _, err := a.issues(ctx, c, q, eq.Since, eq.Until, reserve, sortNewest)
		if err != nil {
			return nil, err
		}
		found = withNewest(found, newest, limit, eq.Since)
	}
	out := make([]domain.RuntimeErrorGroup, 0, len(found))
	for _, is := range found {
		g := domain.RuntimeErrorGroup{
			Fingerprint: is.ID, Message: is.Title, Count: int(is.events()), Source: is.Culprit,
			Sample: is.Metadata.Value, ExternalURL: c.link(is.Permalink),
		}
		g.FirstSeen, _ = time.Parse(time.RFC3339, is.FirstSeen)
		g.LastSeen, _ = time.Parse(time.RFC3339, is.LastSeen)
		g.New = !g.FirstSeen.IsZero() && !g.FirstSeen.Before(eq.Since)
		out = append(out, g)
	}
	return out, nil
}

// withNewest ends the most frequent issues with the ones first seen since
// that they do not hold, keeping limit.
func withNewest(frequent, newest []issue, limit int, since time.Time) []issue {
	held := map[string]bool{}
	for _, is := range frequent {
		held[is.ID] = true
	}
	var extra []issue
	for _, is := range newest {
		first, err := time.Parse(time.RFC3339, is.FirstSeen)
		if !held[is.ID] && err == nil && !first.Before(since) {
			held[is.ID] = true
			extra = append(extra, is)
		}
	}
	keep := max(0, min(len(frequent), limit-len(extra)))
	return append(frequent[:keep:keep], extra...)
}

// Table answers the crashes table: the project's issues of the last Days
// before At, the most frequent first, with the crashes table's columns (an
// unhandled issue is a crash; the others keep their level).
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	until := tq.At
	found, truncated, err := a.issues(ctx, c, q, until.Add(-time.Duration(q.Days)*24*time.Hour), until, maxIssueRows, sortFrequent)
	if err != nil {
		return domain.TableResult{}, err
	}
	res := domain.TableResult{Columns: crashColumns(), Rows: make([]map[string]any, 0, len(found)), Truncated: truncated}
	for _, is := range found {
		kind := is.Level
		if is.IsUnhandled {
			kind = "crash"
		}
		res.Rows = append(res.Rows, map[string]any{
			"issue": is.ShortID, "type": kind, "cause": is.Title, "location": is.Culprit,
			"users": float64(is.users()), "reports": is.events(),
			"last_seen": is.LastSeen, "link": c.link(is.Permalink),
		})
	}
	return res, nil
}

func crashColumns() []domain.TableColumn {
	return []domain.TableColumn{
		{Key: "issue", Label: "Issue"}, {Key: "type", Label: "Type"}, {Key: "cause", Label: "Cause"},
		{Key: "location", Label: "Location"}, {Key: "users", Label: "Users", Unit: "users"},
		{Key: "reports", Label: "Reports", Unit: "reports"}, {Key: "last_version", Label: "Last version"},
		{Key: "last_seen", Label: "Last seen"}, {Key: "link", Label: "Link"},
	}
}

func countOf(s string) float64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return n
}

// nextCursor reads Sentry's Link header: the next page's cursor, and whether
// it holds results. Only the cursor is taken, never the URL, so a header
// cannot send the token elsewhere.
func nextCursor(h http.Header) (string, bool) {
	for _, link := range strings.Split(h.Get("Link"), ",") {
		attrs := map[string]string{}
		for _, part := range strings.Split(link, ";")[1:] {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok {
				attrs[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
			}
		}
		if attrs["rel"] == "next" {
			return attrs["cursor"], attrs["results"] == "true" && attrs["cursor"] != ""
		}
	}
	return "", false
}

func (a *Adapter) get(ctx context.Context, c *conn, op, path string, query url.Values, out any) (http.Header, error) {
	resp, err := c.Do(ctx, httpapi.Request{Method: http.MethodGet, Path: path, Query: query, Limit: maxBody})
	if err != nil {
		return nil, err
	}
	if resp.OK() {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return nil, fmt.Errorf("%s %s: the answer is not what Sentry sends — is the URL right?", api, op)
		}
		return resp.Header, nil
	}
	d := c.Scrub(detail(resp.Body))
	suffix := ""
	if d != "" {
		suffix = ": " + d
	}
	switch resp.Status {
	case http.StatusBadRequest:
		return nil, fmt.Errorf("%w: %s %s: HTTP 400%s", port.ErrDataSourceQuery, api, op, suffix)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%s %s: authentication failed (HTTP %d)%s; the token needs org:read, project:read and event:read", api, op, resp.Status, suffix)
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s %s: not found (HTTP 404)%s — check the organization slug and project", api, op, suffix)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("%s %s: rate limited (HTTP 429); try again in a minute", api, op)
	}
	return nil, fmt.Errorf("%s %s: HTTP %d%s", api, op, resp.Status, suffix)
}

// detail is Sentry's {"detail": "…"} message.
func detail(raw []byte) string {
	var body struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	switch d := body.Detail.(type) {
	case string:
		return d
	case map[string]any:
		if m, ok := d["message"].(string); ok {
			return m
		}
	}
	return ""
}
