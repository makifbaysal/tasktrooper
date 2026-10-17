// Package nats is the data source kind that reads a NATS server's JetStream
// consumers through its HTTP monitoring endpoint (/varz, /jsz), no exporter
// needed. It reads live state only: a series is one point, at the instant it
// is read. Its query language is domain.NATSQuery.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/live"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyURL   = "url"
	keyToken = "token"

	api = "the NATS monitoring endpoint"

	defaultVerifyTimeout = 15 * time.Second
	defaultQueryTimeout  = 30 * time.Second
	maxBody              = 32 << 20

	// DefaultMaxInventory bounds the streams and consumers Discover lists.
	DefaultMaxInventory = 2000
	maxRows             = 2000
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
		maxInventory:  DefaultMaxInventory,
		now:           time.Now,
	}
}

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetMaxInventory(n int) { a.maxInventory = n }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindNATS }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityQueues}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "NATS JetStream",
		Description: "A NATS server's JetStream streams and consumers, read live from its monitoring endpoint — no exporter needed. Charts show only the current value; alerts and the queues table work directly.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "Monitoring URL", Type: domain.FieldURL, Required: true,
				Placeholder: "http://localhost:8222",
				Help:        "The server's HTTP monitoring endpoint (nats-server -m 8222). In a cluster, a server that holds the streams to watch.",
			},
			{
				Key: keyToken, Label: "Bearer token", Type: domain.FieldSecret, Secret: true,
				Help: "Only for a monitoring endpoint behind a proxy that asks for one; sent as Authorization: Bearer.",
			},
		},
	}
}

type varz struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Version    string `json:"version"`
	JetStream  *struct {
		Config json.RawMessage `json:"config"`
	} `json:"jetstream"`
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets, a.verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	var v varz
	if err := c.Get(ctx, []string{"varz"}, nil, maxBody, &v); err != nil {
		if errors.Is(err, live.ErrNotFound) {
			return domain.DataSourceInfo{}, errNotNATS
		}
		return domain.DataSourceInfo{}, err
	}
	if v.ServerID == "" || v.Version == "" {
		return domain.DataSourceInfo{}, errNotNATS
	}
	detail := "NATS"
	if v.ServerName != "" {
		detail += " · " + v.ServerName
	}
	if v.JetStream == nil || len(v.JetStream.Config) == 0 || string(v.JetStream.Config) == "null" {
		detail += " · JetStream off"
	}
	return domain.DataSourceInfo{Version: v.Version, Detail: detail}, nil
}

var errNotNATS = errors.New("the URL answered, but not as a NATS monitoring endpoint — is it the -m port, usually 8222?")

type jsz struct {
	AccountDetails []struct {
		Name    string `json:"name"`
		Streams []struct {
			Name      string     `json:"name"`
			Consumers []consumer `json:"consumer_detail"`
		} `json:"stream_detail"`
	} `json:"account_details"`
}

type consumer struct {
	Name           string `json:"name"`
	NumPending     int64  `json:"num_pending"`
	NumAckPending  int64  `json:"num_ack_pending"`
	NumRedelivered int64  `json:"num_redelivered"`
	NumWaiting     int64  `json:"num_waiting"`
}

func (c consumer) value(metric string) *float64 {
	switch metric {
	case domain.NATSMetricAckPending:
		return live.Float(c.NumAckPending)
	case domain.NATSMetricRedelivered:
		return live.Float(c.NumRedelivered)
	case domain.NATSMetricWaiting:
		return live.Float(c.NumWaiting)
	}
	return live.Float(c.NumPending)
}

type match struct {
	account, stream string
	consumer
}

// read answers every consumer of the query's stream (only the one it names,
// when it names one), across the accounts the server reports, and whether
// the stream was there at all.
func (a *Adapter) read(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.NATSQuery) ([]match, bool, error) {
	j, err := a.fetch(ctx, ds, secrets)
	if err != nil {
		return nil, false, err
	}
	var out []match
	found := false
	for _, acct := range j.AccountDetails {
		for _, s := range acct.Streams {
			if s.Name != q.Stream {
				continue
			}
			found = true
			for _, cons := range s.Consumers {
				if q.Consumer == "" || cons.Name == q.Consumer {
					out = append(out, match{account: acct.Name, stream: s.Name, consumer: cons})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].account != out[k].account {
			return out[i].account < out[k].account
		}
		return out[i].Name < out[k].Name
	})
	return out, found, nil
}

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	query, err := domain.ParseNATSQuery(q.Query)
	if err != nil {
		return domain.SeriesResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	at, ok := live.Instant(q.To, a.now())
	if !ok {
		return domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{live.PastWindow("NATS")}}, nil
	}
	matches, found, err := a.read(ctx, ds, secrets, query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: make([]domain.Series, 0, len(matches))}
	switch {
	case !found:
		res.Warnings = append(res.Warnings, fmt.Sprintf("this server holds no stream %q", query.Stream))
	case len(matches) == 0 && query.Consumer != "":
		res.Warnings = append(res.Warnings, fmt.Sprintf("the stream %q has no consumer %q", query.Stream, query.Consumer))
	}
	for _, m := range matches {
		res.Series = append(res.Series, domain.Series{
			Name:   query.Metric,
			Labels: map[string]string{"account": m.account, "stream": m.stream, "consumer": m.Name},
			Points: live.Point(at, m.value(query.Metric)),
		})
	}
	return res, nil
}

var keyColumns = []string{"consumer"}

var tableColumns = []domain.TableColumn{
	{Key: "consumer", Label: "Consumer"},
	{Key: "pending", Label: "Pending", Unit: "messages"},
	{Key: "ack_pending", Label: "Ack pending", Unit: "messages"},
	{Key: "redelivered", Label: "Redelivered", Unit: "messages"},
	{Key: "waiting", Label: "Waiting pulls", Unit: "count"},
}

// Table answers one row per consumer of the query's stream; its metric is
// not used.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TableQuery) (domain.TableResult, error) {
	query, err := domain.ParseNATSQuery(q.Query)
	if err != nil {
		return domain.TableResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	res := domain.TableResult{Columns: tableColumns, KeyColumns: keyColumns, Rows: []map[string]any{}}
	if _, ok := live.Instant(q.At, a.now()); !ok {
		return res, nil
	}
	matches, _, err := a.read(ctx, ds, secrets, query)
	if err != nil {
		return domain.TableResult{}, err
	}
	for _, m := range matches {
		if len(res.Rows) == maxRows {
			res.Truncated = true
			break
		}
		res.Rows = append(res.Rows, map[string]any{
			"consumer":    m.Name,
			"pending":     float64(m.NumPending),
			"ack_pending": float64(m.NumAckPending),
			"redelivered": float64(m.NumRedelivered),
			"waiting":     float64(m.NumWaiting),
		})
	}
	return res, nil
}

// Discover lists "stream/<name>" and "consumer/<stream>/<name>" for every
// JetStream stream and consumer the server reports.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	j, err := a.fetch(ctx, ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	seen := map[string]bool{}
	for _, acct := range j.AccountDetails {
		for _, s := range acct.Streams {
			seen["stream/"+s.Name] = true
			for _, cons := range s.Consumers {
				seen["consumer/"+s.Name+"/"+cons.Name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	inv := domain.Inventory{Metrics: names}
	if len(names) > a.maxInventory {
		inv.Metrics = names[:a.maxInventory]
		inv.Truncated = true
	}
	return inv, nil
}

// DiscoverQueues offers every consumer of every stream, named by the
// consumer.
func (a *Adapter) DiscoverQueues(ctx context.Context, ds domain.DataSource, secrets map[string]string) ([]domain.QueueTarget, error) {
	j, err := a.fetch(ctx, ds, secrets)
	if err != nil {
		return nil, err
	}
	var out []domain.QueueTarget
	seen := map[string]bool{}
	for _, acct := range j.AccountDetails {
		for _, s := range acct.Streams {
			for _, cons := range s.Consumers {
				if seen[s.Name+"\x00"+cons.Name] || len(out) == a.maxInventory {
					continue
				}
				seen[s.Name+"\x00"+cons.Name] = true
				out = append(out, domain.QueueTarget{Name: cons.Name, Params: map[string]string{"stream": s.Name, "consumer": cons.Name}})
			}
		}
	}
	sort.SliceStable(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out, nil
}

func (a *Adapter) fetch(ctx context.Context, ds domain.DataSource, secrets map[string]string) (jsz, error) {
	c, err := a.open(ctx, ds, secrets, a.queryTimeout)
	if err != nil {
		return jsz{}, err
	}
	defer c.Close()
	var j jsz
	if err := c.Get(ctx, []string{"jsz"}, jszParams, maxBody, &j); err != nil {
		if errors.Is(err, live.ErrNotFound) {
			return jsz{}, errNotNATS
		}
		return jsz{}, err
	}
	return j, nil
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, timeout time.Duration) (*live.Conn, error) {
	ep := live.Endpoint{Base: ds.Config[keyURL], API: api}
	if token := secrets[keyToken]; token != "" {
		ep.Headers = []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + token}}
		ep.Masks = []string{token}
	}
	return live.Open(ctx, a.policy, ep, timeout)
}

var jszParams = url.Values{"accounts": {"true"}, "streams": {"true"}, "consumers": {"true"}}
