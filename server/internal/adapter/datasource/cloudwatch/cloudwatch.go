// Package cloudwatch is the data source kind for Amazon CloudWatch metrics,
// read with a connected AWS account: GetMetricData for series and
// ListMetrics for discovery, over CloudWatch's query protocol, signed with
// SigV4 from the account's own keys. Its query language is
// domain.CloudWatchQuery.
package cloudwatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	awscloud "github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/aws"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyAccount = "cloud_account_id"
	keyRegion  = "region"

	signingService = "monitoring"
	apiVersion     = "2010-08-01"

	defaultVerifyTimeout = 15 * time.Second
	defaultQueryTimeout  = 30 * time.Second
	maxBody              = 16 << 20

	maxDataPages = 20
	maxListPages = 20
	// DefaultMaxInventory bounds the metric names Discover returns.
	DefaultMaxInventory = 5000
	maxSeriesPoints     = 11000
	// recentlyActive keeps discovery to metrics that had data lately, as the
	// Prometheus kinds do with their one-hour window.
	recentlyActive = "PT3H"

	minPeriod = 60
	// After 15 days CloudWatch keeps only 5-minute data and after 63 days
	// only hourly; a finer period over that range returns nothing.
	fiveMinuteAge = 15 * 24 * time.Hour
	hourlyAge     = 63 * 24 * time.Hour

	maxUpstreamText = 300
	maskMinLen      = 4
)

var regionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d{1,2}$`)

type Adapter struct {
	policy        urlguard.Policy
	accounts      port.CloudAccountReader
	baseURL       string
	signer        *v4.Signer
	verifyTimeout time.Duration
	queryTimeout  time.Duration
	maxInventory  int
	now           func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.MetricDiscoverer      = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination until SetURLPolicy
// and reads no account until SetCloudAccounts; datasource.NewService sets
// both.
func New() *Adapter {
	return &Adapter{
		signer:        v4.NewSigner(),
		verifyTimeout: defaultVerifyTimeout,
		queryTimeout:  defaultQueryTimeout,
		maxInventory:  DefaultMaxInventory,
		now:           time.Now,
	}
}

// SetURLPolicy applies to the regional endpoint like to any other address.
func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetCloudAccounts(r port.CloudAccountReader) { a.accounts = r }

// SetBaseURL replaces https://monitoring.<region>.amazonaws.com for every
// region; tests only.
func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) SetMaxInventory(n int) { a.maxInventory = n }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindCloudWatch }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Amazon CloudWatch",
		Description: "Load balancers, ECS, Lambda, SQS, MSK and every other AWS metric, read with a connected AWS account. Nothing to install.",
		Fields: []domain.DataSourceField{
			{
				Key: keyAccount, Label: "AWS account", Type: domain.FieldCloudAccount,
				Provider: domain.CloudAWS, Required: true,
				Help: "Its access key needs cloudwatch:GetMetricData and cloudwatch:ListMetrics.",
			},
			{
				Key: keyRegion, Label: "Region", Type: domain.FieldText,
				Placeholder: "the account's region",
				Help:        "The region whose metrics to read. Empty reads the account's own.",
			},
		},
	}
}

type client struct {
	base   *url.URL
	host   string
	ips    []net.IP
	http   *http.Client
	creds  aws.Credentials
	region string
	masks  []string
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, a.verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.http.CloseIdleConnections()
	var out listMetricsResponse
	if err := a.call(ctx, c, url.Values{"Action": {"ListMetrics"}, "RecentlyActive": {recentlyActive}}, &out); err != nil {
		return domain.DataSourceInfo{}, err
	}
	return domain.DataSourceInfo{Detail: "Amazon CloudWatch · " + c.region}, nil
}

type message struct {
	Code  string `xml:"Code"`
	Value string `xml:"Value"`
}

type metricDataResult struct {
	ID         string    `xml:"Id"`
	Label      string    `xml:"Label"`
	StatusCode string    `xml:"StatusCode"`
	Timestamps []string  `xml:"Timestamps>member"`
	Values     []string  `xml:"Values>member"`
	Messages   []message `xml:"Messages>member"`
}

type getMetricDataResponse struct {
	Result struct {
		MetricDataResults []metricDataResult `xml:"MetricDataResults>member"`
		NextToken         string             `xml:"NextToken"`
		Messages          []message          `xml:"Messages>member"`
	} `xml:"GetMetricDataResult"`
}

type seriesKey struct{ id, label string }

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, _ map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	query, err := domain.ParseCloudWatchQuery(q.Query)
	if err != nil {
		return domain.SeriesResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	c, err := a.open(ctx, ds, a.queryTimeout)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.http.CloseIdleConnections()

	period := periodFor(query.Period, q.Step, q.From, a.now())
	form, names := metricDataForm(query, q.From, q.To, period)
	var (
		order    []seriesKey
		points   = map[seriesKey][]domain.SeriesPoint{}
		warnings []string
		capped   bool
	)
	warn := func(w string) {
		if w = c.scrub(w); !slices.Contains(warnings, w) {
			warnings = append(warnings, w)
		}
	}
	next := ""
	for page := 0; ; page++ {
		if page == maxDataPages {
			warn(fmt.Sprintf("stopped after %d pages of data; narrow the range or raise the step", maxDataPages))
			break
		}
		if next != "" {
			form.Set("NextToken", next)
		}
		var out getMetricDataResponse
		if err := a.call(ctx, c, form, &out); err != nil {
			return domain.SeriesResult{}, err
		}
		for _, m := range out.Result.Messages {
			warn(m.Code + ": " + m.Value)
		}
		for _, r := range out.Result.MetricDataResults {
			key := seriesKey{r.ID, r.Label}
			if _, ok := points[key]; !ok {
				order = append(order, key)
				points[key] = []domain.SeriesPoint{}
			}
			switch r.StatusCode {
			case "Complete", "PartialData", "":
			default:
				warn(fmt.Sprintf("%s: %s", label(r, names), r.StatusCode))
			}
			for _, m := range r.Messages {
				warn(fmt.Sprintf("%s: %s: %s", label(r, names), m.Code, m.Value))
			}
			n := min(len(r.Timestamps), len(r.Values))
			for i := 0; i < n; i++ {
				if len(points[key]) == maxSeriesPoints {
					capped = true
					break
				}
				t, terr := parseTime(r.Timestamps[i])
				v, verr := strconv.ParseFloat(strings.TrimSpace(r.Values[i]), 64)
				if terr != nil {
					continue
				}
				if verr != nil {
					v = math.NaN()
				}
				points[key] = append(points[key], domain.FinitePoint(t, v))
			}
		}
		next = out.Result.NextToken
		if next == "" {
			break
		}
	}
	if capped {
		warn(fmt.Sprintf("a series has more than %d points; showing the first", maxSeriesPoints))
	}

	res := domain.SeriesResult{
		Series: make([]domain.Series, 0, len(order)), Warnings: warnings,
		Step: time.Duration(period) * time.Second,
	}
	for _, key := range order {
		pts := points[key]
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })
		s := domain.Series{Name: key.label, Labels: map[string]string{}, Points: pts}
		if query.Expression == "" {
			s.Name = query.Metric
			for k, v := range query.Dimensions {
				s.Labels[k] = v
			}
			s.Labels["stat"] = query.Stat
		} else if s.Name == "" {
			s.Name = query.Expression
		}
		res.Series = append(res.Series, s)
	}
	return res, nil
}

func label(r metricDataResult, names map[string]string) string {
	if r.Label != "" {
		return r.Label
	}
	if n := names[r.ID]; n != "" {
		return n
	}
	return r.ID
}

// periodFor rounds the step up to a whole minute, then to what CloudWatch
// still keeps for a range starting that long ago. An explicit period is the
// query author's choice and is kept.
func periodFor(explicit int, step time.Duration, from, now time.Time) int {
	if explicit > 0 {
		return explicit
	}
	p := max(int(math.Ceil(step.Seconds()/minPeriod))*minPeriod, minPeriod)
	switch age := now.Sub(from); {
	case age > hourlyAge:
		p = roundUp(p, 3600)
	case age > fiveMinuteAge:
		p = roundUp(p, 300)
	}
	return p
}

func roundUp(n, unit int) int { return (n + unit - 1) / unit * unit }

const singleID = "m"

// metricDataForm answers the request and, per query id, the name a warning
// calls it by. An expression's own id is the first of e, e1, e2… the metrics
// do not use.
func metricDataForm(q domain.CloudWatchQuery, from, to time.Time, period int) (url.Values, map[string]string) {
	v := url.Values{
		"Action":    {"GetMetricData"},
		"StartTime": {from.UTC().Format(time.RFC3339)},
		"EndTime":   {to.UTC().Format(time.RFC3339)},
		"ScanBy":    {"TimestampAscending"},
	}
	names := map[string]string{}
	if q.Expression == "" {
		addMetricStat(v, 1, singleID, q.CloudWatchMetric, period, true)
		names[singleID] = q.Metric
		return v, names
	}
	ids := make([]string, 0, len(q.Metrics))
	for id := range q.Metrics {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i, id := range ids {
		addMetricStat(v, i+1, id, q.Metrics[id], period, false)
		names[id] = q.Metrics[id].Metric
	}
	exprID := "e"
	for n := 1; taken(q.Metrics, exprID); n++ {
		exprID = "e" + strconv.Itoa(n)
	}
	p := member(len(ids) + 1)
	v.Set(p+"Id", exprID)
	v.Set(p+"Expression", q.Expression)
	v.Set(p+"Period", strconv.Itoa(period))
	v.Set(p+"ReturnData", "true")
	names[exprID] = q.Expression
	return v, names
}

func member(n int) string { return "MetricDataQueries.member." + strconv.Itoa(n) + "." }

func addMetricStat(v url.Values, n int, id string, m domain.CloudWatchMetric, period int, returnData bool) {
	p := member(n)
	v.Set(p+"Id", id)
	v.Set(p+"MetricStat.Metric.Namespace", m.Namespace)
	v.Set(p+"MetricStat.Metric.MetricName", m.Metric)
	dims := make([]string, 0, len(m.Dimensions))
	for name := range m.Dimensions {
		dims = append(dims, name)
	}
	sort.Strings(dims)
	for i, name := range dims {
		d := p + "MetricStat.Metric.Dimensions.member." + strconv.Itoa(i+1) + "."
		v.Set(d+"Name", name)
		v.Set(d+"Value", m.Dimensions[name])
	}
	v.Set(p+"MetricStat.Period", strconv.Itoa(period))
	v.Set(p+"MetricStat.Stat", m.Stat)
	v.Set(p+"ReturnData", strconv.FormatBool(returnData))
}

type listMetricsResponse struct {
	Result struct {
		Metrics []struct {
			Namespace  string `xml:"Namespace"`
			MetricName string `xml:"MetricName"`
		} `xml:"Metrics>member"`
		NextToken string `xml:"NextToken"`
	} `xml:"ListMetricsResult"`
}

// Discover lists "<namespace>/<metric>" for every metric with data in the
// last three hours; ListMetrics answers one row per dimension set, so names
// repeat across rows and the page cap bounds the work, not the names.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, _ map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, a.queryTimeout)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.http.CloseIdleConnections()
	seen := map[string]bool{}
	truncated := false
	form := url.Values{"Action": {"ListMetrics"}, "RecentlyActive": {recentlyActive}}
	for page := 0; ; page++ {
		if page == maxListPages {
			truncated = true
			break
		}
		var out listMetricsResponse
		if err := a.call(ctx, c, form, &out); err != nil {
			return domain.Inventory{}, err
		}
		for _, m := range out.Result.Metrics {
			seen[m.Namespace+"/"+m.MetricName] = true
		}
		if out.Result.NextToken == "" {
			break
		}
		form.Set("NextToken", out.Result.NextToken)
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > a.maxInventory {
		names = names[:a.maxInventory]
		truncated = true
	}
	return domain.Inventory{Metrics: names, Truncated: truncated}, nil
}

// DiscoverMetric asks ListMetrics about one "<namespace>/<metric>" with data
// in the last three hours: on an account large enough that Discover stops at
// its page cap, a recipe's detect metric may lie past where it stopped.
func (a *Adapter) DiscoverMetric(ctx context.Context, ds domain.DataSource, _ map[string]string, metric string) (bool, error) {
	i := strings.LastIndex(metric, "/")
	if i <= 0 || i == len(metric)-1 {
		return false, nil
	}
	c, err := a.open(ctx, ds, a.queryTimeout)
	if err != nil {
		return false, err
	}
	defer c.http.CloseIdleConnections()
	var out listMetricsResponse
	form := url.Values{
		"Action": {"ListMetrics"}, "Namespace": {metric[:i]}, "MetricName": {metric[i+1:]}, "RecentlyActive": {recentlyActive},
	}
	if err := a.call(ctx, c, form, &out); err != nil {
		return false, err
	}
	return len(out.Result.Metrics) > 0, nil
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, timeout time.Duration) (*client, error) {
	if a.accounts == nil {
		return nil, errors.New("cloud accounts are not available here")
	}
	id, err := uuid.Parse(ds.Config[keyAccount])
	if err != nil {
		return nil, errors.New("no AWS account is selected")
	}
	cred, err := a.accounts.CloudCredential(ctx, id)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return nil, errors.New("the AWS account is no longer connected")
	case err != nil:
		return nil, fmt.Errorf("reading the AWS account: %w", err)
	case cred.Provider != domain.CloudAWS:
		return nil, errors.New("the selected cloud account is not an AWS account")
	}
	creds, accountRegion, err := awscloud.SigningCredentials(ctx, cred)
	if err != nil {
		return nil, errors.New("the AWS account has no usable access key")
	}
	region := strings.TrimSpace(ds.Config[keyRegion])
	if region == "" {
		region = cred.Meta["region"]
	}
	if region == "" {
		region = accountRegion
	}
	if !regionPattern.MatchString(region) {
		return nil, fmt.Errorf("%q is not an AWS region", region)
	}
	raw := a.baseURL
	if raw == "" {
		raw = endpointFor(region)
	}
	target, err := a.policy.Validate(ctx, raw+"/")
	if err != nil {
		return nil, refused(err, raw)
	}
	hc := a.policy.ClientFor(target, timeout)
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client{
		base: target.URL, host: target.Host, ips: target.IPs, http: hc, creds: creds, region: region,
		masks: []string{creds.SecretAccessKey, creds.SessionToken, creds.AccessKeyID},
	}, nil
}

func endpointFor(region string) string {
	if strings.HasPrefix(region, "cn-") {
		return "https://monitoring." + region + ".amazonaws.com.cn"
	}
	return "https://monitoring." + region + ".amazonaws.com"
}

func refused(err error, raw string) error {
	log.Warn().Err(err).Str("url", urlguard.LogRaw(raw)).Msg("data source: destination refused")
	return refusedError{}
}

type refusedError struct{}

func (refusedError) Error() string        { return "the CloudWatch endpoint is not an allowed destination" }
func (refusedError) Is(target error) bool { return target == urlguard.ErrBlocked }

type errorResponse struct {
	Error struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error"`
}

var authCodes = map[string]bool{
	"InvalidClientTokenId": true, "SignatureDoesNotMatch": true, "AccessDenied": true,
	"AccessDeniedException": true, "ExpiredToken": true, "UnrecognizedClientException": true,
	"IncompleteSignature": true, "MissingAuthenticationToken": true, "InvalidSignatureException": true,
}

var queryCodes = map[string]bool{
	"InvalidParameterValue": true, "InvalidParameterCombination": true, "MissingParameter": true,
	"ValidationError": true, "InvalidNextToken": true,
}

func (a *Adapter) call(ctx context.Context, c *client, form url.Values, out any) error {
	form.Set("Version", apiVersion)
	body := form.Encode()
	op := form.Get("Action")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.String(), strings.NewReader(body))
	if err != nil {
		return errors.New("the CloudWatch endpoint cannot be requested")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("User-Agent", "TaskTrooper")
	sum := sha256.Sum256([]byte(body))
	if err := a.signer.SignHTTP(ctx, c.creds, req, hex.EncodeToString(sum[:]), signingService, c.region, a.now()); err != nil {
		return errors.New("signing the CloudWatch request failed")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.transportError(op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return c.transportError(op, err)
	}
	if len(raw) > maxBody {
		return fmt.Errorf("%s: the response is larger than %d MiB; narrow the range or raise the step", op, maxBody>>20)
	}
	if resp.StatusCode != http.StatusOK {
		var e errorResponse
		_ = xml.Unmarshal(raw, &e)
		code, msg := e.Error.Code, c.scrub(e.Error.Message)
		switch {
		case authCodes[code] || (code == "" && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden)):
			return fmt.Errorf("%s: authentication failed (HTTP %d %s): %s", op, resp.StatusCode, code, msg)
		case queryCodes[code]:
			return fmt.Errorf("%w: %s: %s", port.ErrDataSourceQuery, code, msg)
		case code != "":
			return fmt.Errorf("%s: HTTP %d: %s: %s", op, resp.StatusCode, code, msg)
		default:
			return fmt.Errorf("%s: HTTP %d", op, resp.StatusCode)
		}
	}
	if err := xml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: the answer is not CloudWatch's", op)
	}
	return nil
}

func (c *client) transportError(op string, err error) error {
	if errors.Is(err, urlguard.ErrBlocked) {
		return refused(err, c.base.String())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out", op)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return fmt.Errorf("%s: timed out", op)
		}
		err = ue.Err
	}
	return fmt.Errorf("%s: cannot reach %s: %s", op, c.host, c.scrub(err.Error()))
}

// scrub bounds upstream text, masks the access key, secret and session token
// should an answer quote one, and names the host rather than the address it
// resolved to.
func (c *client) scrub(s string) string {
	for _, v := range c.masks {
		if len(v) >= maskMinLen {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	for _, ip := range c.ips {
		s = strings.ReplaceAll(s, ip.String(), c.host)
	}
	if r := []rune(s); len(r) > maxUpstreamText {
		s = string(r[:maxUpstreamText]) + "…"
	}
	return s
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func taken(metrics map[string]domain.CloudWatchMetric, id string) bool {
	_, ok := metrics[id]
	return ok
}

// tableWindow is how far back a table read looks for each series' latest
// datapoint: CloudWatch publishes most metrics a few minutes late.
const tableWindow = 20 * time.Minute

// Table reads the query over the 20 minutes up to q.At at a one-minute
// period and answers each series' latest datapoint as a row: its labels (a
// single metric's dimensions and stat) and the value under
// domain.TableValueKey. GetMetricData answers no dimensions for an
// expression's series, so such a row is the value alone.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TableQuery) (domain.TableResult, error) {
	at := q.At
	if at.IsZero() {
		at = a.now()
	}
	res, err := a.Series(ctx, ds, secrets, domain.SeriesQuery{Query: q.Query, From: at.Add(-tableWindow), To: at, Step: time.Minute})
	if err != nil {
		return domain.TableResult{}, err
	}
	labels := map[string]bool{}
	out := domain.TableResult{Rows: make([]map[string]any, 0, len(res.Series))}
	for _, s := range res.Series {
		row := map[string]any{domain.TableValueKey: nil}
		for k, v := range s.Labels {
			if k != domain.TableValueKey {
				labels[k] = true
				row[k] = v
			}
		}
		for i := len(s.Points) - 1; i >= 0; i-- {
			if v := s.Points[i].Value; v != nil {
				row[domain.TableValueKey] = *v
				break
			}
		}
		out.Rows = append(out.Rows, row)
	}
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		out.Columns = append(out.Columns, domain.TableColumn{Key: k, Label: k})
	}
	out.Columns = append(out.Columns, domain.TableColumn{Key: domain.TableValueKey, Label: domain.TableValueKey})
	return out, nil
}
