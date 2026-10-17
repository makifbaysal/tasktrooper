package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultVerifyTimeout = 15 * time.Second
	defaultQueryTimeout  = 30 * time.Second

	maxSmallBody     = 1 << 20
	maxInventoryBody = 16 << 20
	maxSeriesBody    = 32 << 20
	// DefaultMaxInventory bounds the metric names Discover returns; a source
	// with more answers Truncated.
	DefaultMaxInventory = 20000
	// discoverWindow keeps the name listing to what was scraped recently,
	// which is also far cheaper on Mimir and Thanos than all of history.
	discoverWindow = time.Hour

	maxUpstreamText = 300
	maskMinLen      = 4
)

// Endpoint is one Prometheus HTTP API: Base is the URL /api/v1 hangs off,
// Headers go on every request in order (a later one replaces an earlier one
// of the same name, so credentials come first and a person's own headers
// after), and Masks are the values no returned error may carry.
type Endpoint struct {
	Base    string
	Headers []domain.HeaderLine
	Masks   []string
}

// API calls the Prometheus HTTP API at an Endpoint. It is what every
// Prometheus-family kind shares: a kind adds only where the API lives and how
// it authenticates.
type API struct {
	policy        urlguard.Policy
	verifyTimeout time.Duration
	queryTimeout  time.Duration
	maxInventory  int
	now           func() time.Time
}

// NewAPI answers an API that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy.
func NewAPI() *API {
	return &API{
		verifyTimeout: defaultVerifyTimeout,
		queryTimeout:  defaultQueryTimeout,
		maxInventory:  DefaultMaxInventory,
		now:           time.Now,
	}
}

// SetURLPolicy replaces where an endpoint may connect; every request is
// checked against it again at dial time.
func (a *API) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *API) SetMaxInventory(n int) { a.maxInventory = n }

type apiResponse struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
	Infos     []string        `json:"infos"`
}

// reply is one answer: the HTTP status, the Prometheus envelope when the body
// is one (nil otherwise), and otherwise the message of a JSON error object
// ({"error": {"message"}}, the Google API shape), when the body is that.
type reply struct {
	code   int
	env    *apiResponse
	detail string
}

type buildInfo struct {
	Version     string `json:"version"`
	Application string `json:"application"`
}

// Verify reads build info, falling back to a trivial instant query on any
// answer but success or an auth refusal: servers without the endpoint (older
// Mimir and Thanos, some proxies) answer 404, 400 or 501 for it.
func (a *API) Verify(ctx context.Context, ep Endpoint) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ep, a.verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.client.CloseIdleConnections()
	r, err := c.call(ctx, http.MethodGet, "/api/v1/status/buildinfo", nil, maxSmallBody)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	switch {
	case r.code == http.StatusOK:
		if r.env == nil || r.env.Status != "success" {
			return domain.DataSourceInfo{}, errNotPrometheus
		}
		var bi buildInfo
		_ = json.Unmarshal(r.env.Data, &bi)
		detail := bi.Application
		if detail == "" {
			detail = "Prometheus API"
		}
		return domain.DataSourceInfo{Version: bi.Version, Detail: detail}, nil
	case r.code == http.StatusUnauthorized || r.code == http.StatusForbidden:
		return domain.DataSourceInfo{}, c.statusError("build info", r)
	}
	if err := c.probe(ctx); err != nil {
		return domain.DataSourceInfo{}, err
	}
	return domain.DataSourceInfo{Detail: "Prometheus API (no build info)"}, nil
}

// Probe runs only the trivial instant query, for an API known to have no
// build info endpoint.
func (a *API) Probe(ctx context.Context, ep Endpoint) error {
	c, err := a.open(ctx, ep, a.verifyTimeout)
	if err != nil {
		return err
	}
	defer c.client.CloseIdleConnections()
	return c.probe(ctx)
}

// probe POSTs the form: Prometheus takes GET or POST there, and Google's
// Cloud Monitoring API documents prometheus.api.v1.query as POST only.
func (c *conn) probe(ctx context.Context) error {
	r, err := c.call(ctx, http.MethodPost, "/api/v1/query", url.Values{"query": {"1"}}, maxSmallBody)
	if err != nil {
		return err
	}
	if r.code != http.StatusOK {
		return c.statusError("query", r)
	}
	if r.env == nil || r.env.Status != "success" {
		return errNotPrometheus
	}
	return nil
}

var errNotPrometheus = errors.New("the URL answered, but not with the Prometheus HTTP API — is it the API base URL?")

type matrixSeries struct {
	Metric     map[string]string `json:"metric"`
	Values     []samplePair      `json:"values"`
	Histograms json.RawMessage   `json:"histograms"`
}

type samplePair struct {
	T float64
	V string
}

func (p *samplePair) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &p.T); err != nil {
		return err
	}
	return json.Unmarshal(raw[1], &p.V)
}

func (a *API) QueryRange(ctx context.Context, ep Endpoint, q domain.SeriesQuery) (domain.SeriesResult, error) {
	c, err := a.open(ctx, ep, a.queryTimeout)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.client.CloseIdleConnections()
	form := url.Values{
		"query": {q.Query},
		"start": {formatTime(q.From)},
		"end":   {formatTime(q.To)},
		"step":  {strconv.FormatFloat(q.Step.Seconds(), 'f', -1, 64)},
	}
	r, err := c.call(ctx, http.MethodPost, "/api/v1/query_range", form, maxSeriesBody)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if r.code != http.StatusOK || r.env == nil || r.env.Status != "success" {
		return domain.SeriesResult{}, c.statusError("query", r)
	}
	var data struct {
		ResultType string         `json:"resultType"`
		Result     []matrixSeries `json:"result"`
	}
	if err := json.Unmarshal(r.env.Data, &data); err != nil {
		return domain.SeriesResult{}, errNotPrometheus
	}
	if data.ResultType != "matrix" {
		return domain.SeriesResult{}, fmt.Errorf("query: expected a matrix result, got %q", data.ResultType)
	}
	out := domain.SeriesResult{Series: make([]domain.Series, 0, len(data.Result))}
	out.Warnings = append(out.Warnings, r.env.Warnings...)
	out.Warnings = append(out.Warnings, r.env.Infos...)
	skippedHistograms := false
	for _, ms := range data.Result {
		if len(ms.Histograms) > 0 && string(ms.Histograms) != "null" {
			skippedHistograms = true
		}
		s := domain.Series{Labels: map[string]string{}, Points: make([]domain.SeriesPoint, 0, len(ms.Values))}
		for k, v := range ms.Metric {
			if k == "__name__" {
				s.Name = v
				continue
			}
			s.Labels[k] = v
		}
		if s.Name == "" {
			s.Name = seriesName(s.Labels, q.Query)
		}
		for _, p := range ms.Values {
			v, err := strconv.ParseFloat(p.V, 64)
			if err != nil {
				v = math.NaN()
			}
			s.Points = append(s.Points, domain.FinitePoint(time.UnixMilli(int64(math.Round(p.T*1000))).UTC(), v))
		}
		out.Series = append(out.Series, s)
	}
	if skippedHistograms {
		out.Warnings = append(out.Warnings, "native histogram samples are not shown; query a quantile or a bucket rate instead")
	}
	return out, nil
}

type vectorSample struct {
	Metric map[string]string `json:"metric"`
	Value  *samplePair       `json:"value"`
}

// Instant runs an instant query (POST /api/v1/query) at q.At and answers
// each sample of the vector as a row: its labels but __name__, and its value
// under domain.TableValueKey (nil for NaN, ±Inf or a native histogram). A
// scalar is one row of just the value. Rows are ordered by their labels.
func (a *API) Instant(ctx context.Context, ep Endpoint, q domain.TableQuery) (domain.TableResult, error) {
	c, err := a.open(ctx, ep, a.queryTimeout)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.client.CloseIdleConnections()
	form := url.Values{"query": {q.Query}}
	if !q.At.IsZero() {
		form.Set("time", formatTime(q.At))
	}
	r, err := c.call(ctx, http.MethodPost, "/api/v1/query", form, maxSeriesBody)
	if err != nil {
		return domain.TableResult{}, err
	}
	if r.code != http.StatusOK || r.env == nil || r.env.Status != "success" {
		return domain.TableResult{}, c.statusError("query", r)
	}
	var data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(r.env.Data, &data); err != nil {
		return domain.TableResult{}, errNotPrometheus
	}
	var samples []vectorSample
	switch data.ResultType {
	case "vector":
		if err := json.Unmarshal(data.Result, &samples); err != nil {
			return domain.TableResult{}, errNotPrometheus
		}
	case "scalar":
		var p samplePair
		if err := json.Unmarshal(data.Result, &p); err != nil {
			return domain.TableResult{}, errNotPrometheus
		}
		samples = []vectorSample{{Value: &p}}
	default:
		return domain.TableResult{}, fmt.Errorf("%w: a table query must answer an instant vector, not a %s", port.ErrDataSourceQuery, data.ResultType)
	}
	return vectorTable(samples), nil
}

func vectorTable(samples []vectorSample) domain.TableResult {
	labels := map[string]bool{}
	type keyed struct {
		key string
		row map[string]any
	}
	rows := make([]keyed, 0, len(samples))
	for _, s := range samples {
		row := map[string]any{domain.TableValueKey: nil}
		plain := map[string]string{}
		for k, v := range s.Metric {
			if k == "__name__" || k == domain.TableValueKey {
				continue
			}
			labels[k] = true
			row[k] = v
			plain[k] = v
		}
		if s.Value != nil {
			if v, err := strconv.ParseFloat(s.Value.V, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
				row[domain.TableValueKey] = v
			}
		}
		rows = append(rows, keyed{key: seriesName(plain, ""), row: row})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := domain.TableResult{Columns: make([]domain.TableColumn, 0, len(keys)+1), Rows: make([]map[string]any, 0, len(rows))}
	for _, k := range keys {
		out.Columns = append(out.Columns, domain.TableColumn{Key: k, Label: k})
	}
	out.Columns = append(out.Columns, domain.TableColumn{Key: domain.TableValueKey, Label: domain.TableValueKey})
	for _, r := range rows {
		out.Rows = append(out.Rows, r.row)
	}
	return out
}

func seriesName(labels map[string]string, query string) string {
	if len(labels) == 0 {
		return query
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(labels[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// MetricNames lists the metric names seen over the last hour, sorted and
// capped at the API's inventory limit.
func (a *API) MetricNames(ctx context.Context, ep Endpoint) (domain.Inventory, error) {
	c, err := a.open(ctx, ep, a.queryTimeout)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.client.CloseIdleConnections()
	now := a.now()
	params := url.Values{
		"start": {formatTime(now.Add(-discoverWindow))},
		"end":   {formatTime(now)},
	}
	r, err := c.call(ctx, http.MethodGet, "/api/v1/label/__name__/values", params, maxInventoryBody)
	if err != nil {
		return domain.Inventory{}, err
	}
	if r.code != http.StatusOK || r.env == nil || r.env.Status != "success" {
		return domain.Inventory{}, c.statusError("metric names", r)
	}
	var names []string
	if err := json.Unmarshal(r.env.Data, &names); err != nil {
		return domain.Inventory{}, errNotPrometheus
	}
	sort.Strings(names)
	inv := domain.Inventory{Metrics: names}
	if len(names) > a.maxInventory {
		inv.Metrics = names[:a.maxInventory]
		inv.Truncated = true
	}
	if inv.Metrics == nil {
		inv.Metrics = []string{}
	}
	return inv, nil
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', 3, 64)
}

type conn struct {
	base    *url.URL
	ips     []net.IP
	client  *http.Client
	headers []domain.HeaderLine
	masks   []string
}

func (a *API) open(ctx context.Context, ep Endpoint, timeout time.Duration) (*conn, error) {
	raw := strings.TrimSpace(ep.Base)
	if raw == "" {
		return nil, errors.New("no URL is configured")
	}
	target, err := a.policy.Validate(ctx, raw)
	if err != nil {
		return nil, refused(err, raw)
	}
	if target.URL.User != nil {
		return nil, errors.New("the URL must not carry credentials; use the username and password fields")
	}
	client := a.policy.ClientFor(target, timeout)
	// A redirect to another host, or from https to http, would carry the
	// tenant and auth headers along with it.
	client.CheckRedirect = a.policy.SameHostRedirect(target.URL)
	masks := append([]string(nil), ep.Masks...)
	for _, h := range ep.Headers {
		masks = append(masks, h.Value)
	}
	return &conn{base: target.URL, ips: target.IPs, client: client, headers: ep.Headers, masks: masks}, nil
}

// refused logs why and answers only that: the reason names the address a
// host resolved to, which is not the caller's to learn.
func refused(err error, raw string) error {
	log.Warn().Err(err).Str("url", urlguard.LogRaw(raw)).Msg("data source: destination refused")
	return refusedError{}
}

type refusedError struct{}

func (refusedError) Error() string        { return "the URL is not an allowed destination" }
func (refusedError) Is(target error) bool { return target == urlguard.ErrBlocked }

// call sends values as the query string of a GET or the form of a POST.
func (c *conn) call(ctx context.Context, method, path string, values url.Values, limit int64) (reply, error) {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawPath = ""
	u.RawQuery = ""
	var body io.Reader
	if method == http.MethodGet {
		u.RawQuery = values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return reply{}, errors.New("the URL cannot be requested")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TaskTrooper")
	for _, h := range c.headers {
		req.Header.Set(h.Name, h.Value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return reply{}, c.transportError(path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return reply{}, c.transportError(path, err)
	}
	if int64(len(raw)) > limit {
		return reply{}, fmt.Errorf("%s: the response is larger than %d MiB; narrow the query or raise the step", path, limit>>20)
	}
	r := reply{code: resp.StatusCode}
	var env apiResponse
	if json.Unmarshal(raw, &env) == nil && env.Status != "" {
		r.env = &env
		return r, nil
	}
	var gerr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &gerr) == nil {
		r.detail = gerr.Error.Message
	}
	return r, nil
}

// transportError drops the request URL net/http puts in its message and
// names the host as the person typed it: the guard dials the address it
// resolved, so a raw dial error would tell them which internal IP a name
// points at. The cause (refused, timed out, TLS) is what they can act on.
func (c *conn) transportError(path string, err error) error {
	if errors.Is(err, urlguard.ErrBlocked) {
		return refused(err, c.base.String())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out", path)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return fmt.Errorf("%s: timed out", path)
		}
		err = ue.Err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && opErr.Err != nil {
		return fmt.Errorf("%s: cannot connect to %s: %s", path, c.base.Host, c.scrub(opErr.Err.Error()))
	}
	return fmt.Errorf("%s: %s", path, c.scrub(err.Error()))
}

func (c *conn) statusError(op string, r reply) error {
	resp := r.env
	switch {
	case r.code == http.StatusUnauthorized || r.code == http.StatusForbidden:
		if r.detail != "" {
			return fmt.Errorf("%s: authentication failed (HTTP %d): %s", op, r.code, c.scrub(r.detail))
		}
		return fmt.Errorf("%s: authentication failed (HTTP %d)", op, r.code)
	case resp == nil:
		if r.code == http.StatusOK {
			return errNotPrometheus
		}
		if r.detail != "" {
			return fmt.Errorf("%s: HTTP %d: %s", op, r.code, c.scrub(r.detail))
		}
		return fmt.Errorf("%s: HTTP %d", op, r.code)
	case resp.ErrorType == "bad_data":
		return fmt.Errorf("%w: %s", port.ErrDataSourceQuery, c.scrub(resp.Error))
	case resp.Error != "":
		return fmt.Errorf("%s: HTTP %d: %s: %s", op, r.code, resp.ErrorType, c.scrub(resp.Error))
	default:
		return fmt.Errorf("%s: HTTP %d", op, r.code)
	}
}

// scrub bounds upstream text, masks any secret a misbehaving server echoed
// back (an error quoting the Authorization header it rejected), and puts the
// typed host back where a resolved address of it appears.
func (c *conn) scrub(s string) string {
	for _, v := range c.masks {
		if len(v) >= maskMinLen {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	if host := c.base.Hostname(); host != "" {
		for _, ip := range c.ips {
			s = strings.ReplaceAll(s, ip.String(), host)
		}
	}
	if r := []rune(s); len(r) > maxUpstreamText {
		s = string(r[:maxUpstreamText]) + "…"
	}
	return s
}
