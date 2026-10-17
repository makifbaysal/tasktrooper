// Package rabbitmq is the data source kind that reads a RabbitMQ broker's
// queues through its management plugin's HTTP API, no exporter needed. It
// reads live state only: a series is one point, at the instant it is read.
// Its query language is domain.RabbitMQQuery.
package rabbitmq

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/live"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyURL      = "url"
	keyUsername = "username"
	keyPassword = "password"
	keyVhost    = "vhost"

	defaultVhost = "/"
	api          = "the RabbitMQ management API"

	defaultVerifyTimeout = 15 * time.Second
	defaultQueryTimeout  = 30 * time.Second
	maxBody              = 32 << 20

	// pageSize is the management API's largest page.
	pageSize = 500
	// maxPages bounds one listing however many pages the server claims, and
	// however few queues of each a name pattern keeps; past it the answer is
	// Truncated.
	maxPages = 40
	// DefaultMaxQueues bounds the queues one read or listing covers; a vhost
	// with more answers Truncated.
	DefaultMaxQueues = 2000
)

type Adapter struct {
	policy        urlguard.Policy
	verifyTimeout time.Duration
	queryTimeout  time.Duration
	maxQueues     int
	now           func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.QueueDiscoverer       = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy; datasource.NewService
// sets it.
func New() *Adapter {
	return &Adapter{
		verifyTimeout: defaultVerifyTimeout,
		queryTimeout:  defaultQueryTimeout,
		maxQueues:     DefaultMaxQueues,
		now:           time.Now,
	}
}

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetMaxQueues(n int) { a.maxQueues = n }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindRabbitMQ }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityQueues}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "RabbitMQ",
		Description: "A RabbitMQ broker's queues, read live through its management plugin — no exporter needed. Charts show only the current value; alerts and the queues table work directly.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "Management URL", Type: domain.FieldURL, Required: true,
				Placeholder: "http://localhost:15672",
				Help:        "The management plugin's base URL, with its path prefix if it has one.",
			},
			{
				Key: keyUsername, Label: "Username", Type: domain.FieldText, Required: true,
				Help: "A user with the monitoring tag is enough.",
			},
			{Key: keyPassword, Label: "Password", Type: domain.FieldSecret, Required: true, Secret: true},
			{
				Key: keyVhost, Label: "Virtual host", Type: domain.FieldText,
				Placeholder: defaultVhost,
				Help:        "The virtual host whose queues to read. Empty reads /.",
			},
		},
	}
}

type overview struct {
	RabbitMQVersion string `json:"rabbitmq_version"`
	ProductVersion  string `json:"product_version"`
	ClusterName     string `json:"cluster_name"`
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets, a.verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	var ov overview
	if err := c.Get(ctx, []string{"api", "overview"}, nil, maxBody, &ov); err != nil {
		return domain.DataSourceInfo{}, notFoundAs(err, errors.New("the URL answered, but not as the RabbitMQ management API — is it the management plugin's base URL?"))
	}
	if ov.RabbitMQVersion == "" && ov.ProductVersion == "" {
		return domain.DataSourceInfo{}, errors.New("the URL answered, but not as the RabbitMQ management API — is it the management plugin's base URL?")
	}
	vhost := vhostOf(ds)
	var vh struct {
		Name string `json:"name"`
	}
	if err := c.Get(ctx, []string{"api", "vhosts", vhost}, nil, maxBody, &vh); err != nil {
		return domain.DataSourceInfo{}, notFoundAs(err, fmt.Errorf("the virtual host %q does not exist, or this user cannot see it", vhost))
	}
	version := ov.RabbitMQVersion
	if version == "" {
		version = ov.ProductVersion
	}
	detail := "RabbitMQ"
	if ov.ClusterName != "" {
		detail += " · " + ov.ClusterName
	}
	return domain.DataSourceInfo{Version: version, Detail: detail + " · vhost " + vhost}, nil
}

type queueInfo struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Messages  *int64 `json:"messages"`
	Ready     *int64 `json:"messages_ready"`
	Unacked   *int64 `json:"messages_unacknowledged"`
	Consumers *int64 `json:"consumers"`
	Stats     struct {
		Publish    rate `json:"publish_details"`
		DeliverGet rate `json:"deliver_get_details"`
		Ack        rate `json:"ack_details"`
	} `json:"message_stats"`
}

type rate struct {
	Rate *float64 `json:"rate"`
}

// value: the management API leaves a rate out until the queue has seen that
// event, which is a rate of zero, not a gap.
func (q queueInfo) value(metric string) *float64 {
	count := func(v *int64) *float64 {
		if v == nil {
			return nil
		}
		return live.Float(*v)
	}
	perSecond := func(r rate) *float64 {
		if r.Rate == nil {
			zero := 0.0
			return &zero
		}
		return r.Rate
	}
	switch metric {
	case domain.RabbitMQMetricReady:
		return count(q.Ready)
	case domain.RabbitMQMetricUnacked:
		return count(q.Unacked)
	case domain.RabbitMQMetricConsumers:
		return count(q.Consumers)
	case domain.RabbitMQMetricPublishRate:
		return perSecond(q.Stats.Publish)
	case domain.RabbitMQMetricDeliverRate:
		return perSecond(q.Stats.DeliverGet)
	case domain.RabbitMQMetricAckRate:
		return perSecond(q.Stats.Ack)
	}
	return count(q.Messages)
}

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	query, err := domain.ParseRabbitMQQuery(q.Query)
	if err != nil {
		return domain.SeriesResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	at, ok := live.Instant(q.To, a.now())
	if !ok {
		return domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{live.PastWindow("RabbitMQ")}}, nil
	}
	queues, truncated, err := a.queues(ctx, ds, secrets, query.Queue)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: make([]domain.Series, 0, len(queues))}
	if len(queues) == 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("no queue matches %q in the virtual host %q", query.Queue, vhostOf(ds)))
	}
	if truncated {
		res.Warnings = append(res.Warnings, fmt.Sprintf("more than %d queues match; showing the first", a.maxQueues))
	}
	for _, qi := range queues {
		res.Series = append(res.Series, domain.Series{
			Name:   query.Metric,
			Labels: map[string]string{"queue": qi.Name, "vhost": vhostOf(ds)},
			Points: live.Point(at, qi.value(query.Metric)),
		})
	}
	return res, nil
}

var keyColumns = []string{"queue"}

var tableColumns = []domain.TableColumn{
	{Key: "queue", Label: "Queue"},
	{Key: "state", Label: "State"},
	{Key: "ready", Label: "Ready", Unit: "messages"},
	{Key: "unacked", Label: "Unacked", Unit: "messages"},
	{Key: "consumers", Label: "Consumers", Unit: "count"},
	{Key: "publish_rate", Label: "Publish rate", Unit: "msg/s"},
	{Key: "deliver_rate", Label: "Deliver rate", Unit: "msg/s"},
	{Key: "ack_rate", Label: "Ack rate", Unit: "msg/s"},
}

// Table answers one row per queue the query's name (or * pattern) matches;
// its metric is not used.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TableQuery) (domain.TableResult, error) {
	query, err := domain.ParseRabbitMQQuery(q.Query)
	if err != nil {
		return domain.TableResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	res := domain.TableResult{Columns: tableColumns, KeyColumns: keyColumns, Rows: []map[string]any{}}
	if _, ok := live.Instant(q.At, a.now()); !ok {
		return res, nil
	}
	queues, truncated, err := a.queues(ctx, ds, secrets, query.Queue)
	if err != nil {
		return domain.TableResult{}, err
	}
	res.Truncated = truncated
	for _, qi := range queues {
		row := map[string]any{"queue": qi.Name}
		if qi.State != "" {
			row["state"] = qi.State
		}
		for key, metric := range map[string]string{
			"ready": domain.RabbitMQMetricReady, "unacked": domain.RabbitMQMetricUnacked,
			"consumers": domain.RabbitMQMetricConsumers, "publish_rate": domain.RabbitMQMetricPublishRate,
			"deliver_rate": domain.RabbitMQMetricDeliverRate, "ack_rate": domain.RabbitMQMetricAckRate,
		} {
			if v := qi.value(metric); v != nil {
				row[key] = *v
			}
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// Discover lists the virtual host's queues as "queue/<name>".
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets, a.queryTimeout)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	queues, truncated, err := a.list(ctx, c, vhostOf(ds), url.Values{"columns": {"name"}}, nil)
	if err != nil {
		return domain.Inventory{}, err
	}
	inv := domain.Inventory{Metrics: make([]string, 0, len(queues)), Truncated: truncated}
	for _, qi := range queues {
		inv.Metrics = append(inv.Metrics, "queue/"+qi.Name)
	}
	sort.Strings(inv.Metrics)
	return inv, nil
}

// DiscoverQueues offers every queue of the virtual host by its name. One
// whose name holds a * is left out: a query would read it as a pattern.
func (a *Adapter) DiscoverQueues(ctx context.Context, ds domain.DataSource, secrets map[string]string) ([]domain.QueueTarget, error) {
	c, err := a.open(ctx, ds, secrets, a.queryTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	queues, _, err := a.list(ctx, c, vhostOf(ds), url.Values{"columns": {"name"}}, nil)
	if err != nil {
		return nil, err
	}
	out := make([]domain.QueueTarget, 0, len(queues))
	for _, qi := range queues {
		if strings.Contains(qi.Name, "*") {
			continue
		}
		out = append(out, domain.QueueTarget{Name: qi.Name, Params: map[string]string{"queue": qi.Name}})
	}
	return out, nil
}

// queues reads one queue by name, or every queue a * pattern matches.
func (a *Adapter) queues(ctx context.Context, ds domain.DataSource, secrets map[string]string, name string) ([]queueInfo, bool, error) {
	c, err := a.open(ctx, ds, secrets, a.queryTimeout)
	if err != nil {
		return nil, false, err
	}
	defer c.Close()
	vhost := vhostOf(ds)
	if !strings.Contains(name, "*") {
		var qi queueInfo
		err := c.Get(ctx, []string{"api", "queues", vhost, name}, nil, maxBody, &qi)
		if errors.Is(err, live.ErrNotFound) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return []queueInfo{qi}, false, nil
	}
	pattern := globPattern(name)
	filter := url.Values{"name": {pattern}, "use_regex": {"true"}}
	return a.list(ctx, c, vhost, filter, regexp.MustCompile(pattern))
}

type queuePage struct {
	Items         []queueInfo `json:"items"`
	PageCount     int         `json:"page_count"`
	FilteredCount int         `json:"filtered_count"`
}

// list pages through the vhost's queues. keep re-applies the server-side name
// filter, so a server ignoring it still answers only the queues asked for.
func (a *Adapter) list(ctx context.Context, c *live.Conn, vhost string, extra url.Values, keep *regexp.Regexp) ([]queueInfo, bool, error) {
	var out []queueInfo
	for page := 1; page <= maxPages; page++ {
		params := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(pageSize)}}
		for k, v := range extra {
			params[k] = v
		}
		var p queuePage
		err := c.Get(ctx, []string{"api", "queues", vhost}, params, maxBody, &p)
		if errors.Is(err, live.ErrNotFound) {
			return nil, false, fmt.Errorf("the virtual host %q does not exist, or this user cannot see it", vhost)
		}
		if err != nil {
			return nil, false, err
		}
		for _, qi := range p.Items {
			if keep != nil && !keep.MatchString(qi.Name) {
				continue
			}
			if len(out) == a.maxQueues {
				return out, true, nil
			}
			out = append(out, qi)
		}
		if page >= p.PageCount || len(p.Items) == 0 {
			return out, false, nil
		}
	}
	return out, true, nil
}

// globPattern turns a queue name with * wildcards into an anchored regular
// expression, for the management API's use_regex filter and for re-checking
// what it answers.
func globPattern(glob string) string {
	parts := strings.Split(glob, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return "^" + strings.Join(parts, ".*") + "$"
}

func vhostOf(ds domain.DataSource) string {
	if v := strings.TrimSpace(ds.Config[keyVhost]); v != "" {
		return v
	}
	return defaultVhost
}

// open masks the encoded basic-auth value beside the password: a server
// quoting the header it refused quotes that, not the password.
func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, timeout time.Duration) (*live.Conn, error) {
	basic := base64.StdEncoding.EncodeToString([]byte(ds.Config[keyUsername] + ":" + secrets[keyPassword]))
	ep := live.Endpoint{
		Base:    ds.Config[keyURL],
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Basic " + basic}},
		Masks:   []string{secrets[keyPassword], basic},
		API:     api,
	}
	return live.Open(ctx, a.policy, ep, timeout)
}

func notFoundAs(err, as error) error {
	if errors.Is(err, live.ErrNotFound) {
		return as
	}
	return err
}
