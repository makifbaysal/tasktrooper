// Package kafka is the data source kind that reads a Kafka cluster's consumer
// groups directly over the Kafka protocol (franz-go's kadm), no exporter
// needed: committed offsets against end offsets, per partition. It reads live
// state only: a series is one point, at the instant it is read. Its query
// language is domain.KafkaQuery.
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/live"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyBootstrap = "bootstrap_servers"
	keySASL      = "sasl_mechanism"
	keyUsername  = "username"
	keyPassword  = "password"
	keyTLS       = "tls"
	keyCACert    = "ca_cert"

	saslNone     = "none"
	saslPlain    = "plain"
	saslSCRAM256 = "scram-sha-256"
	saslSCRAM512 = "scram-sha-512"
	tlsOff       = "off"
	tlsOn        = "on"

	defaultPort = "9092"
	clientID    = "tasktrooper"

	defaultVerifyTimeout = 15 * time.Second
	defaultQueryTimeout  = 30 * time.Second
	// A broker that cannot be reached fails the read after a few tries
	// rather than retrying until the deadline, so the cause is what the
	// person sees, not "timed out".
	requestRetries = 2
	retryTimeout   = 8 * time.Second

	// DefaultMaxInventory bounds the groups and topics Discover lists.
	DefaultMaxInventory = 2000
	maxRows             = 5000

	maxUpstreamText = 300
	maskMinLen      = 4
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

// New answers an adapter that dials nothing (the zero urlguard.Policy allows
// no destination) until SetURLPolicy; datasource.NewService sets it.
func New() *Adapter {
	return &Adapter{
		verifyTimeout: defaultVerifyTimeout,
		queryTimeout:  defaultQueryTimeout,
		maxInventory:  DefaultMaxInventory,
		now:           time.Now,
	}
}

// SetURLPolicy decides which brokers may be dialled: the bootstrap servers
// and every address the cluster's metadata advertises, each checked at dial
// time.
func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetMaxInventory(n int) { a.maxInventory = n }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindKafka }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityQueues}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Kafka",
		Description: "A Kafka cluster's consumer groups — lag per partition, members — read live over the Kafka protocol, no exporter needed. Charts show only the current value; alerts and the queues table work directly.",
		Fields: []domain.DataSourceField{
			{
				Key: keyBootstrap, Label: "Bootstrap servers", Type: domain.FieldText, Required: true, Destination: true,
				Placeholder: "localhost:9092",
				Help:        "One or more brokers as host:port, comma-separated. The brokers the cluster advertises must be reachable from this computer too.",
			},
			{
				Key: keySASL, Label: "SASL", Type: domain.FieldSelect,
				Options:     []string{saslNone, saslPlain, saslSCRAM256, saslSCRAM512},
				Placeholder: saslNone,
			},
			{Key: keyUsername, Label: "Username", Type: domain.FieldText, Help: "For SASL."},
			{Key: keyPassword, Label: "Password", Type: domain.FieldSecret, Secret: true, Help: "For SASL."},
			{
				Key: keyTLS, Label: "TLS", Type: domain.FieldSelect,
				Options:     []string{tlsOff, tlsOn},
				Placeholder: tlsOff,
			},
			{
				Key: keyCACert, Label: "CA certificate", Type: domain.FieldSecret, Secret: true, Multiline: true,
				ShowWhen: map[string]string{keyTLS: tlsOn},
				Help:     "PEM of the CA that signed the brokers' certificates, when it is not a public one.",
			},
		},
	}
}

type session struct {
	cl    *kgo.Client
	adm   *kadm.Client
	masks []string
}

func (s *session) close() { s.cl.Close() }

func (a *Adapter) open(ds domain.DataSource, secrets map[string]string) (*session, error) {
	seeds, err := domain.ParseHostList(ds.Config[keyBootstrap], defaultPort)
	if err != nil {
		return nil, fmt.Errorf("bootstrap servers: %w", err)
	}
	var tlsCfg *tls.Config
	switch ds.Config[keyTLS] {
	case "", tlsOff:
	case tlsOn:
		if tlsCfg, err = a.tlsConfig(secrets[keyCACert]); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("TLS %q is neither on nor off", ds.Config[keyTLS])
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(seeds...),
		kgo.ClientID(clientID),
		kgo.Dialer(a.dial(tlsCfg)),
		kgo.RequestRetries(requestRetries),
		kgo.RetryTimeout(retryTimeout),
	}
	mech, err := mechanism(ds, secrets)
	if err != nil {
		return nil, err
	}
	if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, errors.New("the Kafka client cannot be set up with this configuration")
	}
	return &session{cl: cl, adm: kadm.NewClient(cl), masks: []string{secrets[keyPassword]}}, nil
}

func mechanism(ds domain.DataSource, secrets map[string]string) (sasl.Mechanism, error) {
	m := ds.Config[keySASL]
	if m == "" || m == saslNone {
		return nil, nil
	}
	user, pass := strings.TrimSpace(ds.Config[keyUsername]), secrets[keyPassword]
	if user == "" || pass == "" {
		return nil, errors.New("SASL needs a username and a password")
	}
	switch m {
	case saslPlain:
		return plain.Auth{User: user, Pass: pass}.AsMechanism(), nil
	case saslSCRAM256:
		return scram.Auth{User: user, Pass: pass}.AsSha256Mechanism(), nil
	case saslSCRAM512:
		return scram.Auth{User: user, Pass: pass}.AsSha512Mechanism(), nil
	}
	return nil, fmt.Errorf("SASL mechanism %q is not one of none, plain, scram-sha-256, scram-sha-512", m)
}

// tlsConfig starts from the policy's TLS config, which only ever changes who
// is trusted, and trusts the source's own CA instead when it has one.
func (a *Adapter) tlsConfig(caPEM string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if a.policy.TLSClientConfig != nil {
		cfg = a.policy.TLSClientConfig.Clone()
		cfg.MinVersion = max(cfg.MinVersion, tls.VersionTLS12)
	}
	if strings.TrimSpace(caPEM) != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(normalizePEM(caPEM)) {
			return nil, errors.New("the CA certificate is not a PEM certificate")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

var pemMarker = regexp.MustCompile(`-----(BEGIN|END) [A-Z0-9 ]+-----`)

// normalizePEM puts back the line breaks around the markers that a
// single-line password field drops when a PEM is pasted into it; the base64
// body may stay on one line.
func normalizePEM(s string) []byte {
	return []byte(pemMarker.ReplaceAllStringFunc(s, func(m string) string { return "\n" + m + "\n" }))
}

// dial vets every broker address through the policy, the bootstrap servers
// and the ones the cluster advertises alike, and connects to the address it
// vetted. Its errors name the broker as given, never the address it resolved
// to.
func (a *Adapter) dial(tlsCfg *tls.Config) func(context.Context, string, string) (net.Conn, error) {
	policy := a.policy
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := policy.DialContext(ctx, network, address)
		if err != nil {
			return nil, dialError(address, err)
		}
		if tlsCfg == nil {
			return conn, nil
		}
		cfg := tlsCfg.Clone()
		if host, _, err := net.SplitHostPort(address); err == nil {
			cfg.ServerName = host
		}
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			var opErr *net.OpError
			if errors.As(err, &opErr) && opErr.Err != nil {
				err = opErr.Err
			}
			return nil, fmt.Errorf("TLS with %s: %s", address, err.Error())
		}
		return tc, nil
	}
}

// clearTextPassword: SASL PLAIN sends the password as it is, so without TLS
// anyone on the path to a broker off this computer can read it. SCRAM never
// sends it.
func clearTextPassword(ds domain.DataSource, hosts []string) bool {
	if ds.Config[keySASL] != saslPlain || ds.Config[keyTLS] == tlsOn {
		return false
	}
	for _, h := range hosts {
		if host, _, err := net.SplitHostPort(h); err == nil {
			h = host
		}
		ip := net.ParseIP(h)
		if !strings.EqualFold(h, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return true
		}
	}
	return false
}

type blockedBroker struct{ address string }

func (e blockedBroker) Error() string {
	return fmt.Sprintf("broker %s is not an allowed destination, or its name does not resolve here", e.address)
}

func (blockedBroker) Is(target error) bool { return target == urlguard.ErrBlocked }

type refusedError struct{}

func (refusedError) Error() string        { return "a broker address is not an allowed destination" }
func (refusedError) Is(target error) bool { return target == urlguard.ErrBlocked }

func dialError(address string, err error) error {
	if errors.Is(err, urlguard.ErrBlocked) {
		log.Warn().Err(err).Str("broker", address).Msg("data source: destination refused")
		return blockedBroker{address: address}
	}
	var opErr *net.OpError
	switch {
	case errors.As(err, &opErr) && opErr.Timeout():
		return fmt.Errorf("cannot connect to %s: timed out", address)
	case opErr != nil && opErr.Err != nil:
		return fmt.Errorf("cannot connect to %s: %s", address, opErr.Err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("cannot connect to %s: timed out", address)
	}
	return fmt.Errorf("cannot connect to %s", address)
}

var saslFailures = []error{kerr.SaslAuthenticationFailed, kerr.IllegalSaslState, kerr.UnsupportedSaslMechanism}

func (s *session) fail(op string, err error) error {
	var blocked blockedBroker
	var auth *kadm.AuthError
	switch {
	case errors.As(err, &blocked):
		return blocked
	case errors.Is(err, urlguard.ErrBlocked):
		return refusedError{}
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: timed out; are the bootstrap servers, and the addresses the brokers advertise, reachable from here?", op)
	case errors.As(err, &auth):
		return fmt.Errorf("%s: not authorized: %s", op, s.scrub(err.Error()))
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%s: the broker closed the connection; check the SASL mechanism and credentials, and whether the listener expects TLS", op)
	}
	for _, f := range saslFailures {
		if errors.Is(err, f) {
			return fmt.Errorf("%s: authentication failed: %s", op, s.scrub(err.Error()))
		}
	}
	return fmt.Errorf("%s: %s", op, s.scrub(err.Error()))
}

func (s *session) scrub(msg string) string {
	for _, v := range s.masks {
		if len(v) >= maskMinLen {
			msg = strings.ReplaceAll(msg, v, "[redacted]")
		}
	}
	if r := []rune(msg); len(r) > maxUpstreamText {
		msg = string(r[:maxUpstreamText]) + "…"
	}
	return msg
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, a.verifyTimeout)
	defer cancel()
	s, err := a.open(ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer s.close()
	md, err := s.adm.BrokerMetadata(ctx)
	if err != nil {
		return domain.DataSourceInfo{}, s.fail("metadata", err)
	}
	brokers := fmt.Sprintf("%d brokers", len(md.Brokers))
	if len(md.Brokers) == 1 {
		brokers = "1 broker"
	}
	info := domain.DataSourceInfo{Detail: "Kafka · " + brokers}
	if md.Cluster != "" {
		info.Detail += " · cluster " + md.Cluster
	}
	hosts, _ := domain.ParseHostList(ds.Config[keyBootstrap], defaultPort)
	for _, b := range md.Brokers {
		hosts = append(hosts, b.Host)
	}
	if clearTextPassword(ds, hosts) {
		log.Warn().Msg("data source: Kafka SASL PLAIN without TLS to a broker off this computer")
		info.Detail += " · warning: SASL PLAIN without TLS sends the password unencrypted; turn TLS on or use SCRAM"
	}
	req := kmsg.NewPtrApiVersionsRequest()
	req.ClientSoftwareName = clientID
	req.ClientSoftwareVersion = "1"
	if resp, err := req.RequestWith(ctx, s.cl); err == nil && resp.ErrorCode == 0 {
		info.Version = kversion.FromApiVersionsResponse(resp).VersionGuess()
	}
	return info, nil
}

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	query, err := domain.ParseKafkaQuery(q.Query)
	if err != nil {
		return domain.SeriesResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	at, ok := live.Instant(q.To, a.now())
	if !ok {
		return domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{live.PastWindow("Kafka")}}, nil
	}
	r, err := a.read(ctx, ds, secrets, query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Warnings: r.warnings(query)}
	if !r.exists {
		return res, nil
	}
	labels := func(extra ...string) map[string]string {
		l := map[string]string{"group": query.Group}
		if query.Topic != "" {
			l["topic"] = query.Topic
		}
		for i := 0; i+1 < len(extra); i += 2 {
			l[extra[i]] = extra[i+1]
		}
		return l
	}
	switch query.Metric {
	case domain.KafkaMetricLagByPartition:
		for _, p := range r.partitions {
			res.Series = append(res.Series, domain.Series{
				Name:   domain.KafkaMetricLag,
				Labels: labels("topic", p.topic, "partition", fmt.Sprint(p.partition)),
				Points: live.Point(at, known(p.lag)),
			})
		}
	case domain.KafkaMetricConsumers:
		res.Series = append(res.Series, domain.Series{Name: query.Metric, Labels: labels(), Points: live.Point(at, live.Float(int64(r.consumers)))})
	case domain.KafkaMetricMembers:
		res.Series = append(res.Series, domain.Series{Name: query.Metric, Labels: labels(), Points: live.Point(at, live.Float(int64(r.members)))})
	default:
		res.Series = append(res.Series, domain.Series{Name: query.Metric, Labels: labels(), Points: live.Point(at, r.totalLag())})
	}
	return res, nil
}

var keyColumns = []string{"topic", "partition"}

var tableColumns = []domain.TableColumn{
	{Key: "topic", Label: "Topic"},
	{Key: "partition", Label: "Partition"},
	{Key: "committed_offset", Label: "Committed offset"},
	{Key: "end_offset", Label: "End offset"},
	{Key: "lag", Label: "Lag", Unit: "messages"},
	{Key: "member_id", Label: "Member"},
	{Key: "client_id", Label: "Client"},
}

// Table answers one row per partition the group consumes or has committed
// to (of the query's topic, when it names one); its metric is not used.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TableQuery) (domain.TableResult, error) {
	query, err := domain.ParseKafkaQuery(q.Query)
	if err != nil {
		return domain.TableResult{}, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	res := domain.TableResult{Columns: tableColumns, KeyColumns: keyColumns, Rows: []map[string]any{}}
	if _, ok := live.Instant(q.At, a.now()); !ok {
		return res, nil
	}
	r, err := a.read(ctx, ds, secrets, query)
	if err != nil {
		return domain.TableResult{}, err
	}
	for _, p := range r.partitions {
		if len(res.Rows) == maxRows {
			res.Truncated = true
			break
		}
		row := map[string]any{"topic": p.topic, "partition": float64(p.partition)}
		for key, v := range map[string]int64{"committed_offset": p.committed, "end_offset": p.end, "lag": p.lag} {
			if v >= 0 {
				row[key] = float64(v)
			}
		}
		if p.member != "" {
			row["member_id"] = p.member
		}
		if p.client != "" {
			row["client_id"] = p.client
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// Discover lists "group/<name>" for every consumer group and "topic/<name>"
// for every topic but the internal ones.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	ctx, cancel := context.WithTimeout(ctx, a.queryTimeout)
	defer cancel()
	s, err := a.open(ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer s.close()
	groups, partial, err := s.groups(ctx)
	if err != nil {
		return domain.Inventory{}, err
	}
	topics, err := s.adm.ListTopics(ctx)
	if err != nil {
		return domain.Inventory{}, s.fail("topics", err)
	}
	inv := domain.Inventory{Metrics: []string{}, Truncated: partial}
	for _, g := range groups {
		inv.Metrics = append(inv.Metrics, "group/"+g)
	}
	for _, t := range topics.Names() {
		inv.Metrics = append(inv.Metrics, "topic/"+t)
	}
	if len(inv.Metrics) > a.maxInventory {
		inv.Metrics = inv.Metrics[:a.maxInventory]
		inv.Truncated = true
	}
	return inv, nil
}

// DiscoverQueues offers every consumer group by its name.
func (a *Adapter) DiscoverQueues(ctx context.Context, ds domain.DataSource, secrets map[string]string) ([]domain.QueueTarget, error) {
	ctx, cancel := context.WithTimeout(ctx, a.queryTimeout)
	defer cancel()
	s, err := a.open(ds, secrets)
	if err != nil {
		return nil, err
	}
	defer s.close()
	groups, _, err := s.groups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.QueueTarget, 0, min(len(groups), a.maxInventory))
	for _, g := range groups[:min(len(groups), a.maxInventory)] {
		out = append(out, domain.QueueTarget{Name: g, Params: map[string]string{"group": g}})
	}
	return out, nil
}

// groups lists the cluster's consumer groups, sorted; partial when some
// broker did not answer.
func (s *session) groups(ctx context.Context) ([]string, bool, error) {
	listed, err := s.adm.ListGroups(ctx)
	partial := false
	if err != nil {
		var shard *kadm.ShardErrors
		if !errors.As(err, &shard) || shard.AllFailed {
			return nil, false, s.fail("consumer groups", err)
		}
		partial = true
	}
	return listed.Groups(), partial, nil
}

func (a *Adapter) read(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.KafkaQuery) (groupRead, error) {
	ctx, cancel := context.WithTimeout(ctx, a.queryTimeout)
	defer cancel()
	s, err := a.open(ds, secrets)
	if err != nil {
		return groupRead{}, err
	}
	defer s.close()
	return s.group(ctx, q.Group, q.Topic)
}

func known(v int64) *float64 {
	if v < 0 {
		return nil
	}
	return live.Float(v)
}
