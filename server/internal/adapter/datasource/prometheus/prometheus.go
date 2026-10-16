// Package prometheus is the data source kind for anything that speaks the
// Prometheus HTTP API: Prometheus itself, Mimir, Cortex, Thanos,
// VictoriaMetrics, Grafana Cloud, and Amazon Managed Prometheus behind a
// SigV4 proxy.
package prometheus

import (
	"context"
	"encoding/base64"
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
	keyURL      = "url"
	keyAuth     = "auth"
	keyToken    = "token"
	keyUsername = "username"
	keyPassword = "password"
	keyHeaders  = "headers"

	authNone   = "none"
	authBearer = "bearer"
	authBasic  = "basic"

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
)

type Adapter struct {
	policy        urlguard.Policy
	verifyTimeout time.Duration
	queryTimeout  time.Duration
	maxInventory  int
	now           func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy; datasource.NewService
// sets it from its required Deps.URLPolicy.
func New() *Adapter {
	return &Adapter{
		verifyTimeout: defaultVerifyTimeout,
		queryTimeout:  defaultQueryTimeout,
		maxInventory:  DefaultMaxInventory,
		now:           time.Now,
	}
}

// SetURLPolicy replaces where a source URL may connect; every request is
// checked against it again at dial time.
func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetMaxInventory(n int) { a.maxInventory = n }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindPrometheus }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Prometheus compatible",
		Description: "Prometheus, Grafana Mimir, Cortex, Thanos, VictoriaMetrics, Grafana Cloud, or anything else serving the Prometheus HTTP API.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "URL", Type: domain.FieldURL, Required: true,
				Placeholder: "http://localhost:9090",
				Help:        "The API base URL, without /api/v1. Mimir serves it under /prometheus, Grafana Cloud under /api/prom.",
			},
			{
				Key: keyAuth, Label: "Authentication", Type: domain.FieldSelect,
				Options:     []string{authNone, authBearer, authBasic},
				Placeholder: authNone,
			},
			{
				Key: keyToken, Label: "Bearer token", Type: domain.FieldSecret, Required: true, Secret: true,
				ShowWhen: map[string]string{keyAuth: authBearer},
			},
			{
				Key: keyUsername, Label: "Username", Type: domain.FieldText, Required: true,
				ShowWhen: map[string]string{keyAuth: authBasic},
				Help:     "For Grafana Cloud, the instance ID.",
			},
			{
				Key: keyPassword, Label: "Password", Type: domain.FieldSecret, Required: true, Secret: true,
				ShowWhen: map[string]string{keyAuth: authBasic},
				Help:     "For Grafana Cloud, an access policy token with metrics:read.",
			},
			{
				Key: keyHeaders, Label: "Extra headers", Type: domain.FieldHeaders, Secret: true,
				Placeholder: "X-Scope-OrgID: tenant-1",
				Help:        "One \"Name: value\" per line, sent with every request — a Mimir or Cortex tenant, or an Authorization header of your own. Stored encrypted.",
			},
		},
	}
}

type apiResponse struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
	Infos     []string        `json:"infos"`
}

type buildInfo struct {
	Version     string `json:"version"`
	Application string `json:"application"`
}

// Verify reads build info, falling back to a trivial instant query on any
// answer but success or an auth refusal: servers without the endpoint (older
// Mimir and Thanos, some proxies) answer 404, 400 or 501 for it.
func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets, a.verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.client.CloseIdleConnections()
	code, resp, err := c.call(ctx, http.MethodGet, "/api/v1/status/buildinfo", nil, maxSmallBody)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	switch {
	case code == http.StatusOK:
		if resp == nil || resp.Status != "success" {
			return domain.DataSourceInfo{}, errNotPrometheus
		}
		var bi buildInfo
		_ = json.Unmarshal(resp.Data, &bi)
		detail := bi.Application
		if detail == "" {
			detail = "Prometheus API"
		}
		return domain.DataSourceInfo{Version: bi.Version, Detail: detail}, nil
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return domain.DataSourceInfo{}, c.statusError("build info", code, resp)
	}

	code, resp, err = c.call(ctx, http.MethodGet, "/api/v1/query", url.Values{"query": {"1"}}, maxSmallBody)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	if code != http.StatusOK {
		return domain.DataSourceInfo{}, c.statusError("query", code, resp)
	}
	if resp == nil || resp.Status != "success" {
		return domain.DataSourceInfo{}, errNotPrometheus
	}
	return domain.DataSourceInfo{Detail: "Prometheus API (no build info)"}, nil
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

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	c, err := a.open(ctx, ds, secrets, a.queryTimeout)
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
	code, resp, err := c.call(ctx, http.MethodPost, "/api/v1/query_range", form, maxSeriesBody)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if code != http.StatusOK || resp == nil || resp.Status != "success" {
		return domain.SeriesResult{}, c.statusError("query", code, resp)
	}
	var data struct {
		ResultType string         `json:"resultType"`
		Result     []matrixSeries `json:"result"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return domain.SeriesResult{}, errNotPrometheus
	}
	if data.ResultType != "matrix" {
		return domain.SeriesResult{}, fmt.Errorf("query: expected a matrix result, got %q", data.ResultType)
	}
	out := domain.SeriesResult{Series: make([]domain.Series, 0, len(data.Result))}
	out.Warnings = append(out.Warnings, resp.Warnings...)
	out.Warnings = append(out.Warnings, resp.Infos...)
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

func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets, a.queryTimeout)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.client.CloseIdleConnections()
	now := a.now()
	params := url.Values{
		"start": {formatTime(now.Add(-discoverWindow))},
		"end":   {formatTime(now)},
	}
	code, resp, err := c.call(ctx, http.MethodGet, "/api/v1/label/__name__/values", params, maxInventoryBody)
	if err != nil {
		return domain.Inventory{}, err
	}
	if code != http.StatusOK || resp == nil || resp.Status != "success" {
		return domain.Inventory{}, c.statusError("metric names", code, resp)
	}
	var names []string
	if err := json.Unmarshal(resp.Data, &names); err != nil {
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
	config  map[string]string
	secrets map[string]string
	headers []domain.HeaderLine
	// basic is the encoded Authorization value, masked like any secret: a
	// server quoting the header it refused quotes this, not the password.
	basic string
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, timeout time.Duration) (*conn, error) {
	raw := strings.TrimSpace(ds.Config[keyURL])
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
	headers, err := domain.ParseHeaderLines(secrets[keyHeaders])
	if err != nil {
		return nil, fmt.Errorf("extra headers: %w", err)
	}
	client := a.policy.ClientFor(target, timeout)
	// A redirect to another host, or from https to http, would carry the
	// tenant and auth headers set below along with it.
	client.CheckRedirect = a.policy.SameHostRedirect(target.URL)
	c := &conn{base: target.URL, ips: target.IPs, client: client, config: ds.Config, secrets: secrets, headers: headers}
	if ds.Config[keyAuth] == authBasic {
		c.basic = base64.StdEncoding.EncodeToString([]byte(ds.Config[keyUsername] + ":" + secrets[keyPassword]))
	}
	return c, nil
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

// call answers the HTTP status and, when the body is Prometheus API JSON, the
// decoded envelope (nil otherwise). A GET sends values as the query string, a
// POST as a form.
func (c *conn) call(ctx context.Context, method, path string, values url.Values, limit int64) (int, *apiResponse, error) {
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
		return 0, nil, errors.New("the URL cannot be requested")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TaskTrooper")
	switch c.config[keyAuth] {
	case authBearer:
		req.Header.Set("Authorization", "Bearer "+c.secrets[keyToken])
	case authBasic:
		req.SetBasicAuth(c.config[keyUsername], c.secrets[keyPassword])
	}
	for _, h := range c.headers {
		req.Header.Set(h.Name, h.Value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, c.transportError(path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, nil, c.transportError(path, err)
	}
	if int64(len(raw)) > limit {
		return 0, nil, fmt.Errorf("%s: the response is larger than %d MiB; narrow the query or raise the step", path, limit>>20)
	}
	var env apiResponse
	if json.Unmarshal(raw, &env) != nil || env.Status == "" {
		return resp.StatusCode, nil, nil
	}
	return resp.StatusCode, &env, nil
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

func (c *conn) statusError(op string, code int, resp *apiResponse) error {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("%s: authentication failed (HTTP %d)", op, code)
	case resp == nil:
		if code == http.StatusOK {
			return errNotPrometheus
		}
		return fmt.Errorf("%s: HTTP %d", op, code)
	case resp.ErrorType == "bad_data":
		return fmt.Errorf("%w: %s", port.ErrDataSourceQuery, c.scrub(resp.Error))
	case resp.Error != "":
		return fmt.Errorf("%s: HTTP %d: %s: %s", op, code, resp.ErrorType, c.scrub(resp.Error))
	default:
		return fmt.Errorf("%s: HTTP %d", op, code)
	}
}

// scrub bounds upstream text, masks any secret a misbehaving server echoed
// back (an error quoting the Authorization header it rejected), and puts the
// typed host back where a resolved address of it appears.
func (c *conn) scrub(s string) string {
	masks := []string{c.basic}
	for _, v := range c.secrets {
		masks = append(masks, v)
	}
	for _, h := range c.headers {
		masks = append(masks, h.Value)
	}
	for _, v := range masks {
		if len(v) >= 4 {
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
